# P02 · 稿件版本并发：乐观锁 / Compare-And-Swap

- RFC 出处：rev-3 §12.4 / C2；README 顺序=P02
- 状态：待开工
- 前置：无（与 P01 可并行）
- 目标：稿件新增/修订/resave 不再依赖"内存 `prev.VersionNo+1` + 唯一索引最后兜底",改成一个能让并发写**在提交前就探测到冲突并友好告知**的乐观锁。

---

## 1. 问题现象
`model.Article.CurrentVersionNo`、`ArticleVersion.VersionNo` 由应用读取 `prev` 后 `+1`(`revise.go:86/409`, `generation.go` 取 requirement.version),**没有** `WHERE current_version_no=base` 的 compare-and-swap,唯一防线是 `(article_id, version_no)` 唯一索引——并发第二次 `Create` 才在**花完 LLM 之后**撞键报错/回滚。单写者+串行不暴露;开放人工并行编辑后(P08/P09)会成为丢写与整遍返工之源。

## 2. 范围与命中代码
| 文件 | 位置 | 改动 |
|------|------|------|
| `storage/article.go` | `GetArticleByWorkspace`,可新增 `CompareAndBumpVersion(ctx, id, base, next)` | 加 CAS 原子点 |
| `api/service/revise.go` | `ReviseSentenceFull`/`AppendArticleContent` / `ApplyArticleRevision` | 提交带 base_version_no,CAS 判冲突 |
| `api/service/generation.go` | `PersistArticleSnapshot` | 落 article 前 CAS 版本 |
| `api/handler/*`、REST 请求体 | generate/revise/append 请求带 `base_article_version` | 把乐观锁暴露给前端触发者 |

## 3. 可执行步骤
1. **新增 CAS 帮助函数**（storage）：
   `UpdateArticleVersionCAS(ctx, articleID, fromVersionNo, toVersionNo) (bool, error)` 执行
   `UPDATE articles SET current_version_no=toVersionNo WHERE id=articleID AND current_version_no=fromVersionNo` 并用 `RowsAffected==1` 判成功。
2. **落库前先 CAS**：
   - `PersistArticleSnapshot`(generation):进入事务前持 baseline。若 `GetArticleByWorkspace` 得到 current= old,则 CAS old→new;若返回 0 行→冲突(返回 409/专错)。
   - `ReviseSentenceFull`/`AppendArticleContent`(revise/append):读到 prev(即 base)后,事务内先对该 article_id 做 CAS（base→ prev+1）;CAS 失败→不要跑后续,直接向上报"已被更新,请以最新版本重试"。
   - 由此"是否继续跑整链"前移到"允许可接收的 base_version"判定,避免把撞键留到 `Create` 时刻。
3. **请求带 base**：generate/revise/append 的 handler 从请求(或从 path 取 latest)拿到准确的 base_article_version,并在取到"当前已不同"时返回可读冲突(HTTP 409 + message"稿件在编辑期间被他人保存过,请刷新最新版再操作")。
4. 老数据无损:current_version 本来就只增;无须迁移数据。

## 4. 验收标准
- 新 API 级并发测试：两并发 revise 同一 workspace——恰一个成功;失败者收到竞争错误且**不会多生成一个 article_version**(表里 {article_id, version_no}) 唯一)。
- 失败后新 UI/调用方可读提示(Gate P09 会接前端,但接口层先给清晰 message)。
- 既有单测 revision/generation 保持绿;`GetLatestArticleVersion` 语义不变。

## 5. 开放问题
- 错误码:用 409+固定 code 还是可扩张;建议 code 统一常量(如 `CodeVersionConflict`),供前端识别刷新。
- 简版可以先不做 `REST base` 透传,仅后端内部兜 CAS(以 latest 为准);但这样"用户自己改的稿被别人覆盖"即使检测出也不过是重跑。先把接口级透传列为目标,前端反馈实现放到 P12。这里目标收敛为**后端保证一致;前端透传 base 在 P12 补**。

## 6. done gate
“P02 done” = 并发 CAS 测试通过(1 成 1 拒不重复)+ 未额外增加 article_version + 单测与基线绿。
