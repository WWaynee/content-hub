# content-hub 多 Agent 稿件功能的实际实现方案

> 本文件从**代码实际运行链路**出发，说明当前多 Agent 是如何协同实现"稿件生成 / 对话修改 / 证据溯源"的。
> 与 `docs/architecture/multi-agents.md`（设计蓝图）不同，本文记录的是**现已接进 HTTP handler 生产链路、真实跑通**的实现细节，以及仍停留在设计/单测、尚未接线的部分，便于看清实现进展。

---

## 0. Agent 全景（当前实际存在哪些）

| Agent / 组件 | 所在的代码包 | 是否接入生产 handler | 一句话职责 |
|--------------|--------------|:---:|------------|
| 知识检索 agent | `agent/retrieve` | ✅ 生成走 | 把需求拆成检索 query，逐条检索知识库，产出证据 |
| 稿件撰写 agent | `agent/writing` | ✅ 生成走 | 按证据 + 需求生成结构化稿件，建立"句→证据"绑定 |
| 证据整理 agent | `agent/evidence` | ✅ 生成走 | 把"句↔证据"绑定格式化输出为证据清单（不重检） |
| 需求对话 agent | `agent/dialogue` | ✅ 对话走 | 把用户一句话拆成结构化动作计划 `DialoguePlan` |
| 事实合规校验 | `agent/censor` | ✅ 生成+修订都走 | 子需求点缺证核对 + 稿件事实断言校验（"零编造"闸门） |
| 稿件编排 | `agent/orchestrator` | ✅ 生成走 | 串联 检索→撰写→校验→证据 |
| 句子修订器 | `agent/orchestrator/reviser.go` | ⚠️ 未接 handler | 定义了"句子级重写 + 重检"的独立实现，但生产链路未用它 |
| 知识库问答 agent | `agent/qabot` | ✅ 独立问答走 | 纯问答（多轮检索） |

---

## 1. 一个工作区/稿件的生命周期

```
工作区(workspace) ─ 一个 = 一篇稿件的生成上下文
   │  ├─ 需求单(requirement)：表单 + 版本号(version，改字段即递增)
   │  ├─ 勾选范围(requirement_scope)：锁死检索范围（目录=递归）
   │  ├─ 检索快照(retrieval_batch + items)：某次检索的用量快照（带 requirement.version）
   │  ├─ 稿件(article) → 稿件版本(article_version) → 句子→证据绑定(evidence_binding)
   │  └─ 对话会话(conversation + conversation_messages：存 DialoguePlan JSON + target 锚点)
```

**关键数据不变量**：
- 稿件内容**禁止手动字符编辑**，所有文字都来自 AI（生成或对话改写）。
- 每个句子可以绑 0..n 份证据；证据指向知识库某文档某版本某句（`doc_sentence_id`），**不可变版本锚定**。
- 每次改动（生成/修订）都会**新增一个 article_version 快照**，旧版保留。

---

## 2. Agent 之间的工作流

### 2.1 Generation（初稿/整篇）—— 主链路

入口：`POST /api/workspaces/:wid/generate` → `handler.GenerateArticle`。

```
                 handler.GenerateArticle
                 │ 读需求单 → toAgentRequirement()
                 │ 记录原状态 → 置 generating(禁导)
                 ▼
   agent/orchestrator.Generate(ctx, tenant, req, fileIDs)
   │
   ├─ [1] retrieve.Retriever.Retrieve()          ← 知识检索 agent
   │       拆需求→query 列表 → 每条 SearchKbaseSentences()
   │       产出 []Evidence（句级，跨文档去重）
   │       ※ 注：当 orchestrator 判断 checker!=nil，下面 [1.5] 的结果会覆盖这份，
   │         此处检索证据仅在 checker=nil(单元测试) 时作为撰写输入。（见 §5.2）
   │
   ├─ [1.5] censor.ClaimPlanner.Check()          ← 闸门一：子需求点缺证核对
   │       整个需求拆成若干 claim(needs_fact 标注)
   │       每个 needs_fact=true 的点单独检索核对   ← 此结果为生产实际撰写依据
   │       → 有缺证点 ⇒ 抛 ErrInsufficientEvidence(带缺证清单)
   │       → 全空 ⇒ ErrNoEvidence
   │       → 全部有证 ⇒ 用这些证据作为撰写输入
   │
   ├─ [2] writing.Writer.Write()                 ← 稿件撰写 agent
   │       用(需求 + 证据数组)生成整篇结构化稿件 Article
   │       每个句子的 evidence_refs 指向证据数组下标
   │
   ├─ [2.5] censor.FactVerifier.Check()          ← 闸门二：事实断言校验
   │       抽取稿件每条"数据/事实断言"
   │       核对能否在证据原文找到直接支撑(允许语义等价,禁统计推断)
   │       → 有无法支撑的断言 ⇒ ErrFactUnsupported，禁止落库
   │       → 通过 ⇒ applyFactRefs 用校验结果回写每句真实证据绑定
   │
   ├─ [3] evidence.Builder.Build()               ← 证据整理 agent(纯格式化)
   │       由"文章结构 + 已绑定证据"生成 EvidenceManifest(导出清单)
   │
   ▼
   handler: PersistRetrievalBatch(检索快照) → PersistArticleSnapshot(稿件+证据落库)
            → 状态置 generated
```

**结果产物** `GenerationResult{ Article, Evidence, Manifest, Queries }`，其中 Article 已带每句的数据断言对应证据编号。

### 2.2 对话修改（改需求单字段 / 改稿句子 / 追加）

入口:`POST /api/workspaces/:wid/chat` → `handler.WorkspaceChat` → `service.Dispatcher.ProcessChat`。

```
 service.Dispatcher.ProcessChat(ctx, tenant, user, workspace, ...用户一句话, target_type, target_ref)
   ├─ EnsureConversation(建会话)
   ├─ [1] dialogue.Agent.Parse(用户一句话)
   │       把一句话解析成 DialoguePlan{ Actions[] }
   │       每个 action 是原子动作，tool ∈ 白名单：
   │         update_requirement_field / request_retrieval
   │         / revise_article_sentence / append_article_content
   │       schema.Validate(plan)   ← 白名单+字段合法性硬机检(不合法=报错)
   ├─ 落用户消息 + plan 消息到会话
   ├─ 逐 action 执行(不阻断; 每个 action 记 ActionResult{success,message})
   │    execAction 按 action.tool 分发：
   │      update_requirement_field → 直接改 requirements 字段(+version 递增)
   │      request_retrieval        → 补检索一次 → 落检索快照
   │      revise_article_sentence  → 走 service.ReviseSentenceFull (见下)
   │      append_article_content   → 走 service.AppendArticleContent (见下)
   ▼
   返回 DispatchResult{ Plan, Results[] }（供前端逐条展示成败）
```

**两大对话形态**（由前端 target_type 区分，但后端都走同一 dispatcher）：
- 需求单阶段：`update_requirement_field`  对话改需求字段，AI 改完直接覆盖。
- 稿件阶段：改句子(`revise_article_sentence`) / 追加(`append_article_content`)，且在需要查资料时强制先出一个 `request_retrieval`（dialogue prompt 第 4、5 条明确约束"别把检索隐在改写动作里"）。

### 2.3 句子修订 / 追加（实际的生产链路）

这两个动作**没有**走 agent/orchestrator 的 Reviser，而是直接走 `api/service/revise.go` 的成套实现：

```
ReviseSentenceFull(ctx, tenant, workspace, targetIndex, instruction)
  0. EnsureBatchFresh: 需求单 version 与最新检索快照一致?  否则拒绝"需求单已变更请重新生成"
  1. 读当前稿件句子列表
  2. RequirementFileIDScope 拿勾选范围
  3. LLM 重写目标句(rewriteSentenceLLM 单个 JSON {text})
  4. 用 **新句全文** 重新检索证据 (SearchKbaseSentences)
  5. 闸门三：checkRevisionFactSupport —— 新句若含数据断言但无证据 ⇒ 拒绝写入
  6. 落新 ArticleVersion: 被改句=新文本+重检证据; 其余句子继承原文本+原证据

AppendArticleContent(...)  追加段落：链路同构
   - 生成追加内容 → 检索 → 闸门三校验 → 追加为末尾句(带新证据)，原句全继承
```

> 一句话总结修订原则：**只改目标句，未指定句绝不改**；被改句证据重建；改的句子缺数据支撑就拒绝。

---

## 3. Agent 之间的"通信方案"

核心结论：**Agent 之间不是互相自由调度的黑盒，而是通过一套在 `agent/types.go` 里定义的结构化数据契约 + 一个编排器(Orchestrator)按确定顺序传递。** 没有 LangChain/LangGraph 那种自由网络。

具体是怎么通信的：

1. **结构化数据契约（"信号"）**  ——`agent` 包里的 Go 类型即 Agent 之间交换的唯一介质：
   - `Requirement`（需求单的精简视图）
   - `Evidence`（一条证据：file+version+句子锚点+原文+score）
   - `RetrieveResult { Evidence[], Queries[] }`（检索 agent → 编排）
   - `WritingRequest { Requirement, Evidence }`（编排 → 撰写 agent）
   - `Article { Title, Sections[]{Paragraphs[]{Sentences{Text, EvidenceRefs[]}} } }`（撰写 → 编排）
   - `EvidenceManifest`（导出清单）
   - `DialoguePlan{ Actions[] }`（对话 agent → 派发器）
   - `DialogueAction{ tool, field, target, instruction, retrieval_query, needs_retrieval }`（原子动作）

2. **只在两端用 LLM，中间是"人读得懂的程序控制流"**：
   - LLM 只出现在：检索前的 query 提炼、撰写段、对话意图解析（dialogue）、事实校验、句子重写。
   - 检索本身的结果(向量/证据)、句子的绑定关系、证据清单组装，**都不是 LLM 自己"想"出来**的，而是由编排器在内存里用普通 Go 结构传递、由证据整理 agent 纯代码格式化。

3. **编排器 Orchestrator = 唯一"调度中枢"**：
   - 它知道调用顺序(检索→write→证据)，控制分支和失败传播。
   - 它**不让一个 agent 去再调另一个 agent**——比如撰写 agent 拿到的只是"已经检索好的证据数组"，它没有检索能力，也不能自己去追加检索（追加检索的接口在 service 层，只由对话的动作派发触发）。

4. **对话(改动)层是另一条“更贴近 HTTP”的短链**：`dialogue agent → schema 机检 → dispatcher → service(storage) `。这里"通信"本质是**把用户一句话翻译成受白名单约束的动作列表，再由 dispatcher 逐条对数据库执行**，而不是 agent 之间互相讲话。

5. **贯穿审计**：每次调用带全链路 trace_id / tenant / user（middleware 注入 ctx），日志与动作都有审计。

---

## 4. 生成稿件的"边界 case"处理（对应你要的"零编造"）

| # | 场景 | 在哪里拦截 | 行为 |
|---|------|-----------|------|
| B1 | 知识库完全没有该主题数据 | `orchestrator.Generate` (闸门一) | 抛 `ErrNoEvidence` → 前端提示"未检索到相关资料"，不生成 |
| B2 | 需求要 A也有B没有(部分缺证) | `censor.ClaimPlanner.Check` (闸门一) | 逐点核对，把 B 这类缺证点列成**缺证清单**返回，**整篇禁止生成、不写 B** |
| B3 | 稿里写出了证据没有的具体数字 | `censor.FactVerifier` (闸门二) | 抽取数据断言→核对→有无法在证据原文找到的数字 ⇒ `ErrFactUnsupported`，整稿禁止落库，并把无源断言提示给用户 |
| B4 | 需求要"晴天总天数"这种需要统计推导 | 同 B3 (闸门二) | 明细里只有"1/1晴、1/2晴"；"晴天共2天"这条推断被判 unsupported ⇒ 拦截(符合"禁止统计推断") |
| B5 | 语义等价(同义改写) | FactVerifier | **允许**——"年假5天"对上"年休假5日"判 supported 放行 |
| B6 | 纯公文句(无数据断言) | FactVerifier / ClaimPlanner | **放行**——不拦"公司高度重视…"这类不含数据断言的句子 |
| B7 | 修订一句、但该句新内容需要的数据没证据 | `ReviseSentenceFull` (闸门三) | 拒绝写入修订，错误带原因回传给对话面板 |
| B8 | 对话改需求单字段 | dispatcher | 直接改字段、version 递增；字数/勾选范围不在可对话白名单 |
| B9 | 需求单内容变了再做局部修订 | `ReviseSentenceFull` 内 `EnsureBatchFresh` + `storage.IsBatchStale` | 检索快照与需求 version 不一致即拒绝，要求先重新生成 |
| B10 | 生成中途失败(LLM超时/落库失败) | handler | 工作区状态从 generating **回退**到之前的状态(不再卡死)；若此前有旧稿则保留可导出 |
| B11 | 检索相关度低 | `SearchKbase`/`SearchKbaseSentences` (KBE_MIN_SCORE) | 相似度阈值过滤低相关命中，避免噪声当证据 |

---

## 5. 实际 "实现了多少 / 还有哪些没接线" — 实现进度

### 5.1 已经接入生产链路（点击可用）
- 整篇 generation：`orchestrator.Generate` 串起「知识检索 agent →(censor claim 缺证核对)→ 撰写 agent →(censor 事实校验)→ 证据整理」，产出的稿件带句级证据绑定并落库。
- 缺证/编造的**三层闸门**（前述 B1-B6）：已生效，缺证清单、事实校验、纯公文放行都有真实效果。
- 对话修改：需求字段 / 句子重写 / 追加 / 补检索，全部经 dispatcher 落到 DB。
- 检索快照 + 惰性失效(B9)：`retrieval_batches` 落库，防止"需求改了却基于旧证据乱改"。
- 导出：Markdown(正文+证据清单)、导出锁定(generating/revising 时禁导)。
- 后端单元测试(go test ./...),含集成测试（generation/dispatcher/检索/kbase 等）。

### 5.2 存在实现，但**未接生产链路 handler**（可能属于"设计先行已写好代码，只差接线"）
- `agent/orchestrator/reviser.go`（`Reviser` + 句子级重写接口）与 `agent/orchestrator/revision_apply.go`（`ApplySentenceRevision`），以及 `writing.Writer.RewriteSentence`：**当前主链路修订实际用的是 `api/service/revise.go` 的 `ReviseSentenceFull`/`AppendArticleContent`/`rewriteSentenceLLM`**。这两套实现并存且逻辑并不完全等价（service 版+闸门三；orchestrator 版更贴蓝图但未接）。看到本文件时可留意：要继续演进，宜收敛为一条主链，避免双份实现漂移。
- 需求对话 agent 的"补检索"动作目前是**同步 / 只落快照**，并未真的把它检索到的证据喂回去驱动句子修改(batch 口径)，实际追加的逻辑是各自独立再检索一次。
- **generation 里存在"检索跑两遍"的实现细节**：`orchestrator.Generate` 开头会先跑一次 `retrieve.Retriever.Retrieve()`（整篇拆多个 query 检索），随后又跑 `checker.Check()`（按子需求点逐 claim 检索）；当 `checker` 非 nil（生产链路如此）时，**撰写实际使用的是 `checker` 那次的结果，开头那次 Retrieve 的证据会被丢弃**（只在其为 nil 的单元测试分支里生效）。功能上最终证据口径正确（按 claim 逐点核对更严谨），但一轮检索属于冗余调用，后续若优化可去掉开头那次或让其与 claim 检索合并。

### 5.3 暂未实现 / 能力边界外的
- **跨文档聚合统计**(如 366 天 → 晴天天数 / 求和) 按你的要求**明确不做**：系统不自行统计估算，只认"直接检索到原文"的证据；这类要么用户预先准备汇总文档，要么被 B4 拦截并提示。
- **claim/证据映射的持久化**：当前闸门一/二的结果是**运行时内存态**，缺证清单会通过接口提示返回，但没有把 "claim→evidence、某句 unverified" 这类结果落成一张表(仅快照批次落库)。如需导出审计/回溯历史稿件的无证句子，需再补一张持久化表。
- 旧版本稿件证据比对 / "该资料有新版本"提示(二期预留字段)。

---

## 6. 技术要点清单（帮助快速看懂代码）

- 编排核心：`agent/orchestrator/orchestrator.go`(`Generate`)、`agent/types.go`(契约)。
- 证据检索：`api/service/kbase_search.go`(切片/句子检索+阈值)、`api/service/retrieval.go`(快照+批次+惰性失效)、`api/service/kbase_searcher.go`(对话/claim-planner 用的 Searcher 适配)。
- 事实合规：`agent/censor/censor.go`(claim 拆解)、`agent/censor/factverifier.go`(事实断言校验)。
- 对话：`agent/dialogue/agent.go`(意图→DialoguePlan)、`api/service/dispatcher.go`(派发+exec)、`agent/schema`(动作 plan 机检)。
- 稿件落库：`api/service/generation.go`(PersistArticleSnapshot)、`api/service/revise.go`(ReviseSentenceFull/AppendArticleContent + 闸门三)。
- 检索实现：`storage/`(mysql/qdrant/redis + model)，`splitter`(切片)。
- 边界状态恢复：`api/handler/article.go`(restoreWorkspaceStatus，失败回退)。
