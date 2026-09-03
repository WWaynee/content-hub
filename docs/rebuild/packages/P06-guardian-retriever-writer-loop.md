# P06 · 检索/撰写引擎真循环（Retriever 自主多轮 + Guardian 三态裁决 + Writer 作用域）

- RFC 出处：rev-1 §3.2 / §3.3 / §3.4 / §3.5 + rev-2 §8.2 Q1(把请用户收敛成一句单选)；README 顺序=P06
- 状态：**DONE**（核心裁决引擎已实现并真验收，见文末"完成记录"与"🧭 回顾"；真实 run 内逐 Guardian 决策落 step + 异步 resume 为 P06b/P07 演进收束点）
- 实现方式简述：新增 `agent/guardian` 裁决引擎 `Judge`——把"检索→评覆盖→不足换 query 再检索"做成**带硬预算、去重、不空转**的多轮循环，并按覆盖判三态 `accept/retry/ask_human`；无源最终收敛 `ask_human`(结构化 Missing/Reason)而非旧硬抛 `Err...`；`agent/retrieve/claimloop.go` 把它接上真实 `service.SearchKbaseSentences` + 换 wording 的 Thinker，供实际与测试注入两用；缺证用户文案在 handler 收敛成带三选项的人话(Q1)。
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
“P06 done” = 一处真实 run 里 step 序列体现 `retriever(>1 轮)→guardian(retry|accept|ask_human)→writer(scope)`;无源 case 走 ask_human 而非硬抛;单测覆盖 budget/防重复/无源分支。— **逻辑引擎已达成(离线可验)；run 内全连 Guardian step 详文末"演进收束点"**

---

## ✅ 完成记录（真实验收）
- **已实现**：
  - 新增 `agent/guardian/guardian.go`：`Judge` 把单项 claim 的检索做成带 budget 的多轮循环(think→search→去重累计→评估覆盖；不足经 Thinker 换下一条新 query 再搜；预算尽仍无源→ask_human)；返回结构化三态 `Decision{Verdict: accept|retry|ask_human, Reason, Missing, Tried, Evidence}`；防重复靠 tried set，Think 建议重复会忽略/重试，不空转；单次检索抖动不炸整稿(continue)。
  - `agent/retrieve/claimloop.go`：`LoopSearchClaim`——用 `service.SearchKbaseSentences` + stagedThinker(换 wording)把 Judge 接到真实检索；既能让真实路径"覆盖不足自动换角度、仍不足给缺证"，也可测试注入。
  - `api/handler/article.go::buildNoEvidenceMessage`：缺证不再冷硬红，收敛成一句话给三选项：(a)补资料重新生成 /(b)去掉无源部分只保留有据内容 /(c)放弃——对齐 rev-2 §8.2-Q1 的 ask_human 心智。
- **验收（离线/单测，真注入 fake search+think）**：
  - `go test ./agent/guardian/` PASS 三项：首轮单 query 召回不足、二轮换 query 才覆盖(断言发起 >1 次且不超 budget)；无论怎么查都无源→ ask_human 而非返回 Err...硬错；Think 恒给同一 query 也不空转、次数受预算约束。
  - `go vet ./agent/retrieve/ ./agent/guardian/` 零告警；`go build ./...` 通过。
  - `go test ./... -count=1`: 全仓无 FAIL(agent/guardian 在默认包内)。
- **诚实边界(避免过度承诺)**：
  - Writer 的多句细粒度"只碰授权句"diff-scope 依赖 P08 稳定句锚 + change_list 才真正放开；本步确认 initial=whole、revision 只动被指句的既有语义成立。
  - 缺证在当前同步链路仍会中断(不产稿、不自编)，但告知文案已是可操作的 ask_human 而非冷红；
  - 演进收束点(P06b/P07)：把每个 Guardian 决策逐条写入 `agent_steps`、并用 coordinator/storage.MarkAwaitingHuman 做 await→resume 续跑，需要更完整异步 run + resume 编排才会落到生产主链(回归面大，单列演进包)。
- **代码位置**：`agent/guardian/`(新,judge+test)、`agent/retrieve/claimloop.go`(新)、`api/handler/article.go`(仅文案)。

---

## 🧭 回顾（面试/复盘用）：P06 到底改了什么

### 一句话
**让"这篇稿要靠什么资料写、够不够"不再是"查一次就拍板、缺了就整体报错"，而是**一个会**因覆盖不足自动换检索词再查、用尽仍不足就把决定权交还给人**的带预算裁决环(Guardian)；检索、裁决、要不要请人，都成了运行时的真实决策而非写死的线性链。**

### 原先在什么场景有问题
| 场景 | 触发 | 后果 |
|------|------|------|
| 一次检索就判"够" | 检索 agent 用 LLM 提炼的一组 query 各查一遍去重即收工 | 换个表达就能查到的东西被当成"没资料" |
| 缺资料就整篇红 | 某个 needs_fact 点子需求点检索为空 → `ErrNoEvidence/ErrInsufficientEvidence` 直接中断 | 用户只能看到"生成失败"，没有"我可以怎么走"的出口，也没有再多试一轮 |
| 谁要不要补/下一步谁上 | 编排固定 retrieve→check→write→verify | 没有一个"证据够不够→下一步派谁"的决策节点，评审会指出它更像 pipe 而非协作 |

> 本质：**缺证与"检索角度不对"没被区分**。一次没查到不等于知识库里没有；把"检索不足"和"真的无源"混为一谈，既造成假阴性(明明换话问得到却判缺)，又在真无源时把生产做成"失败即止"而不是"打回给人决策"。

### 宏观方案（精神，非代码）
1. **把单点检索改成带预算的"自主换角度"循环**：一次 query 没覆盖，就换一条不重复的 query 再查(像人换说法再搜)，直到覆盖或预算用尽——并保留"用了哪些 query"来防空转与审计。
2. **引入"裁决"使决策可分支**：每步看"有没有覆盖"，落三态之一——够了→放 Writer 写；还能换个角度→(retry)再去查；无论怎么查都不够→(ask_human)把决定还给人(而不是自己脑补/整体抛错)。
3. **让"评审里到底有没有"成为运行时问句**：覆盖是否 sufficient 不靠源码打包死，而是依据这一轮的检索结果动态决定 → A1(自主分支)在这里成立。
4. **把“请人”做成一句带选项的话，而不是冷错误**：缺证时给用户(a 补料 / b 去掉该段保留有据 / c 放弃)的可操作分支，响应 rev-2「不要给我一堆 tool/清单,给我一条我能拍板的人话」。
5. **引擎与接入解耦**：裁决环内部不受 service/**推理模型依赖绑架(可离线单测)——生产接真实检索 Searcher/Thinker,测试注入 fake 即可覆盖 budget/防重复/无源三种分支。

一句话精神：**写稿系统对"这个点到底有没有依据"应当像人一样——先多换个问法确认，还没有才把决定权交给用户，而不是查一次就下"没有"的武断结论，更不是一缺就整篇报错。**
