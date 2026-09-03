# P04 · 证据绑定的「人读 source + has_newer」装载

- RFC 出处：rev-2 §8.2-Q2 / §10.1, rev-4 §13.6 / W6；README 顺序=P04
- 状态：**DONE**（已实现并真验收，见文末"完成记录"与"🧭 回顾"）
- 实现方式简述：新增 `api/service/sources.go` 装配层——`LoadSentenceSources` 把 bindings 一次性批量 join `doc_sentences`(原文句)/`doc_chunks`(章节)/`kbase_files`(名/当前版本/active) 产出**人读 source**；`has_newer`=绑定版本≠文件当前版本，`file_deleted`=文件已软删仍还原；`GetArticle` 新增 `sentence_views`(claim_type + sources)，导出走同一装配。
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

---

## ✅ 完成记录（真实验收）
- **已实现**：
  - 新增 `api/service/sources.go` 装配层（read model）：`LoadSentenceSources(ctx, tenantID, bindings) map[ArticleSentenceID][]SourceView`
    - 用 `ListDocSentencesByIDs / ListDocChunksByIDs / ListFilesByIDs`（storage 层新增的 3 个 `id IN` 批量查询）一次取齐原文句/章节/文件元数据，**替代逐条 SQL 的 N+1**；
    - 每条 evidence 展开成 `{doc_sentence_id, source_text(原句引文), file_id, file_name, scope, chapter_title, version_md5, has_newer, file_deleted}`；
    - `has_newer` = 绑定 `version_md5` ≠ `kbase_files.current_version_md5`（W6：资料之后又更新过）；
    - `file_deleted` = 文件已 `active=0` 软删，但 DB 保留原文/chunks → 仍能还原 `file_name` 与当时原文（不可变锚，仅提示不可见）；
    - 孤儿绑定（原文被物理清理查不到）安全忽略：只少一条、不强编、不回滚整稿。
  - `api/handler/article.go::GetArticle`：response 新增 **`sentence_views`**（`BuildSentenceViews`→逐句 `claim_type`(bound/plausible-ai) + `sources`），旧字段 `sentences/sections/bindings/full_content` 完整保留 → 现前端零破坏。
  - `api/service/export.go`：证据清单不再逐条 `GetSentenceByID`，改走同一 `LoadSentenceSources`；每条输出**可复制的原句引文** + 文档名/章节/版本，并顺带标注"该资料已被删除 / 之后又有新版本"。
  - `web/src/types.ts`：新增 `EvidenceSource / SentenceView` 类型 + `Article.sentence_views?`（可选字段），前端渲染不变（渲染给 P11）。
- **验收（本机真 MySQL，非 skip）**：
  - 新增 `api/service/sources_integration_test.go::TestEvidenceSourcesAssembly`：造 3 份来源文档（无新版/已更新/软删）+ bound/unbound 句，断言 bound 句逐源还原 file/chapter/source_text/version、`has_newer`、`file_deleted` 语义均正确，unbound 句 `plausible-ai + 空 sources`，导出含文件名与"已被删除/新版本"提示：
    `go test ./api/service/ -run TestEvidenceSourcesAssembly -count=1 -v` → **PASS**
  - `go test ./... -count=1`（全量纯单测）全绿；`go vet ./api/service ./storage ./api/handler` 无告警；既有 `TestExportArticle`、P03 `TestPersistStructuredHierarchy`、P01/P02 DB 回归均绿。
- **兼容/边界**：P04 采用 **join（不做表冗余）**，与 README「先 join、量大再冗余」默认一致；孤儿/已删引用的 `source_text` 来自保留的 `doc_sentences` 快照，不因文件删除而消失。`no_source`(疑似该有据却无)态由 P09 治理层填写，P04 只给 bound/plausible-ai 两态占位。

---

## 🧭 回顾（面试/复盘用）：P04 到底改什么

### 一句话
**把"这句稿子引用了哪份资料"从一串没人看得懂的 id(`doc_sentence_id`) 还原成一段人读的出处——原句引文、文档名、章节、版本——并让用户知道它引用的资料是否已经变老或已被删除。**

### 原先在什么场景违约（为什么它是"可溯源"的花瓶）
| 场景 | 触发 | 后果 |
|------|------|------|
| 用户 hover 稿子某句想看"出自哪份原文" | 数据层只给了 `doc_file_id/doc_sentence_id` 两个数字 | 前端拿不到原句，tooltip/清单只能写"证据 xN"，所谓可溯源是你看不见的出处=空壳 |
| 资料被更新/删除后 | 引用仍静默指向旧版，用户毫无感知 | 用户引的是 3 天前旧资料还在发稿，还以为是当前的 |
| 审单人要对证据逐条核对 | 导出证据清单只有文档编号 + 段落 id | "审要核"变成"去库里一条条找"，审计单没法用 |

> 本质：**溯源只在"你能读懂引用了什么"时才成立**。我们当时把"能索引到这句"当成了卖点，但忘了把"能读、能核、能察觉它老/没了"一起交到界面。光在 DB 里留下不可变锚点、却在前端不给原文，等于把仓库锁好但把钥匙丢了。

### 宏观方案（精神，非代码）
核心一句话：**让"引用"在数据层就同时带"不可变锚（审）"与"可读快照（看/核）"，读端点边局查询即还原，不等 UI 层事后去补。**

1. **分离"锚"与"人读层"**：`evidence_bindings` 仍只存不可变外键（审计/派生用）；首次需要"看"时，通过批量 join 原生句/切片(章节)/文件元数据，装配出 `source_text + 文件名 + 章节 + 版本` 这个人读快照视图——锚不动、人读层随读生成。
2. **把时间维度放进去**：不只给版本号，还给 `has_newer`(引用之后资料又更新) 与 `file_deleted`(资料已删)——让"引用停在旧版/已删"不再是日志级，而是用户可见、可提示的成熟状态。
3. **一处装配、处处复用**：`GetArticle` 的 tooltip、导出的审计清单走同一套装配，保证界面看到的和导出的出处一致（不会再出现"UI 说得对、导出一对 id"的两张皮）。
4. **兼容不倒退**：新增字段以追加形式给，不删既有 bindings/文本墙字段，让旧流程与未来 P11(结构化渲染)并行可用；装配用去重批量读，不因"多了一次 join"把响应打崩。
5. **诚实兜底**：孤儿/找不到的引用不强编、不整稿失败，如实略过并在导出注明"保留当时原文/已被删除"。

一句话精神：**"有出处"不应只有一个看不见的 id——把不可变的锚交给审计，把可读的引文、章节、版本乃至"它老了/没了"交给读者，溯源才算真正交到用户手上。**
