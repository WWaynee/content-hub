# content-hub 多 Agent 技术方案

> 本文档专门阐述 content-hub 的多 Agent 架构、职责划分、对话模型、数据流与失效一致性。
> 与 `docs/architecture/architecture.md` 互补：architecture.md 讲整体分层与存储，本文档聚焦多 Agent 的协作机制。
> 文档状态：v1 定稿（经多轮讨论收敛）。

## 1. 总体定位

采用**「统一入口对话 agent + 专职子 agent + 结构化产物传递」**的混合形态：

- **对话 agent 是唯一入口**：所有阶段的用户对话都由它理解意图、维护对话历史、派发任务。
- **子 agent 专职化**：检索 / 撰写 / 证据整理各自专职，无状态，被派发执行，产出结构化产物。
- **数据流用结构化产物**：agent 之间只传「严格格式化、可解析、经机检」的产物，不传对话历史、不传自然语言。
- **不是一次性流水线**：用户随时可回到需求单界面重新分析，或回到稿件界面改段落/句子。

## 2. Agent 职责划分（专职化）

| agent | 职责 | 持有对话历史 | 输入 → 产出物 |
|-------|------|:----------:|---------------|
| **对话 agent**（统一入口） | 理解意图、维护阶段对话历史、派发任务 | ✅ 是（按阶段分 session） | 用户消息 → `DialoguePlan`（动作计划） |
| **知识检索 agent** | 在勾选范围内检索证据 | ❌ 无状态 | 需求+范围 → `RetrievalBatch`（doc_sentence 指针列表） |
| **稿件撰写 agent** | 生成/改写稿件 | ❌ 无状态 | 需求+证据 → `Article`（含句级证据绑定） |
| **证据整理 agent** | 格式化证据清单 | ❌ 无状态 | Article+证据 → `EvidenceManifest` |

**关键约束**：
- 对话 agent 是**唯一**持有对话上下文的 agent；其余 agent 无状态、可重放、可测试。
- 撰写 agent 只负责"写稿"、检索 agent 只负责"检索"，不兼任"理解对话意图 / 回填需求单"。

## 3. 对话模型（阶段私有 + 统一入口）

- **对话历史/上下文按「阶段」分离持久化**，每个阶段一个 session：
  - 需求单阶段对话 → 一个 session
  - 稿件阶段对话 → 一个 session
  - （检索界面对话一期不做）
- **统一入口**：无论哪个阶段，都由「对话 agent」接收用户消息。
- **消息带 target 锚点**（乙+锚点模型）：`target_type`（sentence / paragraph / requirement_field）+ `target_ref`。

### 3.1 各阶段对话的权限边界

| 对话阶段 | 允许的动作 | 不允许 |
|----------|-----------|--------|
| 需求单界面 | 修改需求单字段；闲聊（"你好"等） | 改稿件、改检索 |
| 稿件界面 | 修改需求单字段 + 改稿件 + 请求补检索 | — |

- 稿件界面对话涉及改需求单 → **回填需求单表**（结构化，不只留对话记忆）。
- 字数要求、勾选文档范围等"只能手动改"的字段，对话中出现时提示去需求单手动改（不自动回填）。

## 4. 对话 agent 的产物：DialoguePlan（动作计划）

对话 agent 不直接执行业务操作，而是产出**结构化动作计划**：

```
DialoguePlan {
  actions: [
    { tool: "update_requirement_field", field, value },       // 回填需求单字段
    { tool: "request_retrieval",        query, target },      // 请求补检索
    { tool: "append_article_content",   position, instruction }, // 增补稿件
    { tool: "revise_article_sentence",  target, instruction },   // 改写句/段
  ]
}
```

- 每个 action 独立**机检（JSON Schema 校验）**，不合法的拒绝。
- 工具调用受**字段白名单 + 枚举**约束，防止 LLM 乱填。
- 动作计划落入 `conversation_messages`（含 target 锚点），可审计、可追溯。

## 5. 执行与数据流（派发 → 产物 → 拉取）

```
用户说话
   │
   ▼
对话 agent（统一入口，持有本阶段 session）
   │  理解意图（按阶段权限判断）
   ▼
DialoguePlan（动作计划，机检后落 conversation_messages）
   │
   ▼
调度层（轻量派发，非流水线编排）
   │
   ├─ update_requirement_field → 直接写需求单表（需求版本号 +1）
   ├─ request_retrieval        → 派发「知识检索 agent」→ 产出 RetrievalBatch（指针列表）
   ├─ append/revise_article    → 派发「稿件撰写 agent」→ 产出新 Article（句级证据绑定）
   │
   ▼
证据整理 agent：读取 Article + RetrievalBatch → EvidenceManifest（用于展示/导出）
```

**核心原则**：
- agent 间只传**严格格式化的产物**，不传对话历史、不传自然语言。
- **产物落 DB、用引用 ID 拉取**，不区分是哪个 agent 产的。
- 检索产物只存**标识符/指针**（doc_sentence_id），不冗余存文本本身。

## 6. 失效与一致性（惰性版）

- 需求单有**递增 version** 字段。
- 每次检索产出一个 `retrieval_batch`，记录其**基于的需求单 version**。
- 用户改需求单（无论从哪个界面）→ version +1 → 旧检索 batch 标记"过期"。
- **惰性**：不立刻重新生成；用户回到稿件界面触发修订/重新生成时，检测 version 不一致 → 重新检索 → 基于新证据重写。
- **上下游失效判定**：不靠 LLM 猜"该替换哪条证据"，靠「旧检索 batch 指针集合 vs 新检索 batch 指针集合」的 **ID 集合 diff**。

### 6.1 需求单变更 vs 稿件变更

| 变更类型 | 触发方式 | 处理 |
|----------|---------|------|
| 改需求单（全局字段：风格/章节/字数等） | 需求单 version+1 | 全量重新生成（generation） |
| 改需求单（勾选范围） | 需求单 version+1 | 全量重新生成（证据来源可能全变） |
| 改稿件（句/段级） | 对话派发 revision | 局部重写 + 该句重检测证据（未动句证据继承） |

## 7. 产出物落库

| 产物 | 落库表 | 说明 |
|------|--------|------|
| 检索结果 | `retrieval_batches` + 关联 `doc_sentence_ids` | 指针列表 + requirement_version |
| 稿件 | `articles` / `article_versions` / `article_sentences` | 快照式 |
| 证据绑定 | `evidence_bindings`（稿件句 → doc_sentence_id） | 句级 |
| 对话历史 | `conversations` / `conversation_messages`（含 action plan JSON） | 阶段私有 |
| 需求单 | `requirements`（含 version，待新增） | 版本号驱动失效 |

## 8. 明确不做（一期）

- 检索界面的对话。
- 逐步确认式对话（一期同步一次执行完整个 DialoguePlan + 汇报）。
- 证据"已有新版本"的提示（schema 预留引用链，功能二期）。

## 9. 设计取舍说明

- **统一对话入口 vs 各阶段独立 agent**：选择统一入口，避免"对话理解 + 意图分诊 + 工具调度"逻辑在多 agent 间重复；阶段差异通过「不同的动作权限白名单」体现，而非不同的入口 agent。
- **无状态子 agent vs 有状态子 agent**：子 agent 无状态，产物落 DB；上下文只归对话 agent，保证可测试、可重放、职责清晰。
- **惰性失效 vs 立即重算**：惰性（用户触发时才重算）省资源、符合"随时回来"的交互，且用需求单 version 做判定基准，避免 LLM 语义判断的不确定性。
