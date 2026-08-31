# content-hub 开发实现计划（plan.md）

> 本文档将全部功能（docs/features）与技术架构（docs/architecture）落地为**可执行的模块化步骤**。
> 每完成一步打勾 `[x]`。当前进度：第 1 步已完成。
> 数据表设计见 `docs/architecture/db.md`（18 张表）。

## 阶段 0 · 文档基线（已完成）

- [x] 初始化项目（go.mod / README / .gitignore）
- [x] 功能特性文档 `docs/features/features.md`（v3 定稿 + 增量契约）
- [x] 架构与技术方案 `docs/architecture/architecture.md`
- [x] 数据库设计 `docs/architecture/db.md`

## 阶段 1 · 环境与配置（已完成 ✅）

- [x] 独立中间件 `docker-compose.yml`（content-mysql/redis/qdrant/rabbitmq，独立端口）
- [x] `.env`（含敏感配置，已 gitignore）/ `.env.example` 模板
- [x] `config/config.go` 结构化配置加载（MySQL/Redis/Qdrant/RabbitMQ/JWT/LLM/Embedding/OSS/Log/Chunk/Retrieval）
- [x] 项目目录骨架（按 architecture.md §14）+ `.gitignore` 加固（.env/data 不入库）
- [x] 中间件连通验证（MySQL content_hub 库 / Redis PONG / RabbitMQ ping / Qdrant Up）

## 阶段 2 · 数据表落库（已完成 ✅）

- [x] 编写 GORM model（22 张表：含原 18 张 + retrieval_batches/retrieval_batch_items/qa_sessions/qa_messages；详见 db.md）
- [x] `cmd/migrate` 迁移工具 + 建表（含唯一索引/联合索引 per db.md）
- [x] 验证 22 张表在 content_hub 库生成
- [x] `docs/architecture/db.md` 索引与模型核对（字段/类型/默认值/索引已通过 information_schema 逐表核对无误）

## 阶段 3 · 账号与鉴权模块（已完成 ✅）

- [x] 基础中间件：trace / recovery / logger(结构化含trace) / cors / response / validator
- [x] 租户/用户 model + storage（TenantID 强隔离）
- [x] bcrypt 密码 + JWT 签发/解析（最小载荷 user_id/tenant_id/role）
- [x] 注册/登录/鉴权中间件 + 私有路由组
- [x] Redis 限流（滑动窗口）+ 操作审计日志（audit_logs，trace 贯穿）
- [x] 初始化脚本 / smoke 测试（多租户隔离对抗验证）
- [x] 单元测试（util JWT/bcrypt + middleware 鉴权 + service 注册登录集成测试）

## 阶段 4 · 知识库 kbase 模块（完成 ✅，MQ 异步待补）

- [x] 目录树（公有/私有，递归）kbase_dirs CRUD + 权限（读写分离）
- [x] 文档上传/下载/预览（阿里云 OSS，物理扁平 + 目录逻辑映射）
- [x] 文档版本（doc_versions，只增不减、latest 指针、md5）
- [x] 上传覆盖选择 + 版本一致性兜底（事务保证 latest 唯一 + 失败保留上一版，权限结构天然消除并发写冲突）
- [x] 文档切片 splitter（structured → 自然段 → 软300字 → 完整句末截断）
- [x] 向量化入库 worker（RabbitMQ document_parse 异步队列 + cmd/worker 进程 + 上传投递改异步）
- [x] Qdrant 检索（按租户 collection/payload 隔离 + latest + 勾选范围 document_ids 过滤，top-K=20）

## 阶段 5 · 多 Agent 编排（核心）

- [x] llmclient 封装（DeepSeek Chat + 硅基流动 Embedding、超时/退避/熔断、ChatWithJSON）
- [x] 知识检索 agent（方案乙：LLM 提炼 query → 单次 SearchKbase → 去重汇总，限定勾选范围）
- [x] 稿件撰写 agent（结构化生成 Article，句级证据绑定）
- [x] 证据整理 agent（格式化证据清单，不重检）
- [x] 需求对话 agent：升级为「统一入口 + DialoguePlan 多动作计划 + 字段白名单硬拦截 + JSON Schema 机检」（已真实测试通过）
- [x] Orchestrator（generation 工作流已跑通；revision 编排待阶段6）

## 阶段 6 · 工作区 / 需求单 / 稿件 / 会话 / 导出（完成 ✅）

- [x] workspaces CRUD（本人可见、首页倒序检索）
- [x] requirements CRUD（基础字段 + 任务要求 + requirement_scope 活引用递归展开 + version 字段）
- [x] 新增 retrieval_batches 表（检索快照：doc_sentence 指针 + requirement_version）+ 关联表 retrieval_batch_items
- [x] 对话 agent 升级：统一入口 + DialoguePlan（动作计划）+ 字段白名单硬拦截 + JSON Schema 机检
- [x] 稿件生成工作流接入（generation：需求→检索→撰写→证据→快照完成态）
- [x] 段落/句子 AI 修订（revision 核心：句子级重写 + 未动句继承 + 被改句方案甲重检测）
- [x] 惰性失效：需求单 version + retrieval_batch 过期判定（IsBatchStale + version 对比）
- [x] 会话模型（conversations + conversation_messages 存 action plan JSON，target 锚点，阶段私有）
- [x] 稿件快照/版本落库（article_versions/article_sentences/evidence_bindings）
- [x] 导出（合并 md：正文在前 + 证据清单在后）
- [x] 测试隔离（真实外网集成测试加 build tag，单元/集成全绿）

> 注：完整 dispatcher（对话派发接 DB + 逐 action 执行）与导出"修改中禁导"运行时状态判定，属阶段6 的收尾延伸，未在本次完成（后续补）。

## 阶段 7 · 前端（React + Vite + TypeScript）

### 7A · 补后端 HTTP 接口（完成 ✅）
- [x] 工作区：列表检索(标题/标签/平台/状态) + 新建 + 软删除
- [x] 需求单：读 + 更新字段(version递增) + 保存勾选范围(requirement_scope)
- [x] 知识库：目录树 CRUD + 文件上传(新/覆盖)/删除/预览/下载 URL（公有库仅管理员）
- [x] 稿件：生成触发(generation) + 读取(含句子+证据标注) + 导出(合并 md)
- [x] 知识库问答：纯问答 agent + 独立会话(qa_sessions/qa_messages) + 建会话/提问/读消息

### 7B · 初始化 React 前端工程（完成 ✅）
- [x] 初始化 React + Vite + TypeScript 工程（web/ 目录）
- [x] 配置 axios / 路由 / 基础布局（左侧导航：工作区 | 知识库）

### 7C · 前端页面（完成 ✅）
- [x] 登录/注册
- [x] 首页工作区列表（倒序 + 标题/标签/平台/状态检索）+ 新建/删除
- [x] 工作区详情：左侧「需求 | 稿件」两阶段 + 对话面板 + 导出
- [x] 知识库：公有/私有、左右分栏（左管理 + 右问答对话）、目录树上传/覆盖/删除、预览/下载
- [x] 反代/构建配置（vite proxy）

## 阶段 8 · 收尾

- [x] Prometheus 指标 + 健康检查（/health 依赖状态）
- [ ] 上线前安全清单（JWT 换密钥 / GIN_MODE=release / CORS 白名单 / 传输 HTTPS）
- [x] 端到端冒烟脚本 / 多租户隔离对抗测试
