# content-hub 架构与技术方案

> 本文档详细描述 content-hub 的整体架构、多 Agent 编排、数据契约、存储模型与技术方案。
> 功能基线见 `docs/features/features.md`（v3 定稿版）。数据表设计见 `docs/architecture/db.md`（后续）。
> 文档状态：v1 技术方案定稿（随讨论持续演进）。

## 1. 技术栈总览

| 分类 | 技术 | 说明 |
|------|------|------|
| 后端 | Go 1.x + Gin | 服务框架 |
| 数据库 | MySQL 8 + GORM | 关系型数据 |
| 缓存 | Redis | 键值内存：限流/用量/内存级会话 |
| 对象存储 | 阿里云 OSS | 文件实物存储（物理扁平） |
| 向量库 | Qdrant | 知识切片向量化与检索 |
| 消息队列 | RabbitMQ | 异步任务（文档解析、稿件生成） |
| 大模型 | DeepSeek（对话）/ 硅基流动（Embedding） | OpenAI 兼容接口 |
| 可观测 | 结构化 JSON 日志 + 全链路 TraceID + 审计日志 | 复用前身 observability |
| 鉴权 | bcrypt + JWT（util/jwt.go + password.go） | 载荷 user_id/tenant_id/role |
| 部署 | Docker Compose（MySQL/Redis/Qdrant/RabbitMQ） | OSS 为云服务直连 |

> 除阿里云 OSS 外的中间件（MySQL/Redis/Qdrant/RabbitMQ）均以 Docker 启动。

## 2. 分层架构

```text
cmd/(api|worker|migrate|configtest)
     │
api/(handler → service)              API 服务：HTTP 入口 + 业务编排
     │      │
     │      └── agent/orchestrator   多 Agent 工作流编排（核心新增）
     │
storage/(MySQL|Redis|OSS|Qdrant)      数据持久化层
     │
llmclient/                            大模型客户端（DeepSeek/SiliconFlow）
splitter/                             文档切片（结构化优先 → 自然段 → 软性300字 → 完整句）
mq/                                   RabbitMQ 封装（trace 贯穿）
observability/                        日志/指标/审计（TraceID 贯穿）
util/(JWT|password)                   bcrypt + JWT
kbase/                                知识库：目录树、文件、版本、切片、向量化
web/                                  前端静态
```

### 分层依赖原则（沿用前身）
- 业务层只依赖 `llmclient.Client` 接口与 `storage`，不直接触碰厂商 SDK / DB 细节，便于换厂商、换存储。
- 全链路 `tenant_id` 来自 ctx/JWT，**不信前端传入**。
- 数据层强隔离：任何跨租户查询一律强制 `tenant_id` 过滤（含向量 payload）。

## 3. 多 Agent 混合编排（核心设计）

### 3.1 形态定位

采用**自研多 Agent 混合编排**（非 LangChain/LangGraph）：
- **全局编排为骨架 + 局部自主补充**。
- 一个 **Orchestrator（编排器）** 用确定的工作流调度多个 agent，控制先后、分支、成败。
- **知识检索 agent** 以 ReAct 自主多步检索。
- 其余 agent 以结构化/对话式单步执行。
- 全程贯穿 trace_id + 审计，agent 间通过**结构化数据**传递。

> 不引入 LangChain Go / LangGraph（无 Go 成熟版），复用前身自研 ReAct 引擎 + 新增 orchestrator。

### 3.2 Agent 分工（4 个 + 编排器）

| Agent | 形态 | 职责 |
|-------|------|------|
| Orchestrator | 工作流引擎（非 LLM agent） | 调度 generation / revision 两种工作流，判断分支、派活 |
| 知识检索 agent | ReAct 自主 | 数据搜集：在勾选范围内检索，返回证据素材 `[]Evidence` |
| 稿件撰写 agent | 结构化单步 | 生成/重写稿件，建立「句子 ↔ 证据」绑定（`ChatWithJSON`） |
| 证据整理 agent | 格式化单步 | 导出时把绑定关系格式化输出证据清单（不重检） |
| 需求对话 agent | 对话 + 意图解析 | 两种对话：需求单对话（改字段）、修订对话（改稿件，输出结构化指令） |

### 3.3 两种工作流（generation / revision）

**Generation（初稿）**：`需求单 → 知识检索 agent → 稿件撰写 agent → 证据绑定落库 →（可导出）`
- 一次性全稿替换，一稿一个稳定完成态。

**Revision（修订）**，触发源：用户对话修改稿件某句/段：
```
对话 agent 理解意图 → 输出结构化修改指令(含 needs_retrieval + query + targets)
      → Orchestrator 判断 → 需要补检索 ? 知识检索 agent(勾选范围) : 跳过
      → 稿件撰写 agent(仅改目标句/段，用户未指定句绝对不改)
      → 对话结束后统一重建被改句的证据绑定(证据整理 agent)
      → 形成"本次修订完成态"（可导出）
```

### 3.4 修订编排归属（关键约束）
- **需求单对话**：单 agent 直通（需求对话 agent 独立完成，改需求单字段）。
- **修订对话**：由 **Orchestrator 派活**（可能牵动检索 → 撰写 → 证据）。需求对话 agent 只输出结构化指令，**补检索的决策交给 Orchestrator**（可控、可审计）。
- 一次对话可含多个修改目标，但**整次对话视为一次修改**；修改进行中（未生成成功）阻断导出。

### 3.5 两种对话（界面语义）

| 对话 | 界面 | 目的 | 编排 |
|------|------|------|------|
| 对话 A | 需求单界面 | 改需求单字段（风格/章节等） | 单 agent 直通 |
| 对话 B | 稿件界面 | 检索 + 修改稿件句/段 | Orchestrator 派活 |

## 4. Agent 数据契约

### 4.1 知识检索 agent
- **输入**：`tenant_id`（ctx）、勾选范围（目录/文档，递归解析为文件+版本清单）、需求单任务要求、（可选）检索意图
- **输出**：`[]Evidence`
```
Evidence { evidenceId (指针引用), documentId, versionMd5, chapterTitle(可空),
           chunkId, sentenceId, sourceText(原始原话，不加工), weight }
```
- top-K 上限 = **20**。

### 4.2 稿件撰写 agent
- **输入**：需求单（完整）、`[]Evidence`
- **输出**：`Article`（结构化，含句级证据绑定）
```
Article { title,
          sections[{ heading, paragraphs:[{ sentenceChunks:[{
              sentenceId, text, evidenceRefs:[]EvidenceId  }] }] }] }
```
- 写作时**仅在引用资料的句子绑定证据**；无源句子 `evidenceRefs=[]`。
- 撰写/重写时**用户未指定修改的句子绝对不改**（硬约束，写进 prompt + 校验）。
- 严格 JSON 输出（复用 `ChatWithJSON` + 容错）。

### 4.3 证据整理 agent
- **输入**：`Article`（句→evidenceId）、`[]Evidence`
- **输出**：`EvidenceManifest`（导出格式化的 markdown 证据清单，含原文原话、文档/版本/章节定位）
- 只格式化，不重检。

### 4.4 需求对话 agent
- **输入**：用户自然语言 + 上下文（需求单 or 稿件 + 目标句/段）
- **输出**：结构化操作
```
DialogueAction {
  type: update_requirement | revise_article,
  field?,        // 需求单字段
  targets?[],    // sentenceId | paragraphId（可多个）
  needs_retrieval?, retrieval_query?,   // 是否补检索 + 需查主题（LLM 判断）
  instruction     // 新内容/修改要求
}
```
- 修订时 target 来自会话消息的锚点（乙+锚点模型）。

## 5. 存储模型（MySQL）

### 5.1 分层：切片 / 句子 / 绑定

| 表 | 内容 | 冗余策略 |
|----|------|---------|
| `doc_chunks` | 文档切片原文（~300 字，结构化优先），去重 | 每文档每版本每切片一行，索引含版本 |
| `doc_sentences` | 文档句子（提取自切片，~40 字），去重 | 每文档每版本每句一行；被多稿件引用时大家指向同一行 |
| `article_sentences` | 用户稿件自己的句子（含证据绑定或为空） | 每篇稿件自己的句子各一份（稿件本体） |
| `evidence_bindings` | 稿件句 ↔ 文档句 的证据绑定 | 绑定引用关系，轻量 |

- **证据锚点 = 句子**（最小绑定/重检测/修改单元）；切片是物理组织容器。
- **原文去重**：切片/文档句为文档属性，全局唯一，不因"多人引用"而复制；冗余的只是"稿件自己的句子"（本就是稿件一部分）。
- 切片/句子 **key 包含所属文档版本**：`(document_id, version, index)`，保证旧版本证据锚定稳定、可检索范围的版本隔离。

### 5.2 版本语义（只增不减，MVP）
- 每个文档有身份 `document_id` + `current_version` 指针 + 版本列表（倒序）。
- **只做增量**：上传新版本覆盖 → 旧版 `latest=false`；**MVP 不做**版本删除/回退。
- 旧版本切片/句**物理保留**（MySQL 原文），仅从"可检索视图"移除（向量库标 `latest` 或用 document_ids 过滤）。
- 删除文档：MySQL 软删除 + 向量失效（见 6.2）。

## 6. 检索与向量化（Qdrant）

### 6.1 切片入库
- 上传 →（worker 异步）→ `splitter` 切片 → Embedding（硅基流动）→ 写入 Qdrant（payload 带 `tenant_id` / `document_id` / `version` / `latest` / `chunk_index`）→ 切片/句原文落 MySQL。全链路成功才成为可检索最新版。

### 6.2 切片策略
- **结构化优先**：有 markdown 标题/章节按结构切（标题入章节元信息）。
- 次之**自然段**优先。
- 软性字数上限默认 **300**（可配置）。
- **断点必须在完整句末**：300 字断点落在句中时，延至该句句号后截断，保证切片语义完整。
- 切分先按句号断句（。！？…），再以句为最小合并单元凑 300。

### 6.3 检索
- **collection 按租户隔离**（每个租户一个 Qdrant collection）。
- 检索时 Qdrant **payload filter** 原生支持，检索请求带：
  - `tenant_id`（或 collection 天然隔离）
  - `latest=true`（版本过滤）
  - 勾选范围展开的 `document_ids`（限定到具体文档，缩小候选、提升精度）
- **元数据过滤不影响检索质量**：`tenant_id`/`latest` 属"可见性过滤"，纯增益；`document_ids` 是对"勾选范围"的业务排除，正是产品"锁死范围"的意图，非缺陷。
- 召回 = Query + filter + top-K(20)；二次重排（rerank）为后续优化项，MVP 计划之一不做。
- 旧版切片**不进向量库的检索范围**（只 `latest=true`），但原文保留在 MySQL 供证据读取。

## 7. 版本与证据的追溯

- 证据锚定带版本（`document_id + md5 + chunk/sentence`），**不随文档更新失效或漂移**。
- 检索/预览/下载**永远访问最新版本**；但**已检索证据 / 已绑定证据始终可读取旧版本原文**（旧版切片/句物理保留）。
- **后续项**：数据层预留"绑定版本 vs 当前版本"比对字段，二期在稿件/证据清单中提示"该资料已有新版本"（本期只预留字段，不做提示）。
- 删除文档不删被引用旧数据（软删 + 向量失效），证据仍可追溯。

## 8. 知识库权限模型

- 复用"**数据层死守租户隔离**"原则 + 公/私有隔离。
- 公有库：仅管理员可管理；普通用户可预览/引用，不可下载/更新/删除。
- 私有库：每人一个，仅本人完整读写。
- 勾选引用范围时**公有/私有分开勾选**；目录勾选**递归**含子目录。
- 删除/覆盖等破坏性操作需写权限持有者 + 审计日志。

## 9. 会话模型（乙+锚点）

- 每个工作区/稿件一份会话，跨句子共享（源于 features §4.5）。
- 消息可携带 `target`（句子/段落锚点），纯对话可不带。
- 用户选中句子 → 上下文面板 → 输入框自动 prefill target。
- 对话流为完整贯穿时间线（不分段分组）。

## 10. 限流 / 熔断 / 配额

- 复用前身可观测与容错：
  - **限流**：Redis 滑动窗口（用户/接口维度；对话接口单独限流）。
  - **熔断**：LLM 客户端三态熔断（Closed/Open/Half-Open）+ 整体时间预算 + 指数退避重试。
  - **配额**：租户 Token 配额（Redis 用量对比）。
- 对话接口等必要接口实现**限流 + 熔断**。

## 11. 前端（简）

- 复用前身零构建静态前端的思路（web/ + Nginx 反代）。
- 页面结构（工作区为中心）：
  - 登录/注册
  - 首页：工作区列表（按修改时间倒序，标题/标签/平台/状态检索）
  - 工作区：左侧侧栏「需求 | 稿件」两阶段 + 对话面板（上下文带 target 锚点）+ 导出
  - 知识库管理：公有/私有库（目录树、上传/覆盖/删除）

## 12. 消息队列（异步）

- 复用前身 mq：RabbitMQ，message 带 `task_id/tenant_id/document_id + msg_id/trace_id`，消费端重建 trace ctx，手动 ACK。
- 队列：
  - `document_parse`：文档上传后异步切片+向量化。
  - `article_generate`：稿件生成（初稿/修订）异步任务。

## 13. 明确不在 MVP 的内容

- 版本删除 / 回退。
- 证据"已有新版本"的提示（仅预留字段）。
- 检索二次重排（rerank）。
- 第三方平台互通发布、审核流程（见 features §9）。

## 14. 目录结构（新项目骨架）

```
content-hub/
├── cmd/  api|worker|migrate|configtest
├── config/config.go
├── api/
│   ├── router.go                       # 公开组/私有组/admin
│   ├── handler/  workspace|requirement|article|kbase|auth|evidence|export|health|audit
│   ├── service/  generation|revision|document_parse|kbase|...
│   ├── middleware/ trace|recovery|logger|cors|JWT|context|ratelimit|quota
│   ├── response/  validator/
├── storage/  mysql|redis|oss|qdrant + model/ + 各业务表
├── agent/
│   ├── orchestrator/                   # 工作流编排（generation/revision）
│   ├── retrieve/                       # 知识检索 agent（ReAct）
│   ├── writing/                        # 稿件撰写 agent（结构化生成）
│   ├── evidence/                       # 证据整理 agent
│   └── dialogue/                       # 需求对话 agent
├── kbase/  dirs|files|versions|slice|vectorize
├── splitter/  splitter.go              # 切片（结构化→自然段→软300+句末截断）
├── llmclient/  client.go|usage.go|types.go
├── mq/  rabbitmq.go|message.go
├── observability/  logger.go|metrics.go
├── util/  jwt.go|password.go
├── web/  login|workspace|kbase|...
├── deploy/  nginx.conf
├── docs/  features|architecture(features.md|architecture.md|db.md)
```

## 15. 待数据表设计（docs/db.md）

- tenants / users / kbase_dirs / doc_chunks / doc_sentences / article_sentences /
  evidence_bindings / workspaces / requirements / conversations / audit_logs /
  agent_tasks / tenant_tool_configs（视需要）

> 具体表字段、索引见下一文档 `docs/architecture/db.md`。
