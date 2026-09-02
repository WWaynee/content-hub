# P06 · 检索/撰写引擎真循环（Retriever 自主多轮 + Guardian 三态裁决 + Writer 作用域）

- RFC 出处：rev-1 §3.2 / §3.3 / §3.4 / §3.5 + rev-2 §8.2 Q1(把请用户收敛成一句单选)；README 顺序=P06
- 状态：待开工
- 前置：P05（依赖 run/step 持久与 Coordinator 接力）
- 目标：让稿件生产不再是"离线会一次函数级 from `orchestrator.Generate`"，而是 **Retriever 会因覆盖不足换 query、Guardian 依证据/校验结果动态决策 accept/retry/ask_human、Writer 只在授权句段内动笔**——从而在 `run` 留下的 step 里"下一步派谁/打回还是让人补料"都是真实运行决策(A1/A2/A3 都命中的应用点)。

---

## 1. 现状
`agent/orchestrator/orchestrator.go::Generate` 固定 `retrieve→check→write→verify→build`；
`agent/retrieve/retriever.go` 单轮 query 提炼后 for 检索去重即走(G6 冗余在 P07 一并去)；
Writer 只有一次整稿 `Write`(不含作用域)；
Guardian 概念尚未成为代码。

## 2. 范围与命中代码
| 文件 | 改动 |
|------|------|
| `agent/coordinator/`(P05新) + P05 的 `agent_run` | Guardian 把决策写进 step.successor |
| `agent/retrieve/retriever.go` | 从单轮 → `RetrieveClaim` 带 budget: think→search→assessCover→(不足) thinkNext→...→耗尽交 Guardian 打"覆盖不足该补却无源" |
| `agent/censor/claimplanner(已)` | PlanClaims/逐claim检索封装为 claim 覆盖(inform evaluate) |
| `agent/orchestrator` | 引入 `Guardian` 三态 decide(accept/retry/ask_human);把 decide 接到 run 推进 |
| `api/service/qa` 等复用 | 采用相同 budget 心智(可复用 helper) |

## 3. 可执行步骤
### 3.1 Retriever 真多轮(带 hard budget)
- 抽取公共接口 `Coverage(c c)*`,一次 claim→证据打分/看尚缺那几类(可用 `ClaimPlanner` 已有的子点)。
- 端上伪代码即 RFC §3.2;这里强调 3 个工程约束：
  1. `MaxRetrieveBudget`(config)+调用方 ctx 超时;
  2. 防重复 query:维护 `triedQueries`,thinkNext 时排除已空呼/低命中的 query;
  3. 兜底(写不回证据)不要自己编,而是给"覆盖不足"结构化供 Guardian 决策。
- 把这条也用于 QA(`agent/qabot`)对齐,让 QA 复用 budget(RFC 提到复用自环)。

### 3.2 Guardian 三态(存 step.successor)
- 定义 `GuardianDecision{Role: accept→writer | retry→retriever | ask_human→awaiting_human, reason, missing:[]}`;
- 在 `agent/rcoordinator or orchestrator` 决策处调它;每次把它 write 进 `agent_steps.successor`,并把整个 claim+evidence 覆盖摘要放进 step(审计/可用作 ask_human 予人话)。
- **重点收敛(rev-2 Q1)**：`ask_human` 不要"给你列缺证清单让你逐条审",而是打包成**一条人话待确认**：例如
  `“这段需要『2023演出场次/某阈值』，但知识库没有来源。你要 (a)不写这部分只保持定性 (b)我先去资料库补一段再重写 (c)放弃”。`
  前端(在 P12)呈现成单选。Guardian 落库 PendingDecision 供路由(payload 到 `ask_human`)。

### 3.3 Writer 作用域化
- 给 Writer 新增入参 `Scope`(whole | 指定段 | 指定 index 区间) 与“不得碰未授权句”硬校验(diff 集合⊆授权)。
- 整稿生成(initial)→ scope=whole 保持；revision/append 走既有 `run:revision` 复用(见 P08/P09)；此处主要是从"两套实现(reviser.go 在 orchestrator 但未接)"收敛到“Coordinator 走唯一 revision 链”(与 P07 一并)。

## 4. 验收
- 单测(无外网/可注入 fake llm/vect)：给一条"首轮单 query 召回不足、需二次换 query 才能覆盖"的 claim，断言 Retriever 会发起 >1 次检索且不超 budget；给"无论怎么查都无源"的，断言 Guardian 返回不 `accept` 而是 `retry`(有换角度可能)或 `ask_human`(一无所有)而非直接抛 `Err...`(旧行为)——
- 现有 `ErrNoEvidence/ErrInsufficientEvidence/ErrFactUnsupported` 语义改为 run 内部 `declined/await` 而非立刻报错外层。
- 状态合法转移测试接 P05。

## 5. 依赖/开放
- 依赖 P05(run 持久),依赖 P02(并发)。
- 是否把 `Guardian` 标成一个独立 LLM 调用还是用确定性规则先做初判?建议：**先做成 LLM 决策(承载 A1)**,并在 RuleVerifier(P07) 后再接确定性校验结果——两处协同,Guardian 只管"证据够不够/可不可写",不含真伪判断(真伪留给 RuleVerifier)。

## 6. done gate
“P06 done” = 一处真实 run 里 step 序列体现 `retriever(>1 轮)→guardian(retry|accept|ask_human)→writer(scope)`;无源 case 走 ask_human 而非硬抛;单测覆盖 budget/防重复/无源分支。
