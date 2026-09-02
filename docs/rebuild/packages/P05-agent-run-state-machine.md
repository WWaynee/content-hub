# P05 · agent run 状态机与持久体（run/step 一等实体 + 异步队列消费）

- RFC 出处：rev-1 §2.1 / §3.5, rev-3 >D1(直接一步建 run 表)；README 顺序=P05（第一个"把 agent 从 workflow 变持久协作"的包）
- 状态：待开工
- 前置：P01(P02 相关少)。实现 A2(持久副作用)才成立。
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
“P05 done” = run/step 可建/可推进/可审计;并发同一 ws 被锁;`Generate`(至少在同步路径)产生稳定 run+step;既有 generation 单测/集成基线仍绿。
