# 稿件协作多 Agent 化重建方案

> 标题主张：把 content-hub 从「单程 workflow + 名字化 agent」重建为**「计划—执行—验证—裁决—修稿」的协作式多 Agent 稿件生产系统**。
>
> 版本：v1（RFC / 评审稿）
> 作者：@WWaynee
> 状态：待评审（尚未进入代码改造阶段）
> 关联既有文档：`docs/explain.md`、`docs/architecture/architecture.md`、`docs/architecture/multi-agents.md`、`docs/plan.md`、`docs/features/features.md`

---

## 0. 为什么需要这份重建方案（motivation）

### 0.1 当前的客观代码事实（先诚实承认现状）
通读现有实现后，能力边界与"多 Agent"话术之间存在明确落差，面试/评审中被挑刺是必然的：

| # | 现状（代码级证据） | 文档声称 | 落差 |
|---|--------------------|---------|------|
| C1 | `agent/retrieve/retriever.go` 仅 79 行：一次 `ChatWithJSON` 提炼 queries，再 `for` 循环单次检索去重，**无任何 ReAct / 多轮决策** | architecture.md 称检索 agent 为"ReAct 自主多步检索" | 无自主，纯函数 |
| C2 | `agent/writing/writer.go` 一次 `ChatWithJSON` 生成整稿 JSON | "稿件撰写 agent" | 单步 prompt，无状态 |
| C3 | `agent/evidence/builder.go` 仅 43 行纯格式化遍历 | "证据整理 agent" | 不含智能，纯函数 |
| C4 | `agent/orchestrator/orchestrator.go::Generate` 硬编码顺序 `retrieve→check→write→verify→build` | "Orchestrator Agent" | 确定性 pipeline，无分支自治 |
| C5 | `agent/orchestrator/reviser.go` + `revision_apply.go` 存在**但未接 handler**；生产修订实际走 `api/service/revise.go::ReviseSentenceFull`/`AppendArticleContent` | explain.md §5.2 自我承认 | 双份实现、死代码漂移 |
| C6 | 生产链路 `handler/article.go::GenerateArticle` **同步**直调 orchestrator；其中先跑一次 `Retriever.Retrieve()` 又被 `ClaimPlanner` 的检索结果覆盖丢弃 | explain.md §5.2 自我承认重复检索 | 冗余浪费 |
| C7 | 每租户隔离实为**单 Qdrant collection `content_hub_kbase` + payload `tenant_id` 过滤**（`storage/qdrant.go::searchFilter`），非"每租户一 collection" | architecture.md §6.3 称"每个租户一个 collection" | 文档与实现不符 |
| C8 | 对话 `dispatcher` 对四条 tool 的 action **逐个直接同步执行并落库**，无计划预览/确认，无跨动作的会话状态机 | multi-agents.md §10.2 承认"一期无预览直接执行" | 少一个"判断是否该执行"的节点 |
| C9 | 前端 `WorkspaceDetail.tsx` 稿件对话固定 `target_type:'requirement_field'`，稿件阶段无句子级点选+改句的完整 UI 流 | features.md §6.3 设想的"选中句子→上下文→AI 改" | 前端未实现完整改稿交互 |
| C10 | `generate`/`chat` 是同步 HTTP request（axios timeout=120s），无异步任务 + 进度/回放；worker 只消费 `document_parse` | db.md 预留 `article_generate` 任务 | 生成阻塞、无中间态上报 |

> 结论：项目里唯一**真正具备 ReAct 循环形态**的是 `agent/qabot/qa.go`（`{"action":"retrieve"}` / `{"action":"final_answer"}` 迭代 + 纠错回喂 + observation 回喂 + MaxRounds 上限）。但即使这个，它也只有"检索↔作答"两态，且无跨会话持久化的 agent trace。

### 0.2 重建要解决的根本命题
> **把一个"必须逐句可溯源、禁止编造、且允许用户反复对话改动"的写稿系统，做成真正由 Agent 自治编排、每一跳都在数据库/向量库/稿件版本上留下可审计副作用、并能在不确定性中自主转向（改 query / 降级表述 / 报缺证 / 请求人工确认）的协作系统。**
>
> 判定 Agent 与 workflow 的分界，我采用三条可验证标准（下文反复用它们自证）：
> - **A1 自治分支**：执行到中途存在"靠当前结果才知道走哪条路、无法源码预算出分支全集"的决策点；
> - **A2 持久副作用与状态**：每个决策/动作落到 DB/消息/稿件版本，形成可回放、可审计的会话 trace，而非瞬态内存调用；
> - **A3 可转向与可终止**：agent 在证据不足时能**主动改换策略**（换 query、降级、转交人工确认），并受硬性 max-step / 预算 / 熔断约束，而非"失败即抛错退出"。

现状的稿件主链路 A1/A2/A3 全部不满足；重建后应至少让**核心的稿件生产链路**同时满足 A1+A2+A3，再逐步把对话改稿、问答统一到同一套 agent 会话框架下。

---

## 1. 方案总览（TL;DR）

### 1.1 一句话
在生产链路里引入一个**持久的"稿件协作会话(agent run)"**，由**一个轻代码、重策略的 Orchestrator 状态机**去驱动**多个"真正由 LLM 决策 + 确定性工具背书"的专职 agent**，agent 之间通过**落库的 WorkPack/Artifact(结构化产物)** 接力，并由一个**非 LLM 的 Verify 层(规则 + 阈值 + 向量)** 对每一步产出做硬校验；任何一步失败都"软着陆"到下一可执行策略，而非整条 workflow 报错退出。

### 1.2 它为什么不再是"函数堆砌"
- **新增了决策节点**：`Planner→Retriever→Guardian(裁决)→Writer→Verifier` 的顺序**不是编译期写死的**，而是由 Guardian 依据证据/校验结果在每次 run 运行时决定"下一步派给谁"，并持久化为 run 状态（A1）。
- **agent 会真正破坏既有产物来重写**：Writer 每次产出都基于上一次 run 的稿件版本增量演化，Verifier 打回会触发 Guardian 重新分派（改查还是降级还是停下问用户），这是"对真实资源的副作用 + 循环"，不是一次到位（A2, A3）。
- **verify 是"规则优先、LLM 兜底"**：抽取 assert → 用数字归一 / 原文片段 / 阈值做**确定性**校验，LLM 只做语义近义兜底，返回的是带证据引用 + 可解释的分级结论，而非让 LLM 自说自话 bool（堵住 C 号 的单点信任漏洞之一）。

### 1.3 三层落地节奏（避免一次推翻、可控灰度）
| 阶段 | 交付 | 关键节点 | 可回归守卫 |
|------|------|---------|-----------|
| P1 骨架化 | 引入 `agent/coordinator`(run 状态机 + 落库 trace) + `RuleVerifier`(确定性校验) | orchestrator 重构为真状态机 | 现全部 `go test` 绿的基线上做 |
| P2 Agent 化 | Retriever 升格为带 budget 的多轮；Writer 支持增量写与带证据字典约束 | Guardian/裁决上线 | 新增单测 + 保留 integration |
| P3 放开 | 稿件 stage 改成**异步 agent run + 前端 run/step 进度回放 + 改稿对话实体化** | MQ `article_generate` 真消费 | 冒烟 + 隔离对抗 + 导出快照回归 |

> P2/P3 之所以可行，是因为当前已有"`article_version` 快照 / 句级绑定 / retrieval_batch / conversation_messages(存 DialoguePlan JSON)"这套可增量演化的地基——改造不是推翻，而是为已有地基"接上一套会跑循环的骨架"。

---

## 2. 目标架构（数据契约与 run 生命周期）

### 2.1 引入"协作会话 / agent run"作为一等持久实体
`docs/architecture/db.md` 目前没有承载"一次稿件生产过程"的表；`article_version` 是**完成态快照**，`conversation_messages` 是无状态的对话流。中间过程(规划/检索了几轮/校验打了几个点/谁的哪个动作导致这个版本)无法回放——这正是评审会问"你的多 agent 到底跑了什么、每一步证据是什么"时答不出来的根源。

新增大致如下持久结构（表名可再定，此为字段主张）：
- `agent_runs`：一次稿件生产会话。字段主张：`id / tenant_id / user_id / workspace_id / run_type(initial|revision|append|regenerate) / base_article_version / plan(结构化JSON,含子需求claims) / status(running|awaiting_human|success|failed|cancelled) / total_steps / finished_steps / max_steps / created_at/updated_at`。
- `agent_steps`：run 内每一步。字段主张：`id / run_id / step_no / role(planner|retriever|guardian|writer|verifier) / action(如 retrieve/triage/write/verify) / decision(text) / successor(下一步角色,由Guardian填) / outcome(rejected|accepted|raised_flag) / created_at`。**带上 `successor` 即证据"这步是谁决定派给谁的"。**
- `agent_evidence` 复用/扩展当前 `retrieval_batch` → 直接挂到 `run_id`，使"本轮检索依据哪一版需求"可定位。
- 可先不新增表、仅用 **`conversation_messages` 现有 JSON 扩展**跑通 P1，再用新表固话——但为了让"run 可回放"成立，P2 起必须固化。

> 这一层回答评审："Agent 每一步决策是瞬态的还是可回放的？"→ 可回放、可审计（A2）。

### 2.2 Agent 角色划分（重建后）
保留并发扬"只在该决策需要 LLM 自主时才叫 Agent"的态度，明确区分**Agent(从事决策)** vs **Operator/工具(确定性执行)**：

| 角色 | Agent / 工具 | 职责 | 状态/自主边界 | LLM 用在哪 |
|------|:---:|------|---------|:---:|
| **Coordinator/Orchestrator** | 状态机(代码,事务推进) | 驱动 `run` 生命周期、派发给各专职 Agent、记录 `agent_run/steps`、拦截并发(同一工作区同一时刻只有一个 run)，决定"询问用户"或"失败退出" | 无 LLM 决策,只做确定性迁移 | ✗ |
| **Planner** | Agent | 把需求单(表单+章节要求)拆成 `Claim[]`(needs_fact 粒度) 并存为 run.plan | 决策"拆成哪些点、哪些必须事实支撑" | ✅(提炼/拆解) |
| **Retriever** | Agent(带工具) | 对每个 claim 做**可多轮 check-rewrite-verify 的内部循环**，失败时降级/换 query，产 `Evidence[]` | 决策"此 claim 的检索是否足够、要不要换问法重试" | ✅(query挑选/判断是否够) |
| **Guardian(裁决)** | Agent | 读 `plan + cover 摘要 + 上一步产物的校验分级`，**裁决**下一步：`accepted` → 放 Writer；`ask_human` → 停等用户(走现有对话/人工补料)；`retry`(要求 Retriever换角度) → 回派 | 决策"证据覆盖是否可写作 / 缺证如何妥协" | ✅(裁决/判断) |
| **Writer** | Agent(受约束) | 依据"见证例(evidence) + Guardian 通过的 claim 边界"写出句级绑定的增量稿;相对目标任务**只增改被指派句**,输出结构化 `ArticleDraft` | 决策词语/章节组织是自由的;但"新建的数据断言必须回指 evidence"。越出则被 Verify 拦 | ✅ |
| **RuleVerifier** | 工具(确定性,非 Agent) | 抽取 Writer 产出的断言(可由 LLM 先辅助抽取)、用**数字归一/原文匹配/阈值/边界约束**确定性核对;对纯语义才二次 LLM | 不自由裁决,规则优先;不被单一 LLM 自评牵着走 | ◐(兜底近义)
| **Human** | 边界 | `ask_human` 时,用户经对话面板给出"确认"或补充范围/资料 → Coordinator 转 Guardian 重排 | 关键行为(缺证妥协/高危改句)需要用户盖章 | — |

> 关键 self-check:Writer 不是"一个连工具调用的壳"。它的**决策范围**(先写哪一章、用哪些证据)与**被验证路径**(每个断言回指)之间有真实的来回;Retriever 因为"覆盖不足会不会被 Guardian 打回"被迫真的去换 query。循环是"因为结果不好而再试",这正是 A1/A3 命根。

### 2.3 一篇稿件从「初稿」到「发布」经过的 run 序列
```
                 ┌───────────── Worker/HTTP ◄─────────────┐
                 │        trigger(initial|regenerate)     │
  用户填需求 ─► Planner ─► (retrieve* M轮) ─► Guardian  ──┤
        ▲                                       │裁决      │
        │                 accept(证据够)         ▼         │
        │                           Writer(写/改稿)         │
        │                                    │            │
        │                            RuleVerifier◄────────┤
        │                            │ accept / flag      │
        │                重query ◄- retry                │
        │                            ▼  accept             │
        │                         落 article_version       │
   └────┴──── user 在稿件面板发对话 ─► dialogue Agent ─► run:revision(repeat以上, 但 base= 该版本)
        │  (改某句 / 追加 / 补料)
        └───────────────────────────────────────────────────┘
```
- `Run` 每次结束都生成一个新的 `article_version`，保证"可导出快照"与"可回放过程"解耦。
- 同一 workspace 同一时刻**只有一个活跃 run**(Coordinator 上锁),杜绝多次 generate/改稿并发写坏文章。
- dialogue(对话)不再是"独立走 DB 的小夜曲",而是与 run 共用同一个 coordinator / dispatch,让"改句子"同样进入 `run:revision` 跑闭环。彻底砍掉 C5 的"service 版 vs orchestrator 修订版"双实现——统一到 coordinator。

---

## 3. 关键子部分与决策依据

### 3.1 RuleVerifier:先规则的"真校验",而不是只信 LLM bool
**问题(对照现状 C7/C 缺)**：`agent/censor/factverifier.go` 现在让 LLM 在输出里自行给出 `supported/evidence_idx`,作为系统"禁止编造"的最后闸门;`checkRevisionFactSupport` 里 LLM 失败默认放行。这让真伪校验本身仍是黑盒单点。评审会问:你凭什么相信模型报的 supported 是真的?

**重建方案**:
1. **断言抽取**:对 Writer 每句,用一次 LLM 把"事实/数字断言"提出来 → `assertion{text, kind(number/date/percent/term/name), normalized_candidate}` 并存结构化 JSON。这一步仍靠 LLM,但只是"提出要查的断言",不参与判定。
2. **确定性核对(任意一条命中即 supported)**:
   - 若证据原文**字符域包含**或**词集高覆盖**该断言 → supported;
   - 若 kind=number/percent/date:做**归一比对**(如 "15%" vs "百分之十五" / "2019年" 别名),一致 → supported;
     - 统计/推导(由明细求和得结论)按**禁止**处理→ unsupported;
   - 若命中分数低于阈值 / 在候选证据里无近似 → unsupported。
3. **仅当规则无法判定**(纯语义等价、"年休假5日"≈"年假5天")才降级为 LLM 近义阅卷并标注 `confidence=low, 需二次交叉(可再抽一个别的句同证据对照)`。
4. **结论含 reason + source 锚点**,不止 bool;`ArticleDraft` 依赖它做证据回填。

> 这把"零编造"命题从"这条 prompt 管不管得住"变成"规则拦得住的情况一定拦;拦不住的,明确归为准语义并留人审"。可在此加集成测试反例(curated):给一条含"15%+统计求和"的稿,断言 RuleVerifier 会拒。

### 3.2 Retriever:给它真正的 round(带 budget)与"要不要换 query"
现状 retrieve 一轮就结束、覆盖不足直接当缺证(假阴性多)。重建后:

```
func (a) RetrieveClaim(claim, scope, budget) (respEvidence, quality, errOnBudget?) {
  queries = a.thinkQueries(claim, priorHits=∅)
  for step in 0..maxRetrieveBudget {
     hits = tools.search(queries[step], scope)
     quality = a.assessCover(claim, hits /*含: 已覆盖几类需求、缺哪类、趋势*/)
     if quality.sufficient for claim: return hits
     if quality.exhausted   : return hits /*不足但不该硬编造*/
     queries = a.thinkNextQueries(claim, {之前的空/低分query集})  // 换角度而非重复
  }
  // 超 budget:把"还有哪些子角度没补到"作为 observation 交给 Guardian
}
```
- 配 `maxRetrieveBudget`(config)与 ctx.timeout,防空转(承接 multi-agents.md 已有 RPO/预算思想)。
- Retriever 的输出 + Guardian 把"我们尽力补了但某子角度无源"降为“可写定性/或 ask_human”--> 直接解决 B2 那段"整篇禁止生成太硬"的可用性问题。

### 3.3 Guardian 三态裁决(替代"失败即抛错"的硬退出)
现状 `ErrNoEvidence / ErrInsufficientEvidence / ErrFactUnsupported` 都是**直接中断 + 返回 message**,无法让用户补一手资料就接着写。Guardian 把结局改为:
- `accept` → 派 Writer;
- `retry` → 回派给 Retriever(换角度/换范围);
- `ask_human` → run 进入 `awaiting_human`,把"缺哪个子角度 / 哪条数据没源 / 倾向怎么降级"作为一条**待确认任务(PendingDecision)** 写给用户(可走对话面板),用户在知识库补料或给出"可降级为定性/删除该句"的指示 → run 恢复重排;
- 仅当用户放弃或预算尽才 final fail —— 失败态也要能在 UI 显示"卡在哪一步、为什么"。

### 3.4 Writer 只在被授权范围内动笔
为支撑"只改被指句/被追加段、未动句继承证据、版本快照不脏",Writer 的输入是一个**明确的作用域**(whole draft / 指定 sentence index / 指定 append pos),输出 `ArticleDraft`(diff + 新证据 refs)。Coordinator 用现有 `applyArticleRevision`/`persistArticleSnapshot` 语义落库(保留未动句继承)。关键约束:
- Writer 不得触碰未授权句(硬校验:diff 集合⊆授权序列)。
- 新增/改写的每句数据断言必须回指证据。不在证据内的点 → 走 3.3 的降级/问人。

### 3.5 异步 run + UI 进度回放(把"生成几秒同步 HTTP"变成"可看的天梯")
- 复用既有 `mq` 与 `agent_tasks` 意向:`article_generate` 队列真正被 worker 消费,`run` 作为任务本体。
- 新增 REST:`POST /workspaces/:wid/runs`(启初稿/重生成)、`GET /workspaces/:wid/runs/:rid/steps`(前端按 step 轮询)、`POST /runs/:rid/decision`(用户针对 ask_human 的 PendingDecision 确认/否决)、`DELETE /runs/:rid`(取消,做幂等停止)。
- 前端把当前 loadArticle→generate→一下午 loading 改成 `RunProgress` 面板:显示 current step/ role/子进度,并支持"卡在 ask_human 时直接补意见" —— 这就是 features.md 里"生成过程可视化+对话改需求"该有的样子。

---

## 4. 组件改动清单(逐文件定位,便于落地)

> 以现状 commit 为基准。这一段用于回答评审"到底改哪些文件、怎么保证不倒退"。

### 4.1 新增
- `agent/coordinator/`：`run.go(状态机)`、`runner.go(编排 execute 顺序 / 阶段锁)`、`store.go(run 落库)`
- `agent/verifier/rulebased.go`：确定性校验 + 阈值 + 归一化;`extract_llm.go`(可选抽断言)
- `agent/planner/` 可并入现有 `agent/censor` 的拆解逻辑改用 run.plan 持久
- `api/handler/run.go`、`api/service/run.go`：异步 run 接口 + PendingDecision 处理
- `web/`：`RunProgressPanel`、工作区文章 Tab 的 run 卡片、改稿对话把 target 完善成 sentence 锚点 UI

### 4.2 重构/对齐
- `agent/orchestrator/orchestrator.go`：从"同步 `Generate` 一次串完"改成"receive run id → 跑 guard/plan/retrieve/write/verify → 逐 step 落库"；砍掉里面 C6 的重复首检(直接以 claim→retrieve→guardian 为准)。
- `agent/retrieve/retriever.go`：multi-round + budget(3.2)。
- `agent/writing/writer.go`：增量/作用域化输出;复用现有 `Article` 结构 + evidence_refs。
- `agent/evidence/builder.go`：保留纯格式化;明确标记为 `non-agent helper`(名实相符,README/explain 同步更新)。
- `agent/dialogue/agent.go`：对话产出 Plan 里的 append/revise/补料 action 改投`coordinator` 来跑 `run:*`,不再各自 `dispatcher` 落 DB;统一 schema 机检。
- `api/service/revise.go` + `api/service/generation.go` / `dispatcher.go`:改由 coordinator 统一调度;删除 or 明确弃置 `agent/orchestrator/reviser.go + revision_apply.go`(二选一收敛,消除 C5 双实现漂移)。
- `storage/qdrant.go`：确认并文档化"单 collection + tenant payload"，或若测明需要扩到多 collection,再改;至少把 architecture.md §6.3 校准为与实现一致,避免文档欠账(C7)。
- `cmd/worker`：新增消费 `article_generate` 队列执行 run。

### 4.3 测试与守卫
- 单元:pure RuleVerifier(curated 反例:统计求和/近义/纯公文)、Retriever 循环栈depth guard、run 状态机合法迁移状态表,可无外部服务。
- 集成(延用 build tag):一次真实 run 端到端(plan→retrieve→guardian→write→verify→落一版) + 多跑并发同一 workspace 被锁的拦截。
- 既有 `go test ./...` 与 `scripts/smoke_e2e.sh`(多租户隔离对抗/导出快照)须保持绿;用 `article_version` distinct 快照断言"每 run 正好 +1 版本"。

---

## 5. 取舍与决策点(希望评审拍板)

| # | 决策 | 选项 A(倾向) | 选项 B | 影响 |
|---|------|--------------|--------|------|
| D1 | run 实体靠新表 vs. `conversation_messages` JSON 扩展 | P1 用消息扩展跑通, P2 新表固话 | 一步到位建 agent_runs/agent_steps | 上线速度 vs 回放严谨度 |
| D2 | generate 是否本月就异步化(MQ 真消费) | 异步 + 前端打进度 | 保留同步但加前端"预计生成中" | 复杂度 vs 体验 |
| D3 | 旧 orchestrator/reviser 双实现 | 删除、收敛 coordinator | 保留二套路径(不推荐) | 删除需迁好 集成测试 |
| D4 | 多 collection 隔离 vs 单 collection+payload | 校准文档一致(status quo) | 按租户真拆 collection | 运维 vs 隔离强度 |
| D5 | `RuleVerifier` 对"纯语义近义"降到多低置信才转 LLM | 规则查不到就转 LLM + confidence 标注 | 一律只允许字符/归一命中 | 准 vs 老别死不拦 |

> 我的默认推荐行(靠上表默认)已写进正文;实施前应在 issues 里逐项开票确认。

---

## 6. 面试/评审演示口径(回答"你还是不是 workflow")

评审若问"这不还是把 workflow 拆成 agent 吗",按以下三点回应并**现场指代码**:
1. **有不可预写分支**:Guardian 的 `ask_human / retry / accept` 取决于"证据够不够、该句能不能降级",而这些判据在用户还没补料/没授权前是未知的——分支全集不在 compiler 期可见。
2. **有副作用与状态**:每一步落在 `agent_run / agent_step / article_version`,可回放、可审计;不是瞬态函数调用。
3. **有失败即决断而非抛错**:证据不足不会直接红(旧 `ErrInsufficientEvidence`)——而是可以换角度重检(G6)→ 仍不够就只能 ask_human 等待用户(而不是拒绝)。这是"人会怎么做"而非"管道会怎么做"。

并把 qabot(唯一现成的 ReAct)与新 run 结构对照,说明方向是把好的自环能力**前移到稿件主链路**,而非只做客问答。

---

## 7. 尚未包含 / 二期不做的(诚实边界)
- 不做把整篇写作变成"自由对话式共创"(每句都要问一遍太慢),仍保留"一次成稿 + 局部修订 + ask_human 只在关键的缺证点"。
- 不做**真正的工具自由函数集**(HTTP/浏览器/第三方),那对"政企可信发布"弊大于利;agent 工具只暴露 `query_kbase / read_document / assert_via_rule / cover_score / emit_draft` 等封闭能力。
- 不做跨 Agent 的任意消息网;固定到 run 这一步的 successor 序列,由 Guardian 决定下一步"谁"(有限后继),保证可测。
- P1/P2 建议以单测/服务测试先回归,集成在配置齐全的环境跑;冒烟+隔离对抗作为后续 gate。
- 硬件/外部依赖(OSS/LLM/Qdrant/MySQL/RabbitMQ)这轮不更换厂商,只补异步消费与健壮边界。

---

### 附:本方案据以立足的代码快照(commit 基线上核)
核对以下文件(与表格 C# 一一对应)以保证重建不偏离代码:
- `cmd/worker/`, `cmd/api/`, `api/router.go`(路由)
- `api/handler/article.go`(同步 generate + 双检索 C6)
- `api/service/{generation,revise,dispatcher,retrieval,qa,conversation,export,health}.go`
- `agent/{orchestrator,retrieve,writing,evidence,censor,qabot}/`
- `storage/qdrant.go`,`storage/model/*`
- `agent/orchestrator/reviser.go`,`revision_apply.go`(C5 死代码)
- `web/src/pages/WorkspaceDetail.tsx`(C9 target 固定)
- `llmclient/*`(HTTP + 指数退避 + 熔断,在异步 run 中需复用其 ctx 超时)

---

# §8 rev-2 · 面向「真实使用场景」的立场修正与体验重建

> rev-2 是一次**方向性补记**(不是小补丁),目标是回答我们自己最该问、却没在 rev-1 里问够的一层:
>> **这套 Agent 化落地后,真的让每天在用的政企普通文案过得更好吗?还是我只是造了一个"技术上很 Agent、用户却不想看"的机器?**
>
> rev-1 的默认姿态(第2/3章)把"计划-执行-验证-裁决"这一整条**过程摊到用户面前**:run/step 进度、Guardian,尤其是`ask_human`被设计成"用户一条条看缺哪些点、再给某句降级还是删"。这对内容评审足够,但对**普通政企文案是一次认知灾难**——他要的不是在旁边看 agent 怎么思考,而是"我说话→我要的稿子对、句子有出处、要改某处就改一处"。
> rev-2 据此把整个方案的用户体验姿态从 **process-first(过程外露)** 修正为 **people-first(体验第一、过程内收)**。技术结论(多 Agent + 顽固可审计 run)不变,但它必须**收到后端跑、只把「需要人拍板的决策」以一句话露出**,而把绝大多数产品功夫放在"稿件长得像公文/能舒服引用/能舒服改"上。

## 8.1 一句话立场
> **Agent 协作是引擎,不是界面;用户的界面始终是人话 + 一篇可读、可引、可改的公文稿。** 任何"把 agent 调用过程/step 摊成用户操作"的设计,宁可舍弃,也不该让普通文案去对抗复杂度。

---

## 8.2 先回答四记来自真实使用场景的尖锐质疑

### Q1 你是不是把"agent 一次生成"做成用户天天要盯的控制台?
**这是 rev-1 最危险的自嗨。** 答案要对普通用户**全程折叠**:
- 生成 = 点一次"生成/重新生成",期间只给**一句人话状态**("正在查资料…已引用X份材料…正在校验是否有编造…"),对应到 run 的 step 映射成**用户能看懂的话**,而不是"What: retrieve, action: triage"这类内部词;
- 真正的 run/step/evidence 明细全部进**"溯源详情"二级面板 / 导出审计单**,默认收起,供需要核证的人(审核、管理员)点开——**普通作者不被迫理解 agent**;
- `ask_human`:不该是"让你逐条审缺证清单再逐句授权",而是**把待确认收敛成一句用户话术**——"这段需要的数据(KBase 里没有:『2023年演出场次』)。你要 (a) 我补"改成不放具体数 / (b) 你先去资料库里补一点材料我重写 / (c) 放弃这段"。用户只做单选,不接受 agent 工作台。
- **对 test**:那些"ask_human/缺证妥协"的 agent 交互,其实对内容相当少见;用户常见的只是"生成/改一句"。因此 UI 的工作量大部分应投在"稿件本身好看可引可改",而不是 agent 控制台。

### Q2 你说"句级可溯源",但前端最多只能拿一串 `doc_sentence_id`——到底怎么让作者"看懂"出处?
**这是产品价值的最硬的空洞,且是代码级的。** (证据见 `storage/model/article.go` 的 `EvidenceBinding`——只存 `DocFileID`/`DocSentenceID` 两个外键,不冗余来源原文、文档名、章节、版本。)
- 后果:`GET /workspaces/:id/article` 里 bindings 只有两个 id,前端即便想做"悬浮显示原句"也拿不到原文。所谓"可溯源"成了"你看有个(看不见内容的)出处",而非"你看这句话来自《2023年度运营总结》第二部分那句原文"。
- rev-2 修复(数据+接口层,否则前端怎么美都是空壳):
  1. DB 侧在写 binding 时把来源切片原文、文档名、章节标题、版本 md5 **冗余进一个带 json 的导出结构**(或对 `doc_sentences`/`doc_versions` 做一次性联表);关键是在 `GET article`/导出/证据清单里**返回人可读的 source**:`{source_text, file_name, dir_name?, chapter_title, version_md5}`。
  2. 给出**纯公文(无源)句**也要能显示"本句为 AI 通稿语,无引用"的区分态,不能混进有据句的 tag 里。
  3. 导出 md 时,证据清单里每个句子条目都应含**可复制的原文引文**(这正是产品卖点该兑现的地方)。

### Q3 政企文案的日常真在"让我一句一句改 AI 稿",而不是"全程不让手编、只准 AI"——你这个"禁止任意字符编辑"是教条吗?
**Rev-1/原 features 都写死"禁止任何手动编辑"。我倾向推翻它,改为"人机混合写作"。**
- 理由(使用场景):对政企使用者,拿到 AI 初稿后高频是**大段拼接、删改、替换、套自家公文体**。死死锁成"只能 AI 改",让核心价值里最日常的"批量把人稿合进来"反而没法用,是产品为了"溯源干净"而牺牲了真实生产。这不是"为了 Agent 而 Agent",是**与目标用户的能力冲突**。
- rev-2 的分层主张(**人可编 + 引用治理不退出**):
  - **直接编辑开放**,且前端支持段落/整句的普通文本输入;
  - 任何"新增/修改句"由系统跑一次**轻量 Revision-run**(就是现有 `run:revision` 那条改句链路)做"这句现在是否需要证据、有没有断言、命题是否新出数据断言",给**三态**:仍有据 / 变无据需人工标注(给个黄点提醒) / 纯措辞无需据;
  - 因此"可溯源性"从"禁止手编所以一定来自AI"变为"**即使人工改动,系统也持续告知你哪些句子证据失效/待复核**",这才是更可信也更实用的模型。
  - 风险评估:代价是修订不再"全自动干净",换来的是真实把"人写的稿子"融入并持续治理;若要保底,可选择"工作在 AI 生成版本上但在处理中给 diff 与证据漂移提示"。

### Q4 稿件长得像一堵预格式文本墙——用户怎么会知道能不能信、谁引了什么?
诚然。rev-1 把"可信"做成了后端判定,却**没有在渲染层变成"看得懂的引用与被引用"**。rev-2 专门给前端稿件体验开一整章(§9)。

---

## 8.3 rev-2 对 rev-1 的结构性改动清单
1. **把 agent 协同全程收到后端 run 与 `溯源详情`面板内**(第2/3章的演进,默认折叠),用户主界面绝不展示 step/role 内部词(除管理员/审核模式)。
2. **数据/接口补"人读 source"**:binding 关联交出 `source_text / file_name / chapter_title / version`,端到端(DB→API→tooltip→导出审计单)都能体现"这句话来自哪份原文"。
3. **推倒"禁止手编"教条,改成"人机混合写作 + 证据漂移治理"**,把人工改动纳入 Revision-run 检测无据句,给黄点提醒而不是拒绝。
4. **稿件渲染与交互重做到可读、可引、可改**(§9),这是用户体验与技术价值兑现的主要载体。

> 这些改动**不与 rev-1 的核心判据冲突(A1/A2/A3 依旧),而是把它放回正确位置**:Agent 的自洽循环在引擎里为可信保驾护航;人在界面里只面对"选可信写法的稿子 + 一句待决策的人话"。

---

# §9 rev-2 · 稿件渲染与交互体验重建(直接回应"稿子是一堵文本墙 / 证据硬贴 / 改稿没落地" )

rev-2 判断:**这部分是产品价值能否被看见的主战场**。Agent 再可信,如果稿子长得像预格式文本、证据只是裸 tag、改稿对话没 UI,用户对"可溯源、可协作"的感知就是零。下面按"先可读、再可引、后可改"三层给到能直接落前端的设计。

## 9.1 排版:把"预文本墙"变成"像政企公文"的可读正文

现状 `WorkspaceDetail` 用 `<Typography.Paragraph style={{whiteSpace:'pre-wrap'}}>{article.full_content}</Typography.Paragraph>` 一股脑贴出整块 Markdown 字符串,标题/段落/列表全糊在一段里,无法看。(§-code 证据见该文件 `article.full_content` 整段渲染)

- **直接渲染结构化 Markdown**,而非字符串:复用 `agent.Article{title, sections[{heading, paragraphs:[{sentences}]}]}` 结构(DB 里 `article_sentences` 已带 section/paragraph/sentence_index),把**结构在 DB 端就从 full_content 与 sentences 里一起返回**;前端做真正的 `markdown renderer + 标题层级 + 段落间距 + 首行缩进`,而不是 pre-wrap 一坨。
- 字体/字重按政企语境:正文用可读衬线/黑体,标题分级(`## 章节` →视觉层级),段间距、行高、标点避讳优化;可选"公文样式预览"(红头/落款)但一期只保证"像篇能看的文档"。
- 提供**打印/导出与屏显一致**的预览,避免"屏上没排版、导出才突然像样"的割裂。

## 9.2 证据/引用呈现:不是 tag,是「句子内联的出处提示」

> 想清楚关键洞察:**证据不是稿件的旁注,而是句子说法的"脚注/来源"**。最好的形态是贴近原文、随句浮现、默认收敛、可点开成清单,而不是把来源文本硬接在句子后堆成墙。

数据前提(已在§8.2 Q2 建立):`GET article` 必须返回**每句的人读 source**(`source_text/file_name/chapter_title/version`),而不是 `doc_sentence_id` 裸指针。没有这一步,下面一切 UI 都是空壳。前端拿到后:

- **有据句**:在句尾给一个低调小标(如与上标 n),鼠标 hover/点击**浮现 tooltip/气泡**,内容是:
  - 该句出现的断言写法 → 一条或多条来源,每条含**原文原句引文**($§ source_text)、文档名 + 章节 + 版本;**鼠标失焦/移开自动收起**,左下/右下角给"复制引文"。
- **无据(纯公文通稿)句**:句尾用一个极淡的"语句"(比如细灰点,不打扰),hover 提示"AI 通稿语句,无外部引用";不与有据混淆。
- **强制清单态两种呈现**:
  1. 页面可一键"只看带断言句与证据"(`证据密度视图`):每个断言句折叠展开其 source 清单;
  2. 导出仍含 §7 的尾部证据清单,但清单要**逐条可读、含原句引文**,不是空引用 id。

## 9.3 改稿:把"点句子→上下文→AI 精改这个句子"真正做出来

> 现状前端没有:稿件阶段连对话面板都固定 `target_type:'requirement_field'`,更不用说"选句改句"。(§-code 见 `WorkspaceDetail` `sendChat` 里硬编码 target_type)

rev-2 前端目标交互(reuse rev-1 的 `run:revision` 改句链路):
1. **正文任一句可"悬停高亮 + 右侧弹出操作"**:「改这句」「追加到句后」「标记无据(人工确认)」「查看证据」。
2. 点「改这句」→ 唤起一个**就地输入框**(不是把用户丢去一个泛泛对话框),prefill 现有 `<sentence target_ref+index>`;**提交走 `POST /workspaces/:id/runs {run_type:revision, target_sentence_index, instruction}`**,后端 rev-1 的 RuleVerifier/Guardian 给你回:整句回来了、证据还在/还是要降级/无据漂移——都**以同样 tooltip 交互就地刷新该句**,而不是整页刷。
3. 「追加到句后」→ 同样地就地追加;总得保留"还能全文重生成"入口。
4. 所有这些仍然**触发同一个异步 run**,后端自洽;前端只做"句子级就地 diff + 状态气泡",把 agent 的过程藏在一个小 spinner/一句人话里。

## 9.4 使用动线:解决用户"到处是功能不知干嘛"
- 工作区详情页保留"【需求】|【稿件】|【溯源】"三页签,**需求=改你想要什么,稿件=看她/继续谈/导,**溯源=核证清单与 run 审计(普通用户默认不进去)。
- 全局把功能收敛成**可控动线**:顶部只有「+新建」「资料库」「我认需求」与右上角账号/退出;任何不明确的入口要么给 tooltip 人话说明,要么干脆不在首屏出现。
- 「稿件」正文默认就有「改一句」「全文再生成」「导出含证据」「溯源/证据密度视图」几颗明确的 action,而不是散落按钮。

---

# §10 rev-2 · 为上述体验落地的增量数据/接口/REST 草图

> 前文 rev-1 已给 run/step 的持久与异步目录;本节补齐让 §9 兑现所需的那一层"人读 source + 结构化正文 + 就地修订"接口约束。

### 10.1 负载结构(request/response 关键字段,避免 "找不到原文就全空壳")
`GET /workspaces/:wid/article` 变更为返回渲染所需结构(除 full_content 快照外):
```
article {
  version, title,
  sections: [
    { heading,
      paragraphs: [ {
        offset,            // paragraph_index
        sentence_views: [ {          // 逐句,前端就地渲染
          sentence_index,
          text,
          claim_type: bound|plausible-ai|no_source|flag_for_review,  // 参见 RuleVerifier 分级
          sources: [ { source_text, file_name, chapter_title, version_md5, doc_sentence_id } ] | [] ,
          needs_human_review: bool, review_note?
        } ]
      } ]
    }
  ],
  full_content,            // 保留给整稿导出/兼容
}
```
- 纯公文句 `sources=[]` 但给 `claim_type=plausible-ai`;RuleVerifier 给 `no_source` 只对"被 flag(疑似该有据却没有)"的句,两者 UI 不同声。

增量接口(沿用 §3.5 异步思路,但约束"逐句就地"):
- `POST /workspaces/:wid/runs`  `{run_type: initial|regenerate}` → 全文再生成,返回 `run_id`。
- `POST /workspaces/:wid/runs`  `{run_type: revision, targets:[{sentence_index, instruction}|{position:'after',index, instruction}]}` → 句子级/追加。
- `GET  /runs/:rid`  +  `GET /runs/:rid/steps`:run 状态与 step 明细(管理员/审核视图用;普通作者只在 UI 拿一个可折叠 spinner)。
- `POST /runs/:rid/decision`  `{decision: override_ok | demote_to_generic | abandon_claim | refill_and_retry}`:对应 Guardian `ask_human` 的单选落地。
- `PUT /workspaces/:wid/article/sentence/:idx/evidence`  `{flag:'no_source', note}`:人工给某句标"无据待核",反向进入治理。

### 10.2 DB 增量(在 rev-1 的 run 表上追加)
- `evidence_bindings` 仍持久 doc 指针(审计用),但 **`article_version` 或新增 `binding_presented` 冗余视图**在导出/展示时带人读 source——避免每次查询一堆 join;最简单:写快照时直接把 `source_text/file_name/chapter` 冗余进一个只读导出列(与 §8.2 Q2 一致)。
- 若 §9.3 要求拿到"解析后的句+段+章节索引",则现 `article_sentences` 的三 index 字段已够;不足的是缺 paragraph_content 级冗余,可在写快照时同存。

### 10.3 别把 rev-1 的 Engine 与 rev-2 的 UI 物理上拆死(保持单仓库落地)
仅建议把前后端改到"同样一次 run、多处消费":
- **run 端到端状态机复用**:一次 `generate`/`revision`/`append` 都是"提交 run→(异步)→step 逐段→成功写新 article_version→UI 刷新该句/整稿"。Agent 逻辑只在 coordinator,UI 永远消费同一批 run/version。

---

## §11 rev-2 · 这么改之后,再自问一遍:还会不会被打脸?
- "你是不是堆了没人用的 Agent?"→ 不会:agent 循环收进 run,UI 主界面是公文稿/sentence 就地交互;普通作者永远看不到 step 词。Agent 的价值由 run 落库审计 + 溯源详情页承接,可点开可证明,但**绝不摊成用户操作负担**。
- "证据又是贴 id?"→ 不会:rev-2 让 `source_text/file_name/chapter` 从写快照那刻就进导出/展示,句上 tooltip 直接给原句引文。
- "AI 稿不能手动改,怎么用?"→ 打开手编 + Revision-run 治理;新增句被检测为"疑似需要据却没有"会黄点提醒,而不是拒绝——真实生产可用,溯源也不退化为教条。
- "稿子没法看/没法信?"→ 按 §9 结构化渲染 + 句内来源 tooltip + 证据密度视图 + 逐句就地改稿 + 明确动线;可信不是隐藏的 tag,而是可悬浮、可复制原句的引号系统。
>
> 归结一句:**rev-2 让多 Agent 从"秀给技术评审看"回到"让政企文案安全地用"—Agent 在后台续命可信,前台是可读、可引、可改的人稿体验。**

---

# §12 rev-3 · 逐条可实施性判定 + 人机混合写作的可落地设计 + 硬伤评审
> rev-3 回答你三个字面的问题:**①我上面讲的每一条,是不是实现后能真落地? ②尤其"人机混合写作",给一份不推翻系统、又能真拆掉"禁手编"教条的可实施设计。 ③再用毒辣的、读过后端+前端代码的眼睛,把仍存的硬伤(技术+体验)一起挖出来并给修复。**
> 写法上先给判定表,再给可落地设计与硬伤,避免把 rev-2 的漂亮话"看起来已实现"。

---

## 12.1 判定表:rev-1/rev-2 每一条,当下代码里"能不能直接落地"

> 判定分三档:**A=只在现有表/接口上加字段与分支,不换交互模型; B=要新增一种提交/落库路径,但仍复用现有 run 与版本快照; C=需要先修一处底层(并发/权限)否则不能安全开。** 没有 C 级之前别宣称"可实施"。

| 条目 | 判定 | 依据(读过代码) | 落地最小起点 |
|------|:--:|------|------|
| §2.1 agent run/step 持久实体 | **B** | 现只 `article_version / conversation_messages(DialoguePlan JSON)`,无 run/step 表 | 先扩 `conversation_messages.kind/tool_result`? 还是新表,见 rev-1 D1 |
| §9.1 结构化渲染(替代 pre-wrap) | **A** | `article_sentences` 已带 section/paragraph/sentence index; `agent.Article` 已在内存 | 前端改写 `GetArticle` 渲染,后端只需加 `sections` 结构 response |
| §9.2 句内 sources tooltip | **B(C?)** | `EvidenceBinding` 只存两外键; `ListArticleBindings` 直接 Find→前端无 source_text→**拿不到原句** | **需先(12.3)`binding→source`冗余/join**,否则是空壳(见原rev-2 Q2) |
| §9.3 逐句就地改句 | **A** | 后端 `run:revision`/`ReviseSentenceFull` 已存在且接 handler; 缺'就地刷新该句'UI | 前端加 sentence 操作 + 提交 revision; 后端改动主要是"把结果回给该句" |
| §9.4 动线收敛 | **A** | 相对独立,先重构侧栏/按钮文案 | 纯前端 |
| §8.2 Q1 归一 human 单选项(ask 收敛) | **B** | Guardian `ask_human` 尚是设计; 现 dispatcher action 是 `request/update/revise/append` | 在 dispatcher 增 `ask_user` 类 decision 即可起步 |
| §8.2 Q2 source 冗余/join | **C** | 见12.3 | 这是 §9.2 前置, 必须先做 |
| §8.2 Q3 人机混合写作 | **C** | 受(12.3)并发与权限限制 | 核心, 见12.4 |

**结论:** 除"source 空洞"与"混合写作"两处为 C 级,其余 rev-1/2 大多是 A/B,可在现有地基上渐次落地,不是推倒重来。但没有 12.3 的修复,§9.2/§10 证据 UI 与 §8.2 Q3 都是"看起来能、其实空"。

---

## 12.2 一句话结论(为下面的硬伤先给坐标)
> 在把"禁手编"打开成"人机混合写作"之前,**必须先解决两个我在代码里确认的基础设施问题**,否则放开手编不是可用性提升,而是引爆漏洞:
> (S1)**检索层没有 owner/scope 权限**——写/列举/删除都按 owner 校验了,但喂给 AI 生成与 QA 问答的**向量检索不打 owner**,私有库内容可被同租户他人经"AI 检索"旁路命中;
> (S2)**稿件版本递增是内存算 `prev.VersionNo+1`,无乐观锁/无 CAS**,仅靠 `(article_id,version_no)` 唯一索引在落库瞬间兜底——单写者+串行请求撞不出来,但放开多人可编辑/并发 revision 后会丢写或浪费一遍 LLM 重跑。
> 这两条是 C 级前置,不修不放开。其余硬伤(含体验)清单见12.5。

---

## 12.3 【C1】检索层可见性硬伤(必须最先修)——`storage/qdrant.go` + `kbase_search`
**证据:**
- `kbase_dir/kbase_file/Rename/Delete/List` 全部传 `scope(public/private)+owner_user_id`;私有库 owner=该 user,公有=0(`api/handler/kbase.go`)→ 文件浏览/写被堵。
- 但 AI 生产检索 `api/service/kbase_search.go::SearchKbase/SearchKbaseSentences` 与 `censor.Searcher`(`kbase_searcher`)只带 `tenantID+fileIDs`;QA 的 `AskQABot→kbaseRetriever.Retrieve`(qa.go)更是**只传 tenantID 不带 owner**。
- `storage/qdrant.go::toPointStruct` payload **没有 `scope`/`owner_user_id` 字段**(只有 tenant/file/chunk/latest/version/content/chapter),```searchFilter``` 只 `tenant_id + latest + (可选file_id)`。

**后果(说人话):** 普通用户 B 不能进 A 的私有库目录看,但他的**AI 问答/稿件生成若不勾紧范围,**把 query 丢向 Qdrant 时,**只过滤了租户,命中了 A 的私有切片也会被回来**(同租户,tenant 过滤放行)。文档说"私有库仅本人可见"被旁路了。这是当前系统**最该先补**的洞,不止体验。
**修复方向(可落地的两条正交防线):**
1. **索引时打上可见平面**:在 `QdrantVector` 与 `toPointStruct` 增加 `owner_user_id` 与 `scope`(公有 owner=0/标记 shared),检索请求显式带 `{tenant, scope∈[public, {scope:'private',owner=me}]}`——**实现要点是把"文件系统那套可见性判定"复用到向量检索。**
2. 检索 token = `tenant+可见scope+owner`;而**勾选fileID仍需再用 `RequirementFileIDScope` 在同可见集合内校验**,若发现请求 fileID 不属于本人/公有可读范围则拒绝。
3. 附录最小校验:用一个**新增集成测试用例**:同租户两个用户,各自私有库;用户 B 的 QA/claim 检索不得返回 A 的私有片(P2 完整做,但至少要能回归)。
   > 这条同时解决"QA default 全租户"这个体验是灾难的合流点。

---

## 12.4 【C2】稿件版本写并发——`storage/article.go` + api 上游
**证据:**
- `model.Article.CurrentVersionNo` 与 `ArticleVersion.VersionNo`;latest= `GetLatestArticleVersion`(按 VersionNo) 。
- 新增/修订先后都在应用内 `VersionNo= prev.VersionNo+1`(`revise.go:86,409`;`generation.go` 取 requirement.version),**没有 `Where("current_version_no=?")` 的 compare_and_swap**。
- 唯一防线是 `uniqueIndex:(article_id,version_no)`——两个并发写同一 version 号会有一个在 `tx.Create` 撞唯一键**,而它通常在花掉整遍 LLM/嵌入之后才发生**,浪费且无法优雅转交。
**后果:** AI-only + 作者单开一篇稿,通常串行、风险低;但 **一旦 rev-2 允许人直接编辑→人人都会在稿件上做编辑→并发编辑/同时 revision 的新常态下**,无锁会成为数据错乱的源头。
**修复方向(C 级、稳、改动小):**
1. 允许编辑 = 前端把修订也**当作一次"对象=Article.VersionNo 的资源",用乐观锁提交**`{base_version_no}`;后端 `UPDATE article SET current_version_no=? WHERE id=? AND current_version_no=base`;CAS 失败返回 409 + 最新版,前端提示"稿件已在你编辑期间被更新,请重载最新版再改"(体验上也最合理,绝不盲目覆盖)。
2. 这样把"唯一索引兜底"升格为"精确的前置 CAS + 可告知用户的冲突语义",放开手编才安全。

---

## 12.5 【混合写作可实施设计】—— 不是"随意改任何字符",而是"人在受控段落里直接编辑 + 每次保存跑轻治理"
> 说明:本节是对 rev-2 §8.2-Q3("推倒'禁手编',改成'人机混合写作'")的**校准与收敛**——Q3 立起的是"人可直接改、而非只能 AI"的方向主张;12.5 把它落到**可分阶段、不推翻系统、且能保住句级治理/版本/source** 的可实施边界(先句/段级受控编辑,前不急着开全稿富文本;要整篇从外部稿起稿则走"导入 skeleton")。下面开始可落地细节。
> 目的:政企文案真正高频的"改一句/调段/套自家用语/把人稿拼接",而不失去痕迹,也不让 AI 全权接管。**不是把整系统变成富文本自由草稿,也不是一刀切禁改。**

### 12.5.1 交互/边界(给前端与后端,能直接做 UI)
1. 稿件区域三个"身份"是并存的,由一段文本当前**是否"脏"**决定:
   - `AI 生成(snapshot)`句:显示来源 tooltip(§9.2);
   - 人工编辑开启后,saved 但未被治理确认 = **dirty(=pending human edit)**,黄点提醒,保留原句 diff;
   - 已跑过 revision(轻治理)承认 = **accepted**,继续给 tooltip/免责。
2. 允许编辑的面:**单句/整段**直接改文本,而不是整稿富文本;这样治理粒度与 `sentence` 对齐(existing `article_sentences` 已经是 unit)。
3. 一次"保存我的改动" = 提交一个 `run{revision, base_article_version, changed:{index,new_text}[]}`,让 §3.4/12.4 的 Writer/Verify 处理这批改动,而**不是把你丢回整稿自由编辑的失控区**。UI 上用户并不感知 agent 内部,只知道"保存 → 系统会检查改动里若引入要据的无源句会提醒我"。
4. 是否保留/降级原句的行为:**保存前就给 preview**:系统会说「此改动仍受原句证据支撑?」或判断「新增断言缺源」,给出保留改动/回退/统一降级三选项——把"禁手编教条"换成**审批式的可放弃**。

### 12.5.2 后端最小路径(改动小,复用)
- 复用现有 `run:revision`/`ReviseSentenceFull`/`AppendArticleContent` 作为载运,但**把输入从"单 instruction 字符串"扩成 "直接 new_text + 所属 sentence_index"**(这就是最简的"人直改"载体);
- 治理(Guardian/RuleVerifier)照旧跑,产出 accepted / no_source(黄点) / request_human 三级;
- 只有 accepted 才把 `dirty` 句刷新成新的 accepted 句与 source 工具提示;no_source/request_human 保留用户改动但展示黄标并允许人工标记 source/no_source。
- 由此,"人改的稿"与"AI 稿"是在**同一条 run+治理管线**,只是来源标记不同(source=manual)。这是一致且可审计的,不是两个世界。

### 12.5.3 “是否该放开全局富文本”要不要做:诚实给答案
- 我建议**一开始不放开‘整稿任意编辑’自由富文本**,先做 12.5.1 的**句/段粒度受控编辑**——因为:
  1. 现有单位就是 sentence/paragraph,粒度自由太多会让"治理+版本+source"全部失效;
  2. 政企文案的真实结构其实多在"句/段",大字糊改反而少见;
  3. 真要整篇改的人场景(拿别人整稿当模板),更适合“新建稿 → 导入把原文放进来 → 逐段治理”,仍是句段级。
- 但为了“可拼接他人稿”,给**"导入文本作为 skeleton/draft"**入口(把导入文本也分段成句,统一进入同一治理管线),从而不靠"全稿富编辑"也能满足“拿素材稿起稿”。——这是我的建议:**先句/段级受控编辑+导入 skeleton**,推后"全稿富文本"。

### 12.5.4 一步到位的话是不是更值得? 
- 若评估允许,也可以把"直接编辑"做成**每 sentence 就地 textarea**,保存 = 12.5.1 的治理提交——体验接近"像 Word 一句一句改",但保存时后端仍跑治理。这其实就是富编辑的最大安全化版本。**是否全稿富文本是"体验 vs 治理安全"的取舍,rev-3 把这个取舍显性列出而不再回避。**

---

## 12.6 硬伤评审清单(技术 + 用户体验,源码佐证;后续逐项开 ticket)

### 12.6.A 技术/数据
| # | 硬伤 | 证据 | 修复/是否需要 C 前置 |
|---|------|------|----------------------|
| H-U1 | **检索层 owner/scope 缺失(越权旁路)** | `searchFilter` 只 tenant+latest;Qdrant payload 无 scope/owner;QA 全租户 | **C1**,见12.3 |
| H-U2 | **稿件版本无乐观锁** | `VersionNo=prev+1` 内存算;仅 uniqueIndex 兜底,并发丢写/重跑 LLM | **C2**,见12.4 |
| H-U3 | “§0.1 C6 重复首检”仍在(`retrieve` 又再 `checker`) | `orchestrator.go::Generate` 先 Retrieve 再 ClaimPlanner.Cover,用后者覆盖 | P1 顺手删冗余 |
| H-U4 | `EvidenceBinding` 无 source 原文 → tooltip/清单空洞 | `model/article.go`;`ListArticleBindings` 直 Find | §9.2 C,必先修 |
| H-U5 | integrate 测试大量依赖外部服务的 build tag(generation/qabot/dispatcher等),`go test ./...`未必覆盖核心多agent逻辑,回归面易漏多 agent 增量 | 测试是 `_integration`,`scripts/smoke` 需 api+worker 起 | 加纯 unit 判定层测试(rule/guardian state machine),减少对外部全依赖 |
| H-U6 | 稿件 revision/append 内 `PersistRetrievalBatch` 失败只 `_ = berr` 吞掉;导出锁定状态机部分依赖 UI | revise.go/revise/append | 落库失败须能显式返回,而不是静默 |

### 12.6.B 用户体验
| # | 硬伤 | 证据 | 修复 |
|---|------|------|------|
| H-X1 | 稿件是一整块 `full_content` pre-wrap 文本,无篇章/段落/标题可读排版 | `WorkspaceDetail` `Typography.Paragraph pre-wrap` | §9.1 结构化渲染 |
| H-X2 | 证据只有“证据 xN”tag,无原句 tooltip;丢失“哪句来自哪份原文” | 同 U4 + 前端 | §9.2 tooltip/source |
| H-X3 | 稿件阶段"改稿对话"没真正做:现有 `sendChat` 固定 `target_type='requirement_field'`,且没有“改这一句”的锚点动作 | `WorkspaceDetail.tsx sendChat target_type` | §9.3 sentence 锚点就地道改 |
| H-X4 | 知识库面板对"公有/私有"语义、回答与稿件检索范围无明确人话提示;公有库"普通用户可引用"是否含"AI 能引用"说不清 → 权限不透明 | 结合 H-U1 | 提示对话 + 权限文档 |
| H-X5 | "到处是功能不知干嘛":侧栏/按钮缺少"这是干嘛、点了会怎样"的人话;生成中无进度只一个 loading | `SpaceCombo`/`generating` boolean | §9.4 动线+ tooltip + run 状态人话 |
| H-X6 | 上传/覆盖/删除等破坏性操作目前对"影响证据/版本引用"的无提醒(删掉某 doc,旧稿证据仍指向它) | storage 软删+旧证据保留 | UI 提醒"该资料已被引用N稿"——二期数据上已可(见 db 预留),前端要感知 |

---

## 12.7 rev-3 落地优先级(给一份能排期的最小序列,把 C 级前置放最前)
1. **C1(S1 检索权限)** —— 不修则放开手编/QA 问答有越权, 先堵。
2. **C2(S2 乐观锁 CAS)** —— 不修则放任手动编辑的并发写会错乱。
3. **D0(sources 空洞)** U4/H-X2 前置 —— 把 `source_text/file_name/chapter` 冗余进 response, 让 tooltip 与清单不是空壳。
4. 然后才开 §9(排版/动线/tooltip/就地改)与 §12.5 混合写作受控编辑。
5. 同步去除 H-U3 冗余检索, 加 rule/guardian 的 pure-unit 层, 收敛 revision 双实现(C5)。

> 若无以上排期而直接放“允许人工编辑”,会先踩到 H-U1(越权)与 H-U2(并发丢写),因此 rev-3 明确把这两条列作“放开手编”的不可绕前置。
---

## 12.8 至此 rev 系列的完整立场(留给评审快速对照)
- **rev-1** 把"不是 workflow"做进可验证判据(A1/A2/A3)与 run/step 持久体。
- **rev-2** 把"Agent 是后台可信引擎,前台是能看的公文稿 + 人机好体验"立为产品姿态;并去掉"禁手编"教条的伪洁癖。
- **rev-3** 把 rev-2 可实施性 pin 到代码(C1 检索越权/C2 乐观锁 + sources 空壳),让人机混合写作与证据 tooltip 是"可改造得真能用"而非"纸面推演",并给出一份能排期、分 C 级前置的落地顺序。
>
> 我们的立场总结成一句给评审的话:**这套 Agent 化不是在"看得见的控制台"上,而是在后台守卫"可读、可引、可改的人稿体验";而让人能安全地直接编辑 AI 稿的前提,是先把代码里已被我核实的检索越权与版本并发两处硬伤用乐观锁与可见性平面堵住,再逐步开放句/段级编辑。**

---

# §13 rev-4 · 需求→技术→实现连坐的"断层"补洞(专防"方案对但实现接不上/体验让人不知道在干嘛")
> rev-4 不是新方向,是替评审把上面几版的"落地前提"再逐个拿真代码来核对后,挖出**如果只字面实现会造成'需求→技术→实现’三连脱节**的具体点,每点都给『代码现状 → 评审会怎问 → 修法/最小闭环』。避免你完成改造、一到"对方案/对代码"就破。
> 一个重要前提自检: **本项目链路的核心是"结构化稿件+句级证据",但大量地方仍在按字符串/整数序号在操作。rev-3 想开放的"人直编/就地端引用"如果没有先修"结构是假的/编号会漂",一放开就乱。** 因此 rev-4 把两条"结构性命根"挑出来最先排,再补体验空洞。

---

## 13.1 【W1】结构化层级根本没有真正落库 → §9.1 结构化渲染与"按段改稿"的前提是假的
- **代码现状**:`model.ArticleSentence` 有 `section_index`/`paragraph_index`,但 `generation.go::PersistArticleSnapshot` 与 `revise.go` 落库时**只写 `sentence_index`(一个全局 sentSeq 平铺号)**,section/paragraph 恒为默认 0;真正的章节/段落只存在临时拼的 `full_content`(markdown 字符串)。前端 `GetArticle` 拿到的只有 **平铺 sentences 数组 + 一段 full_content**。
- **评审会怎么问**:"你要做'结构化渲染/按段评价/句上引用',可稿子的 section/paragraph 分层根本没存——你是存了个把句子当扁平的数组,再用临时 markdown 忽悠渲染?证据 tooltip 想指到段落,数据上能吗?"
- **修法(必先于 rev-2 §9/§10 落地)**:
  1. `PersistArticleSnapshot`/`revise`/`append` 写 `article_sentences` 时,把该句的 `section_index`/`paragraph_index` **真正写入**(遍历 agent.Article.Sections→Paragraphs→Sentences 时携带);
  2. 补一次**历史文章版本的迁移**:从各自 `full_content` 无法安全反推(已生成稿未能保留原结构化) → 对已产出版本,**重跑或标注"(旧版降级为线性文本)"**,对**此后新版本**保证真分层;与用户沟通取舍:旧稿以线性形式保留仍是"可回溯快照",不影响新稿。
  3. 新增 `GetArticle` 返回**带 section/paragraph 的结构**,让前端与"就地改段"能真实按结构工作,而不是靠扁平序号猜段落。

---

## 13.2 【W2】对话动作结果把"内部 tool 名 + 底层 message"直接怼给用户 → 反习惯 + 无感知
- **代码现状**(`WorkspaceDetail.tsx::sendChat`):`r.results.map(x=> \`${x.tool}:${x.success?'成功':'失败'}(${x.message})\`)` 然后 `message.info()`/`message.error()`。用户看到的会是 `"update_requirement_field:成功(已更新需求单字段 style_tone)"` 这类内部实现串。
- **评审会怎么问**:一个给政企文案的系统,把 `tool` 名当产品文案弹给最终用户?——这既是"把内部接口漏到 UI",也没法表达"你的哪些改动我做了、哪些没做"。需求分析显然没把它当产品场景写。
- **修法**:
  1. 后端 `DispatchResult`/`ActionItemResult` 返回**人话文案字段字段**(如 `text="已把基调从'正式'改成'严谨'，此项改动已生效"`)与**面向开发/审计的原段(字段放开给溯源面板),两分离**;
  2. 前端不再按 tool.message 硬拼,而是**逐条展示人话结果条目(带 icon 对/错)**,错误也可说明"为什么没做/需要你补充什么";
  3. 对话产出计划若含"你这句话同时想去改稿与改需求字段",要以**顺序清晰的多条结果**给到用户,而不是"update_requirement_field:成功"一坨。

---

## 13.3 【W3】"句身份"=全局顺序序号,且无删除/换序/向前插 → 手编开放后"我要动的那一句"必然漂移
- **代码现状**:`article_sentences.sentence_index` 是全局 sentSeq;revise/append 的 target 也是整数序号(第 i 句);**没有 delete_sentence / move / 向前插**这些动作(我在 API/agent 层都找不到)。系统内只在"顺次全文/追加末尾/改第 i 句"上走得通。
- **评审会怎么问**:rev-3 说让人直改/就地引用,可如果用户"删掉第二段的一句、把第三段某句挪上来、在某句前插一句",后端怎么知道你要动哪句?它没有对应 model;目前只按 index 怼,一但改顺序/删句 index 就全错位、证据还拿着旧文继承。
- **修法(model 必须加"稳定句身份 + 支持序列 diff",而不只是又加个 action)**:
  1. 每个句子**用 `article_sentences.id` 做稳定锚**(而不是第 i 句);
  2. 放开手编时,前端把整稿当作**一串有序脏操作**上报 `change_list:[{op:edit|insert|delete|move, anchor:sentence_id 或 index(positional), new_text?}]`;后端按顺序 + 12.4 乐观锁(base_version_no) 应用并重写该 segment 的证据;
  3. 后端新增 op=delete/move/insert 的执行器,并用**diff 前后顺序的闭包验证**(closed set,same sentence ids preserved where untouched)保住 rev-1 "未授权句绝不改"的不变量。诚实说: 若不建模 delete/move/insert,**手编只能改字不能调结构**,这与"让人真舒服地改稿"矛盾,是一个因当前只支持线性新增而**藏着没兑现的能力断裂**。

---

## 13.4 【W4】workspace 状态机与 UI 割裂 + 生成能在"半需求/空需求"上轻点触发
- **代码现状**:
  - 新 ws 一律 `status=draft`(`workspace.go CreateWorkspace`),而 `needs_req`(待录入需求,金黄)在筛选里/UI enum 里存在,却几乎没有能被自然流程设置或走到的 分支→ **两个近义词但语义没跑通**;
  - `RequirementComplete` 有"标题&&平台&&至少一种风格才 complete"的初判,但**它并未在 UI 上作为"可生成"的角标强制**;未录需求就点"生成"要么 400「需求单不存在」(实际不会有,因 ws 恒带一张空 req)要么拿着一版空需求跑整条检索——用户在毫不知情的情况下让空需求过了一遍昂贵的 LLM。
- **评审会怎么问**:你的状态机 `draft/generating/generated/revising/failed` 在 UI/筛选都有,但 `needs_req` 到底谁设?一个新用户该在"第几步做什么"从界面上一眼可知吗?如果拿空需求能跑生成,那是把"别浪费 LLM/别对空需求编"的风险全赌在用户守规矩上。
- **修法**:
  1. 前端把"**能否生成**"做成可计算的硬前置:`RequirementComplete(req)`(标题/平台/至少一种风格/勾选范围非空)在 UI 直接作为「生成」禁用态 + 填空 tooltip("还差:平台/发文风格/引用范围");
  2. 状态机引入**明确的用户可理解步骤标签**,而不是让 `draft/needs_req/generating` 这些内部词外显;把"下一步你该做什么"变成界面默认信息(如「填需求→选引用→生成→看稿→导出」作为动线);
  3. `needs_req` 若要存在,去掉与 `draft` 的重叠歧义——要么当作新建的空壳稿直达态(此时生成按钮禁用),要么移除该枚举,避免评审在"状态机名词→实际流转"上抓到矛盾。

---

## 13.5 【W5】两套"用户入口"被混在一个表单里(从零对话生成 vs 拿来稿粘贴) → 都没有走通的闭环
- **现状**:一切从"工作区→需求单表单→生成"。但对政企使用者,有两类真实开始方式:
  a. **完全没稿**,希望 AI 顺着需求给骨架;
  b. **已有一份(随手写的/别处的)类似稿**,只想让系统"帮我校对引证 + 补全 + 别乱编/把没据处标出来"。
  rev-2/3 在 §12.5 提过"导入 skeleton",但没给成 entry 与闭环。
- **评审会怎么问**:你说支持"人机混合/用外部稿起稿",可系统入站只有『工作区→需求单→生成』一条路,粘贴稿进去走哪条?它会被当"需求章文当素材"检索掉,还是直接被判无证据打断?
- **修法(给 b 一条真路径)**:
  1. 需求单里加 `source_kind(build_from_scratch | draft_assist)` 的分岔,
  2. b 时把"用户粘贴/上传的草稿"先做 splitter(重用它)→ 标为非 KBase 凭证的*背景素材*,把它与 KBase 检索证据做**并轨而非二选一**:既有 KBase 支撑则 source_type=knowledge,无 KBase 支撑但它是用户自己给的背景则 source_type=user_draft 且默认"待人工确认"(像现有 no_source 一样给黄点),既服从"别编"又不因为它没进 KBase 而打死用户。
  3. 这条是"需求不会在真实使用中撞墙"的关键闭环——没有它,b 类用户会被误引导认为"系统不能拿着我的稿做事"。

---

## 13.6 【W6】文档被删 / 资料更新后,旧稿上的引用没有任何"它在系统里变老/变没"的用户侧感知
- **代码事实**:`doc_versions` 只增不减,旧版切片/句**永不物理删除**(为可溯源);文档"更新"产生新版、`latest` 平移。这对证据还原是正确的地基,但**界面层用户完全看不到**:某份资料 3 天前被我引在稿件里,今天它更新了新版本,稿子上并不会有任何提示,旧引用还静默指向旧版。
- **评审会怎么问**(如果评审是懂版本溯源的人):你为了'证据永不失效'把旧版都留着,可你看稿时根本不知道哪些句子引的依据已经"过期/被换",这是把可溯源做成了可审计但没人感知的日志。新功能"就地把不改"跟这个会配合出"用户引的是旧资料还不自知"的反例。
- **修法(体验上给最小可见路径)**:
  1. `GET article`(返回 sentence_views)时对每 source 顺带 `{current_latest_version_equals? bool, has_newer?: bool}`——这是 db 里本就能 join 出的信息,加给 tooltip"该原文已有新版本"(二期升级 / 本期至少黄点);
  2. 进入稿件「溯源详情」时可一键"打开该 source 的版本链",看绑定停在旧版、当前是什么。让"不可变的锚 + 你其实可以知道它有新版"成为功能而非日志。

---

## 13.7 小结这轮 rev-4 想根治的"class"级问题(给评审一句话)
> 前几版把「多 Agent 名实 + 可信度量 + 人机混合」立住了;rev-4 补的是**"这个功能它底下到底有没有能支撑它的结构/编号/状态机/入口"**:
> - **没有真分层就讲结构化渲染**(W1)、
> - **没有稳定句身份+增删改换就讲人直编**(W3)、
> - **状态机/入口只在枚举里不在真实流程**(W2 W4)、
> - **拿旧资料/外部稿来时系统不感知、不沟通**(W5 W6)。
> 这六条在代码/数据模型层都能定位到、都能用上面给的改法把"需求→方案→实现"接成闭环——完成它们,方案才不是"对评审看着漂亮、到代码对不上"。

---

## 附 rev-4 落地增量(与 12.7 一起排期)
- 12.7 的 1-5 不变,循环后追加:
  - **0.5 W1**: 修 section/paragraph 真落库(先于 rev-2 §9.1 才能做结构化)
  - **0.7 W3**: 补句稳定 ID 序列操作(insert/move/delete) + change_list(乐观锁上,先于"开放自由编辑")
  - **6 W2/W4**: 对话人话结果 + 状态机/可生成硬前置
  - **7 W5**: 提供 `draft_assist` 并入轨 source_type=user_draft
  - **8 W6**: source 顺带 `has_newer`,溯源详情版本链
  - (W2/W4 多为前端/一层 handler 的改动,成本不高的体验/可理解修复)

