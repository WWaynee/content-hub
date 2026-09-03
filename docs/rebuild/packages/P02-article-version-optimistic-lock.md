# P02 · 稿件版本并发：乐观锁 / Compare-And-Swap

- RFC 出处：rev-3 §12.4 / C2；README 顺序=P02
- 状态：**DONE**（已实现并真验，见文末“完成记录”）
- 实现语义：article 版本号由乐观锁自增决定（`PersistArticleSnapshot`/`ApplyArticleRevision`/`AppendArticleContent` 在写快照前均先 `CASBumpArticleCurrentVersion`，只有把 `current_version_no: base→base+1` 抢到的唯一写者才继续；冲突方返回 `ErrArticleVersionConflict`）。P01/P02 独立可并行。
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

---

## ✅ 完成记录（真实验收）
- **已实现**：
  - `storage/article.go`：新增 `CASBumpArticleCurrentVersion(ctx, articleID, expected, next)(bool,error)`(mysql `UPDATE ... WHERE current_version_no=expected`, RowsAffected==1 判成功) 与 `ListArticleVersions`（全版本列表，测试用）。
  - `api/service/generation.go`：去掉外部传入 versionNo 的参数，首次生成 current 从 0→v1，已有则 `base→base+1` CAS 抢号；失败即 `ErrArticleVersionConflict`（不再带着旧 `req.Version` 覆盖 current，消除与 revise 的版本两源）。
  - `api/service/revise.go`：`ApplyArticleRevision` 与 `AppendArticleContent` 写新版本前同走 CAS 抢号（冲突不落库、不跑后续写入）。
  - `api/service/version_conflict_integration_test.go`（新，integration，纯 MySQL 不触网）：`TestConcurrentGenerationVersionCAS` 与 `TestRevisionApplyCAS`。
  - handler/response：`ErrArticleVersionConflict` → `response.CodeVersionConflict(409)` 可读 message（前端在 P12 用 409 刷新提示；P02 先保后端一致+友好文案）。
- **验收（本机真 MySQL，非 skip）**：
  - `go test -tags=integration -run 'TestConcurrentGenerationVersionCAS|TestRevisionApplyCAS' ./api/service/ -count=1 -v`：
    - generation 并发 6 → **成功=1、冲突=5、版本唯一且连续** PASS
    - revision 顺序 1→2→3；并发 4 → **成功=1、冲突=3、无重复版本** PASS
  - `go test ./... -count=1`（全量纯单测）与 `go test -tags=integration -run '^$' ./...`（integration 全源码编译）通过；`TestMultitenantIsolation` 仍绿。
  - 既有在版本号语义上的相关 `PersistArticleSnapshot(ctx,…,article,evidence)` 签名同步：调用方(handler/append_integration/revise_integration/generation_test/export_test/revise_test)已去掉第 4 参 `1`——新语义下首篇仍是 v1、与旧行为对齐。
- **兼容/已知**：本次把“article 版本号随 requirement.version”解耦为“article 自身单调+1”，与需求单惰性失效(仅以检索批次版本+requirement.version 判定)正交，无回归；历史遗留 article 若 current 与已列最新不齐，在首次新写时会被 CAS 平滑校正到“最新+1”。

---

## 🧭 回顾（面试/复盘用）：P02 到底修了什么

### 一句话
**修的是“稿件多版本并发写入时的版本号错乱 + 乐观锁缺失”——同一篇稿同时被多次生成/修订时会产生重复版本号或静默丢写。**

### 原先在什么场景触发
| 场景 | 触发 | 后果 |
|------|------|------|
| 两生成/修订几乎同时发 | 用户对同一工作区连续点“生成”，或两个请求在 LLM 链上交错 | 都读到旧 `current`，各自 `+1` → 产生两份相同 version_no |
| 昂贵链路最后才失败 | 靠唯一索引在 commit 时才拦 | 冲突方已跑完整个 LLM 才爆错、重试贵且事后才知道 |
| 版本号双源 | generation 用 `requirement.version`、revision/append 用自己的 `+1` | 同一稿件存在两条“我认为下一个版本是多少”的机制，宏观相互冲突 |

> 本质：这里的“并发”不是炫技，是真实场景（同一篇长稿多次操作）。版本号是内容一致性地基，谁都能“先读旧号、写完再报号”的话地基就是错的。

### 宏观方案（精神，非代码）
1. **唯一、只能递增一次的“原子门闩”**：每个想新版本的写者先执行一次 CAS——“若我的基线 == 当前版本号，就把它 +1 据为己有”。数据库保证只有一个赢；其余立刻得到明确的“版本冲突”，而不是等快照落库才撞唯一索引。
2. **版本号收敛到单一来源**：generation/revision/append 统一“以稿件自身当前版本 +1 为准”（去掉 req.Version 与 prev+1 并存），让“稿件版本号是多少”只有一种讲法。
3. **冲突是友好失败**：抢号失败者立刻收到可读冲突信号（接口 409 / 提示刷新），不把昂贵 LLM 的失败拖到落库最后一刻。

一句话精神：**把“版本号该是多少”从「每个人各自算」变成「抢到一把只漏一次的锁才算数」。**
