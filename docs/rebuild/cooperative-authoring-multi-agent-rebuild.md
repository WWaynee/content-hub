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
