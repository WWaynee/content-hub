# P04 · 证据绑定的「人读 source + has_newer」装载

- RFC 出处：rev-2 §8.2-Q2 / §10.1, rev-4 §13.6 / W6；README 顺序=P04
- 状态：待开工
- 前置：P03（依赖真结构做"sentence_views source"）
- 目标：让证据从"一串 `doc_sentence_id`"变成**可读的 source（原文引句 + 文档名 + 章节 + 版本 + has_newer）**，否则前端 tooltip/清单(§9.2)做不真，验证拿不到原句。

---

## 1. 问题现象
`EvidenceBinding` 只存 `doc_file_id/doc_sentence_id`(模型在 `storage/model/article.go:55`),`ListArticleBindings` 直接 `Find` 出整行,不含 source_text/文档名/章节。`GetArticle`(handler/article.go) 只把 bindings 数组裸透给前端,前端拿不到"这句出自《XX》第X章那句"对应原文 → tooltip/可溯源是空壳。同时旧版引用无 has_newer,用户不知道引用资料已更新。

## 2. 范围与命中代码
| 文件 | 位置 | 改动 |
|------|------|------|
| `api/service/*` | 新增 `bindings → sources` 装配(join doc_sentences/doc_versions/kbase_files) | 构建 read model |
| `api/handler/article.go` | `GetArticle` response | 返回带 source 的 sentence_views(见 §10.1 RFC 草案) + 顶层的 full_content |
| `api/service/export.go` | 证据清单导出 | 让清单可用可复制原句引文(呼应卖点) |
| 可能 dosages | （可选）把 source 冗余进 article_version 快照，避免大量 join | 先实用做 join，过快照冗余后续可再做（见"兼容"）

## 3. 可执行步骤
1. **装配 sources**：给定 `article_version_id`,读 bindings → 对每条 doc_sentence_id 联到该句原文+所属 chunk 章节 + 归属 kbase_file 名 + doc_version.version_md5。返回形如
   `{sentence_index, claim_type, text, sources:[{source_text,file_name,document_id,chapter_title,version_md5,current_latest?(==该doc最新version)}]}`。
   旧/新 `has_newer` 判定：比 `version_md5` 与 `kbase_file.current_version_md5`,不等则 `has_newer=true`。
2. **放 response**：`GET /workspaces/:wid/article` 返回 `{art,title, full_content, sentence_views:[...]}`;按 RFC §10.1 schema 对齐。保留旧 bindings 字段一段时间避免破坏现前端(P12/P11 才切走)。
3. **claim_type**：先把每句按有无 sources 打成 `bound`(有)/`plausible-ai`(无源,P03 后可配);真正"flag 待人工 (no_source需复核)"由 P09 的治理层填。此处先给基础两态占位。
4. **导出**：evidence list 每条放 source_text 让可复制;没有 source 的给出"AI 通稿,无外部引用"。

## 4. 兼容与旧数据
- join 不做表改动,不需迁移;但会引入更多查询,若量大了在 article_version 快照冗余 `presented_bindings`(可选链路)。先 join、后观测。

## 5. 验收标准
- 集成:生成一篇(或手工造 binding),`GetArticle` 的 sentence_views.source 能带给文档名/章节/原句/版本;若引用的 doc 已更新另一个 version,`has_newer=true`。
- 既有前端(在不改 WorkspaceDetail 前提下)不会因 response 加字段而崩(向后兼容保留旧 bindings)。

## 6. 依赖 & 顺序说明
- 它把 R RFC §10.1/§9.2 的数据地基补上;前端能绘只有到这里。
- 若 join 明显慢,先只在需要展示时触发;导出在导出路径单独查。

## 7. done gate
“P04 done” = sentence_views 返回可读 source + has_newer;导出证据清单可复制原句;现前端不崩。
