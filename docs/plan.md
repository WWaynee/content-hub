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

- [x] 编写 GORM model（18 张表：tenants/users/kbase_dirs/kbase_files/doc_versions/doc_chunks/doc_sentences/workspaces/requirements/requirement_scope/articles/article_versions/article_sentences/evidence_bindings/conversations/conversation_messages/agent_tasks/audit_logs）
- [x] `cmd/migrate` 迁移工具 + 建表（含唯一索引/联合索引 per db.md）
- [x] 验证 18 张表在 content_hub 库生成
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
- [x] 上传覆盖选择 + 乐观锁兜底 + 上传/解析失败兜底语义
- [x] 文档切片 splitter（structured → 自然段 → 软300字 → 完整句末截断）
- [~] 向量化入库 worker（当前同步 ProcessDocument；RabbitMQ document_parse 异步队列待阶段5接 MQ）
- [x] Qdrant 检索（按租户 collection/payload 隔离 + latest + 勾选范围 document_ids 过滤，top-K=20）

## 阶段 5 · 多 Agent 编排（核心）

- [x] llmclient 封装（DeepSeek Chat + 硅基流动 Embedding、超时/退避/熔断、ChatWithJSON）
- [x] 知识检索 agent（方案乙：LLM 提炼 query → 单次 SearchKbase → 去重汇总，限定勾选范围）
- [x] 稿件撰写 agent（结构化生成 Article，句级证据绑定）
- [x] 证据整理 agent（格式化证据清单，不重检）
- [~] 需求对话 agent：已实现"自然语言→结构化操作"骨架；待升级为「统一入口 + DialoguePlan 多动作计划 + 字段白名单硬拦截」（见阶段6）
- [x] Orchestrator（generation 工作流已跑通；revision 编排待阶段6）

## 阶段 6 · 工作区 / 需求单 / 稿件 / 会话 / 导出（下一步）

- [ ] workspaces CRUD（本人可见、首页倒序检索）
- [ ] requirements CRUD（基础字段 + 任务要求 + requirement_scope 活引用递归展开 + version 字段）
- [ ] 新增 retrieval_batches 表（检索快照：doc_sentence 指针 + requirement_version）
- [ ] 对话 agent 升级：统一入口 + DialoguePlan（动作计划）+ 字段白名单硬拦截 + JSON Schema 机检
- [ ] 稿件生成工作流接入（generation：需求→检索→撰写→证据→快照完成态）
- [ ] 段落/句子 AI 修订（revision：对话派发→检索/撰写/证据重建，未动句继承）
- [ ] 惰性失效：需求单 version + retrieval_batch 过期判定（version 变化后禁止局部修订，先全量重生成）
- [ ] 会话模型（conversations + conversation_messages 存 action plan JSON，target 锚点，阶段私有）
- [ ] 稿件快照/版本与导出状态机（初稿即完成态/N次修改完成态/修改中禁导）
- [ ] 导出（合并 md：正文在前 + 证据清单在后 + 证据整理 agent 格式化）

## 阶段 7 · 前端（静态零构建）

- [ ] 登录/注册
- [ ] 首页工作区列表（倒序 + 标题/标签/平台/状态检索）
- [ ] 工作区「需求 | 稿件」两阶段 + 对话面板（target 锚点）+ 导出
- [ ] 知识库管理（公有/私有、目录树、上传/覆盖/删除）
- [ ] Nginx 反代接入

## 阶段 8 · 收尾

- [ ] Prometheus 指标 + 健康检查（/health 依赖状态）
- [ ] 上线前安全清单（JWT 换密钥 / GIN_MODE=release / CORS 白名单 / 传输 HTTPS）
- [ ] 端到端冒烟脚本 / 多租户隔离对抗测试
