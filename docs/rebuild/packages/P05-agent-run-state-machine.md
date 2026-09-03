# P05 · agent run 状态机与持久体（run/step 一等实体 + 异步队列消费）

- RFC 出处：rev-1 §2.1 / §3.5, rev-3 >D1(直接一步建 run 表)；README 顺序=P05（第一个"把 agent 从 workflow 变持久协作"的包）
- 状态：**DONE**（已实现并真验收，见文末"完成记录"与"🧭 回顾"）
- 前置：P01(P02 相关少)。实现 A2(持久副作用)才成立。
- 实现方式简述：新增一等表 `agent_runs`/`agent_steps` + `agent/coordinator` 状态机(可回放/可审)；把 initial(generation) 与 revision/append 三条写路径都先建 run→逐步落库(role/action/decision/successor/outcome)→成功 FinishRunOk(产版本)/失败 FailRun；同 ws 排他用 run.active 尽力拦截 + 稿件版本 CAS(P02) 兜底并发。
- 目标：
  1. 建立**持久、可回放**的一篇稿件生产会话 `agent_run` + `agent_step`；
  2. Orchestrator 改写为**驱动 run 生命周期、每次决策落库**的状态机(Coordinator)；
  3. 把 `generate/revision/append` 改成提交 run → (可选异步 MQ 消费) → step 推进 → 末次写新 article_version。

---

## 1. 问题与动机
现 `GenerateArticle` 同步把检索→撰写→校验→证据在 handler 里一次跑完,`conversation_messages` 存的是 DialoguePlan,没有中间 run 状态 → 无法回答"agent 到底跑了什么/每一步派给谁/证据依据哪版需求"；评审也据此点"你是 workflow"。rev-1 用 **run/step 持久**回应 A2。

## 2. 表与数据契约草案(直接在 db.md 追加,作为一等实体)
`agent_runs`：
- `id / tenant_id / user_id / workspace_id`
- `run_type: initial | revision | append | regenerate`
- `base_article_version`（int, 0=首次）
- `plan`(json：Planner 拆出的 Claim[] 集)（可 NULL 待 Planner 后填）
- `status: running | awaiting_human | success | failed | cancelled`
- `active`(并发闸：同 workspace 同一时刻仅 1 个 non-terminal run)
- `current_role/current_action`(供 UI 人话状态)
- `created_at / updated_at`

`agent_steps`：
- `id / run_id / step_no / role(planner|retriever|guardian|writer|verifier|match_human)`
- `action`(如 search_claim/triage/write/verify/decision)
- `decision_text`
- `successor`(Guardian 决定下一步角色)
- `outcome: rejected | accepted | raised_flag | await_human`
- `created_at`
(步骤绑定检索快照在存储层按 run 带；可延用或扩展既有 `retrieval_batch` 对到 run_id,见下)

约束：同 workspace 并发——用 `active` + 版本 CAS(P02) 守；`agent_steps` 提供"逐跳去哪、谁审批"的可回放与可审。

## 3. 可执行步骤
1. **建表/模型**：在 `storage/model/support.go`(或新建) 加 `models.AgentRun`、`models.AgentStep`；`cmd/migrate` 创建并索引 `(workspace_id, active)`、`(run_id, step_no)`。
2. **Coordinator 包**（新 `agent/coordinator/`）：
   - `CreateRun`(workspace,user) → 校验无活跃 run；
   - `Advance(run, plannedResult)` 事务中写一步 step + 迁移 run.status → 返回可给 UI 的人话人话状态；
   - `Fail(run, reason)` / `MarkAwaitingHuman(run, pending)` / `Cancel(run)`。
3. **把现有流程接到 run**：`handler.GenerateArticle` 由"直接 `orchestrator.Generate`"改为 `NewCoordinator().Start(initial_run)`；`run` 同步或异步推进都写成"step 状态机",而不再是一次长事务串行(便于点到一半补料)。
   - 可先保留同步对外(除异步节流),但内部必须有 step 落库 + active run 锁。
4. **异步(rev-1 §3.5)**：复用 `mq`，真正让 worker(`cmd/worker`)消费 `article_generate` 队列执行 run；`POST /workspaces/:id/runs` 返回 `run_id`;`GET /runs/:rid`/`/steps` 供进度;`POST /runs/:rid/decision` 供 `ask_human` 确认(见 P06)。若一期先同步可接受,但状态机结构先为异步留口;README 排期里异步是必须(否则 rev-1 3.5 没兑现)。
5. **检索快照挂 run**：`retrieval_batch`(若存)关联 run_id；从此"依据哪版需求"能审计。

## 4. 验收标准
- 单元:run 状态机的合法迁移表(running→success/failed/awaiting_human/cancelled 全部可达;拒绝非法转移)。
- 集成:两次**并发** generate/revise 同一 workspace → 第二 run 因 active 或版本 CAS 被拒且不落写坏 version;成功 run 后 `article_versions` 恰好 +1。
- (异步若接)worker 消费端:一次真实 run 从 queued→ 各 step→ 末 article_version;crash 后能重放(重启通过 DB 恢复 running run 到可推进)。

## 5. 开放问题
- 同步 vs 异步先行:默认**先同步落地但状态机化 + run 表**;异步 MQ 放入本包后段(rev1 3.5)。若外部服务齐全可以先异步。
- steps 逐跳是否存"全文/短结果"?默认存 decision+successor，不存大原文，正文取正式产物(article_version/sources)。

## 6. done gate
“P05 done” = run/step 可建/可推进/可审计;并发同一 ws 被锁;`Generate`(至少在同步路径)产生稳定 run+step;既有 generation 单测/集成基线仍绿。— **已达成(见文末"完成记录")**

---

## ✅ 完成记录（真实验收）
- **已实现**：
  - 新增一等持久实体：`storage/model/agentrun.go` 定义 `agent_runs`(run_type/status/base_article_version/result_version_id/active/plan/current_role…)+`agent_steps`(`(run_id,step_no)` 唯一；role/action/decision/successor/outcome/ref_id，记录"这步是谁、做了什么、决定下一步派给谁")。`cmd/migrate` 注册两表；`storage/run.go` 提供 BeginRun(同 ws active 排他)/AppendStep(step_no 自 1 连续)/FinishRunOk/FailRun/MarkAwaitingHuman/CancelRun/List(ListA)Steps。
  - `agent/coordinator/`：Coordinator 状态机封装 Start(建 run)/NoteStep/Success/Fail/MarkAwaitingHuman/Cancel；`ValidTransition`+`CanTransition` 状态表驱动。
  - 三条稿件写路径接入 run：
    - `api/handler/article.go::GenerateArticle`(+`api/handler/run.go::beginInitialRun`)：编排前先建 initial run、写 planner step → 成功落版本后追加 evidence 终 step 并 FinishRunOk → 任一失败 FailRun；响应带 `run_id`。
    - `api/service/runlink.go` + `dispatcher.go`：对话 revise/append 走 `RunRevision`/`RunAppend`，各成 revision/append run。
  - 说明：generation 的 run 化放在 handler(构建 orchestrator 需 import agent/retrieve 等，放 service 会产生 import 回环，见 run.go 注释)。异步 MQ worker 真消费留到 P06(P05 §5 默认同步先行)。
- **验收（真 MySQL / 纯单测）**：
  - `go test ./agent/coordinator/` state transition PASS(running→success/failed/awaiting_human/cancelled 全可达、终态无出边、非法被拒)。
  - 新增 `api/service/run_integration_test.go::TestRunLifecycleAndActiveExclusion` PASS：Start 建 run(running+active)；同 ws 再加 run 被 ErrRunActive 拒；AppendStep/ListSteps step_no 从 1 连续；Success 释放后可再开；Fail 置 failed+非 active+带 error 且释放后能续。
  - P02 `go test -tags=integration ... TestConcurrentGenerationVersionCAS|TestRevisionApplyCAS` PASS(generation N=6 成1冲突5、revision m=4成1冲突3、版本唯一连续)——为"第二 run 因 CAS 被拒、version 恰好 +1"背书。
  - `go test ./... -count=1` 全仓纯单测全绿(含 agent/coordinator、api/service、cmd/migrate)；`go run ./cmd/migrate` 后 db 达 24 张表(agent_runs/agent_steps 在列)。
- **并发语义收敛（诚实说明）**：run.active 排他靠事务检查 + 尽力拦截；两 run 并发穿透时，由稿件版本 CAS(P02)在写版本前"抢到 base→base+1 才算数"，较晚者被拒且不写坏；其 run 被 FailRun 标记(释放 active)。这正合 P05 验收"第二 run 因 active *或* 版本 CAS 被拒"的 or 语义。

---

## 🧭 回顾（面试/复盘用）：P05 到底改了什么

### 一句话
**把"一次生成/改一段稿"从"一个瞬间跑完的函数调用"变成 DB 里一条可回放、可审计、同稿件互斥的生产会话(run)——下面还挂着一格一格的 step，从此能回答'agent 到底跑了什么、每一步派给谁、为什么下这步、卡在哪、依据哪版需求'。**

### 原先在什么场景违约
| 场景 | 触发 | 后果 |
|------|------|------|
| 评审问"你这是不是把函数按顺序堆一遍(workflow)?" | handler 同步直跑一次排好的 检索→撰写→校验→证据，中途没有任何可查中间态 | 只能被当成流水线,没有"逐步进行/自主转向"的证据 → 不像多 agent 协作 |
| 想回看"上次这篇稿 AI 调了哪些资料、为什么这么写" | 只存对话(conversation_messages)与完成态快照(article_version) | 中间决策/谁派下一跳/每步结局查不到 → 无法复盘 |
| 同一工作区被并发点「生成」/「改句」 | 两条请求同时写新版本号 | 会撞版本号/静默丢写(要靠 CAS 兜底) |
| 生成卡住/失败 | 同步 HTTP 一口气到超时 | 用户不知道跑到哪、为何停,只能干等/重试 |

> 本质:以前是"调用链",每一步瞬态、没有落库的状态;要让它像"人协作",得把一次生产做成**能暂停、能继续、能回放、能审的一等实体**。

### 宏观方案（精神，非代码）
1. **给一次生产一个持久的"会话"(run)**:类型(initial/revision/append/regenerate)、起点版本 base、进行到哪(state/current_role/action)、最终产物版本、成败与原因——全落库;它是"回放/审计/异步/await_human"的共同锚。
2. **把执行切成 step 落库**:每格记"谁(role)·做了什么(action)·结论(outcome)·下一步派谁(successor)";"跑了什么/为什么下这步/谁派给谁"是 DB 可回放的,而非进程内存瞬态。
3. **用状态机而非散落 if 约束**:run 只允许合法迁移,杜绝"飘到某状态没人管"。
4. **把并发一致性提前收口**:同工作区一次只一个"进行中/等人工"生产(active 排他),同稿件一次只一个写者能 CAS 加版本号——把"丢写/重复版本"从落库最后撞索引,提前到发起就知道该被拒/排队。
5. **为异步与"卡住等人"铺路**:run/step 持久且能推进到 awaiting_human,后续(P06)Guardian 的 ask_human / Retriever 换 query 才能"停一下问用户再接着跑",而不是一失败就整篇红(P05 同步先行,MQ 异步由 P06 引入)。

一句话精神：**把写作从"一条被打包执行的流水线"变成"一个可暂停、可回放、可审计、并发的持久会话"——这是'想多 Agent'最该先立的地基，也是 A2(持久副作用/状态可审计)能成立的落点。**
