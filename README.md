# content-hub

多 agent 架构的**政企内容管理平台**。面向政企单位的普通工作人员和有限的管理员，提供两大能力：

1. **单位内部知识库管理**：管理公/私有的 txt / md 文字资料（带目录的网盘形式）。
2. **稿件生成工作流**：基于知识库检索资料，自动生成政企发布的文字稿件，并输出稿件与证据原文的对应关系（句级可溯源）。

核心价值：让生成的稿件**有据可查、来源可溯**——稿件中的每个句子都能回指到知识库中的原始文字片段。

## ✨ 功能

- **知识库**：公有/私有库、目录树、上传（新文件/覆盖旧版本）、删除、在线预览/下载、版本管理（旧版保留但不检索）
- **稿件工作流**：工作区 → 需求单（表单 + 勾选引用范围）→ AI 生成稿件（检索→撰写→证据）→ 句子级证据绑定 → 导出（md 正文 + 证据清单）
- **多 agent 对话**：需求单界面对话（改字段）、稿件界面对话（改句子/追加段落/补检索），DialoguePlan 动作计划 + 字段白名单硬拦截
- **知识库问答**：独立会话，纯检索知识库并回答
- **多租户隔离**：租户间数据完全隔离（含向量检索隔离）

## 🛠 技术栈

| 分类 | 技术 |
|------|------|
| 后端 | Go + Gin |
| 数据库 | MySQL 8 + GORM |
| 缓存/中间件 | Redis / Qdrant 向量库 / RabbitMQ 消息队列 |
| 对象存储 | 阿里云 OSS |
| 大模型 | DeepSeek（对话）+ 硅基流动（Embedding，4096 维） |
| 前端 | React + Vite + TypeScript |
| 可观测 | 结构化 JSON 日志 + 全链路 TraceID + 审计日志 + Prometheus 指标 |
| 鉴权 | bcrypt + JWT（载荷 user_id/tenant_id/role）+ Redis 限流 |

## 📁 目录结构

```
content-hub/
├── cmd/
│   ├── api/          # HTTP 服务入口
│   ├── worker/       # 异步 worker（文档解析向量化，消费 document_parse 队列）
│   ├── migrate/      # 建表迁移工具
│   └── configtest/   # 配置自检工具
├── config/           # 配置加载（.env）
├── api/
│   ├── handler/      # HTTP handler
│   ├── middleware/   # trace/recovery/logger/cors/jwt/context/ratelimit
│   ├── response/     # 统一返回
│   ├── service/      # 业务层（acount/kbase/generation/revision/dispatcher/qa/export/...）
│   └── validator/    # 统一参数校验
├── agent/
│   ├── retrieve/     # 知识检索 agent
│   ├── writing/      # 稿件撰写 agent
│   ├── evidence/     # 证据整理 agent
│   ├── dialogue/     # 需求对话 agent（DialoguePlan）
│   ├── qabot/        # 知识库问答 agent
│   └── orchestrator/ # 工作流编排 + Reviser
├── storage/          # MySQL/Redis/OSS/Qdrant + model + 各模块存储层
├── splitter/         # 文档切片（完整句末软截断）
├── llmclient/        # DeepSeek/硅基流动客户端 + 熔断 + JSON 容错
├── mq/               # RabbitMQ 封装
├── observability/    # 日志 + Prometheus 指标
├── util/             # JWT / 密码
├── web/              # React 前端
├── scripts/          # 冒烟测试脚本
└── docs/             # features / architecture / db / plan
```

## 🚀 启动

```bash
# 1. 启动中间件（MySQL/Redis/Qdrant/RabbitMQ，已在跑可跳过）
docker compose up -d

# 2. 配置（如尚未生成）
cp .env.example .env   # 填入 OSS / LLM / 各中间件配置

# 3. 建表迁移
go run ./cmd/migrate

# 4. 后端 API
go run ./cmd/api

# 5. 异步 worker（文档解析）
go run ./cmd/worker

# 6. 前端（可访问 http://localhost:5173）
cd web && npm install && npm run dev
```

健康检查（真实探活各中间件）：`http://127.0.0.1:8181/health`
Prometheus 指标：`http://127.0.0.1:8181/metrics`

## ✅ 测试

```bash
# 单元测试（快，不依赖外部服务）
go test ./... -count=1

# 集成测试（真实依赖 LLM/OSS/Qdrant/MySQL，需服务配置齐 + 串行执行）
go test -tags=integration ./... -count=1 -p 1

# 端到端冒烟 + 多租户隔离对抗（需后端 api + worker 已启动）
bash scripts/smoke_e2e.sh

# 前端 lint / 编译检查
cd web && npm run lint && npx tsc -b
```

## 📐 架构文档

- 功能特性：`docs/features/features.md`
- 技术架构：`docs/architecture/architecture.md`
- 多 Agent 方案：`docs/architecture/multi-agents.md`
- 数据表设计：`docs/architecture/db.md`
- 开发计划：`docs/plan.md`
