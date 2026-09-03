# P03 · 结构化层级真落库（W1 命根：section/paragraph 不是 0）

- RFC 出处：rev-4 §13.1 / W1；README 顺序=P03
- 状态：**DONE**（已实现并真验，见文末“完成记录”）
- 实现简：`PersistArticleSnapshot` 写真实 `(section,paragraph,sentence)`；DB 读按三元排序；revise(Apply/Append) 继承/推进段落结构；`GetArticle` 返回 `sections` 分层；旧数据线性降级、不做启发式猜标题。
- 目标：让 `article_sentences` 真正携带 `section_index/paragraph_index/sentence_index`，前端/后续包才拿得到"结构化"稿件；并把 `GetArticle` 返回带层级结构，替换 "平铺数组 + 一段 full_content markdown" 的现状。

---

## 1. 问题现象
`model.ArticleSentence` 定义了 `section_index`/`paragraph_index`,但**全仓库没有任何一处给它们赋值**(我 grep 过:`generation.go`/`revise.go` 只写 `SentenceIndex`)。所以 DB 里这两个列恒 0,章节/段落只存在于临时拼出的 `full_content`(markdown) 字符串;前端 `GetArticle` 只能平铺 sentences + 一段 markdown。于是"按段治理/结构化渲染/句→段定位"在数据层是空中楼阁。

## 2. 范围与命中代码
| 文件 | 位置 | 改动 |
|------|------|------|
| `api/service/generation.go` | `PersistArticleSnapshot` 遍历 `article.Sections→Paragraphs→Sentences`(约 44-76 行) | 落库时写入 三级 index,而非全局 sentSeq |
| `api/service/revise.go` | `ApplyArticleRevision`(约 93-109)、`AppendArticleContent`(约 96 行 on 415-427 建新句子部分) | 重排/构建段落时补真 index |
| `api/handler/article.go` | `GetArticle` 返回响应 | 返回带 section/paragraph 的结构(并保留 full_content 兼容) |
| `model`/db migration | 无需加列(已有字段) | — |
| 一次性迁移 | 治理已生成旧版本 | 见"兼容/旧数据" |

## 3. 可执行步骤
1. **generation 层真写 index**：在 `PersistArticleSnapshot` 遍历 `article.Sections` 时,外层以 `section_i`、中层 `para_i`、内层句子 `sent_i` 递进,把它写进每句 `ArticleSentence{SectionIndex:si, ParagraphIndex:pi, SentenceIndex:si}`;保留一个**并行扁平全序索引**`sentSeq`(供某些 call)或改为让读取方用三元组排序。
   > 关键:不要再搞"一个全局 sentSeq 当 sentence_index"当唯一身份(那与 P08 冲突);要么以 (si,pi,si) 组合作为稳定排序键,要么在 P08 引入真正的稳定 id。此处 P03 只需能把结构精确重建,身份/增删移到 P08 处理。

2. **revise/append 也能定位段落**：
   - `ApplyArticleRevision` 在替换目标句时,目标句的 (section/paragraph) index 从原句继承(若换句内容但不换归属);允许按需跨段落则需提供 target 段落/位置(该能力部分在 P08 完全拆;本包先把"目标句带着原两级 index 重建"写对)。
   - `AppendArticleContent` 追加为新段:给 `paragraph_index = 上一个同级或 max+1` 且不与已存在冲突(段间有序)。列一个简单的"排序 =（section,paragraph,sentence)"读取约定并文档化。

3. **GetArticle 返回结构**：让 handler 从 `article_sentences` 按 (section,paragraph,sentence) 排序重构 `sections: [{heading, paragraphs:[{sentences:[...]}]}]`,同时保留 `full_content`(给现前端/导出少改动路径)。给 token/索引即可,原平铺数组可作为向后兼容保留一段时间(前端 P11 会切到结构)。

4. **迁移旧版本**：历史 `article_version` 只有平铺 sentences + full_content。策略见下即可(选择一行拍板做,不要半做)。

## 4. 兼容与旧数据
- 旧版本**(默认)降级为"线性一篇文章区"(section_index=0, paragraph=按出现序)**,文案标注"旧版未保留结构化,已按线性展示"。避免从只有 full_content 用启发式去猜标题/段落,那会造成大量错位。
- 若你想走"从 full_content 反推结构"以保留历史检索到证据的"段归属",可在 P11 之前交给评审开一个单独 gate,本包先不动(存在过度工程与错位风险,默认不推)。

## 5. 验收标准
- 单测:构造 `agent.Article{a.Sections[{Heading, Par...}]}` → `PersistArticleSnapshot` → 读回 `article_sentences`,断言三 index 按结构重建且 heading 顺序对应。
- `GetArticle` 返回的 section/paragraph 层数与输入一致(不含 parse 启发),子句文本/顺序不减。
- 回归 generation/revise 集成测试(基于最新代码,非旧数据)仍通过。

## 6. 开放问题
- "句子稳定 id"在 P08 才引正式 schema;P03 只用（section,paragraph,sentence）作为排序;README 已提示 P08 的依赖顺序(先 P08 再 P09)。

## 7. done gate
“P03 done” = 新生成/新增段/追加段的 `article_sentences` 已带真 section/paragraph;`GetArticle` 返回结构层;旧版走线性降级;单测绿。

---

## ✅ 完成记录（真实验收）
- **已实现**：
  - `api/service/generation.go::PersistArticleSnapshot` 遍历 `article.Sections→Paragraphs→Sentences` 时写入真实的 `SectionIndex/ParagraphIndex/SentenceIndex`（后一项=段内句号、每段从 0 起）；内部全序 sentSeq 仅当快照内把 evidence 绑定回已建好的句行所需的切片 index，不写入 DB 栏。
  - `storage/article.go::ListArticleSentences` 排序改为 `(section_index, paragraph_index, sentence_index)`；`allFlat` 辅助把旧版本(全 0 / 无结构)退化到 `ORDER BY id`（线性）返回，避免误导性重建。
  - `api/service/revise.go`：`sentDraft`/Append 内部 `draft` 增加三栏字段；`ApplyArticleRevision` 落新版时逐句继承 source 行的 sec/para/sent（被改句换文本不换归属）；`AppendArticleContent` 复制旧行时保留其层级，并把新增内容作为**新的一段**落下（`paragraph_index = 同 section 最大 + 1`）。
  - `api/handler/article.go::GetArticle` 新增 `sections` 分层响应（`groupSentencesByStructure`：section→paragraph→sentence 三元重建，并保留 `sentence.id` 便于 evidence binding 展示映射）；保留原 `sentences/full_content/bindings` 兼容现前端。
- **验收（本机真 MySQL，非 skip）**：
  - 新增 `api/service/structure_hierarchy_persist_test.go`：构造 2 section×多 paragraph×多句的 `agent.Article`，`PersistArticleSnapshot` 落库后 `ListArticleSentences` 读回，断言 6 句严格按 `(section,paragraph,sentence)` 三元精确重建：
    `go test ./api/service/ -run TestPersistStructuredHierarchy -count=1 -v`
    → **PASS「6 句按 (section,paragraph,sentence) 精确重建」**
  - `go test ./... -count=1`（全量纯单测）绿；`TestRevisionApplyCAS/TestConcurrentGenerationVersionCAS/TestExport/TestPersistArticleSnapshot/TestRevisionApply` 等 DB 回归均绿；integration 全源码编译通过。
- **兼容/边界**：旧版本（三栏全缺、historically sentence_index 全局递增）无法安全地由 DB 还原多章多段 → 读取方自动退化为**线性(单节线性段)**返回，不做基于 full_content 的启发式猜标题（标题解析交给 P11，在生成端显式保留章节可读源后更可靠）。段落重排/增删句(sentence-level delete/move/insert)归 P08，P03 把"每次写后三段结构可被读取方精确重建"做对，为 P08/P11 打底。
