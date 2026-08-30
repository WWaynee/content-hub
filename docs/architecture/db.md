# content-hub 数据库设计（db.md）

> 数据表设计文档，对应 `docs/architecture/architecture.md` §5、§15。
> 18 张表，按模块分组。物理表名为复数（GORM 约定）。
> 设计原则：
> - 全链路 `tenant_id` 隔离（数据层死守，业务字段强制）
> - 证据锚点 = 句子；切片/句为文档属性去重，稿件句为稿件本体
> - 版本只增不减（MVP 无回退/删除）
> - 稿件为快照式（甲），勾选范围跟随目录（活引用）

## 模块1 · 账号与租户

### tenants 租户
| 字段 | 类型 | 说明 |
|------|------|------|
| id | bigint unsigned PK | |
| name | varchar(128) UNIQUE | 租户名（唯一） |
| status | tinyint default 1 | 1=启用 |
| quota_llm_token | bigint default 0 | LLM token 配额 |
| created_at / updated_at / deleted_at | datetime(3) | |
- 索引：UK `idx_tenants_name(name)`、K `deleted_at`

### users 用户
| 字段 | 类型 | 说明 |
|------|------|------|
| id | bigint unsigned PK | |
| tenant_id | bigint unsigned | 所属租户 |
| username | varchar(64) | |
| password_hash | varchar(256) | bcrypt |
| role | varchar(32) | admin / member |
| status | tinyint default 1 | |
| created_at / updated_at / deleted_at | datetime(3) | |
- 索引：UK `(tenant_id, username)`、K `deleted_at`
- 每租户仅 1 个 admin（应用层强制）

## 模块2 · 知识库 kbase

### kbase_dirs 目录树
| 字段 | 类型 | 说明 |
|------|------|------|
| id | bigint unsigned PK | |
| tenant_id | bigint unsigned | |
| scope | varchar(16) | public / private 库类型 |
| owner_user_id | bigint unsigned | private 库归属人（public 为 0） |
| parent_id | bigint unsigned default 0 | 父目录（0=根） |
| name | varchar(128) | 目录名 |
| created_at / updated_at / deleted_at | datetime(3) | |
- 索引：K `(tenant_id, scope, owner_user_id, parent_id)`
- 公有/私有隔离：公有库 `scope='public'` + admin 可管理；私有库 `scope='private'` + `owner_user_id` 限本人

### kbase_files 文档元数据（逻辑身份）
| 字段 | 类型 | 说明 |
|------|------|------|
| id | bigint unsigned PK | document_id（逻辑身份，跨版本不变） |
| tenant_id | bigint unsigned | |
| scope | varchar(16) | public / private |
| dir_id | bigint unsigned | 所属目录 |
| owner_user_id | bigint unsigned | 上传者（public 可为 0 表示管理员） |
| name | varchar(256) | 文件名 |
| current_version_md5 | varchar(64) | 当前最新版本 md5（指针） |
| file_type | varchar(16) | txt / md |
| size | bigint | 最新版大小 |
| active | tinyint default 1 | 是否可见可检索（删除=0） |
| created_at / updated_at / deleted_at | datetime(3) | |
- 索引：K `(tenant_id, scope, dir_id)`、K `(tenant_id, name)`、K `deleted_at`

### doc_versions 文档版本
| 字段 | 类型 | 说明 |
|------|------|------|
| id | bigint unsigned PK | |
| tenant_id | bigint unsigned | |
| file_id | bigint unsigned | 关联 kbase_files.id |
| version_md5 | varchar(64) | 版本标识（文件内容 md5） |
| version_no | int | 递增版本号（1,2,3…） |
| oss_object_key | varchar(512) | OSS 物理 key（含版本，物理扁平） |
| latest | tinyint default 1 | 是否当前最新版本（admin 校验） |
| status | varchar(32) | pending/processing/success/fail |
| error_msg | text | 失败原因 |
| uploader_user_id | bigint unsigned | 上传者 |
| created_at / updated_at | datetime(3) | |
- 索引：UK `(file_id, version_md5)`、K `(tenant_id, file_id)`、K `latest`
- 上传新版本：旧版 `latest=0`（唯一最新），新版本全链路成功后 `latest=1`

### doc_chunks 文档切片（原文，去重）
| 字段 | 类型 | 说明 |
|------|------|------|
| id | bigint unsigned PK | |
| tenant_id | bigint unsigned | |
| file_id | bigint unsigned | |
| version_md5 | varchar(64) | 所属版本 |
| chunk_index | int | 切片序号 |
| chapter_title | varchar(256) | 章节标题（提取不到留空） |
| content | text | 切片原文（~300 字，完整句末截断） |
| start_char / end_char | int | 原文偏移（供句级定位） |
| created_at | datetime(3) | |
- 索引：UK `(file_id, version_md5, chunk_index)`、K `(tenant_id, version_md5)`
- 版本参与唯一性 → 旧版本切片保留，证据锚定稳定

### doc_sentences 文档句（原文，去重）
| 字段 | 类型 | 说明 |
|------|------|------|
| id | bigint unsigned PK | |
| tenant_id | bigint unsigned | |
| file_id | bigint unsigned | |
| version_md5 | varchar(64) | 所属版本 |
| chunk_id | bigint unsigned | 所属切片 |
| sentence_index | int | 句在切片内序号 |
| content | text | 句原文 |
| start_char / end_char | int | 切片内偏移 |
| created_at | datetime(3) | |
- 索引：K `(file_id, version_md5)`、K `(chunk_id)`
- 被多稿件引用时大家指向同一行（去重）

## 模块3 · 稿件 article

### workspaces 工作区
| 字段 | 类型 | 说明 |
|------|------|------|
| id | bigint unsigned PK | |
| tenant_id | bigint unsigned | |
| owner_user_id | bigint unsigned | 仅创建者本人访问 |
| title | varchar(256) | 工作区标题（需求单标题冗余，首页展示） |
| status | varchar(32) | draft / needs_req（草稿）/ generating / generated / revising / failed |
| created_at / updated_at / deleted_at | datetime(3) | |
- 索引：K `(tenant_id, owner_user_id)`、K `updated_at`（首页倒序）

### requirements 需求单
| 字段 | 类型 | 说明 |
|------|------|------|
| id | bigint unsigned PK | |
| workspace_id | bigint unsigned | 一对一 |
| tenant_id | bigint unsigned | |
| title | varchar(256) | 标题 |
| tags | json | 标签数组（多选） |
| platforms | json | 发布平台枚举数组（公众号/小红书/单位网站/微博） |
| style_tone | varchar(255) | 基调 |
| style_emotion | varchar(255) | 感情色彩 |
| style_audience | varchar(255) | 目标受众 |
| style_purpose | varchar(255) | 发文目的 |
| style_taboo | text | 禁忌话术/其他约束 |
| style_subject | varchar(255) | 发文主体（学校/医院/法院…） |
| word_count | int | 字数要求 |
| chapter_requirement | text | 章节要求（文字说明） |
| created_at / updated_at | datetime(3) | |
- 索引：K `(workspace_id)`、K `(tenant_id)`、K `title`

### requirement_scope 需求单引用范围（活引用，跟随目录）
| 字段 | 类型 | 说明 |
|------|------|------|
| id | bigint unsigned PK | |
| requirement_id | bigint unsigned | |
| tenant_id | bigint unsigned | |
| scope_type | varchar(16) | public / private |
| target_type | varchar(16) | dir / file |
| dir_id | bigint unsigned | target_type=dir 时 |
| file_id | bigint unsigned | target_type=file 时 |
| created_at | datetime(3) | |
- 索引：UK `(requirement_id, scope_type, target_type, dir_id, file_id)`
- 目录为活引用：检索时递归实时展开子目录文件（当前目录树动态变化跟随生效）

### articles 稿件（快照式）
| 字段 | 类型 | 说明 |
|------|------|------|
| id | bigint unsigned PK | |
| workspace_id | bigint unsigned | |
| tenant_id | bigint unsigned | |
| current_version_no | int | 当前稿件版本号（每次生成/修订完成态 +1） |
| title | varchar(256) | 稿件标题 |
| status | varchar(32) | none（未生成）/ generated / revising（修改中）/ failed |
| created_at / updated_at | datetime(3) | |
- 索引：K `(workspace_id)`

### article_versions 稿件版本（快照，完成态）
| 字段 | 类型 | 说明 |
|------|------|------|
| id | bigint unsigned PK | |
| article_id | bigint unsigned | |
| workspace_id | bigint unsigned | |
| tenant_id | bigint unsigned | |
| version_no | int | 1=初稿，2+ 修订 |
| full_content | longtext | 整篇稿件正文（markdown） |
| status | varchar(32) | completed（完成态，可导出）/ failed |
| referenced_version | int | 本次基于的稿件版本（修订时） |
| created_at | datetime(3) | |
- 索引：UK `(article_id, version_no)`
- 快照式：每次生成/修订完成形成新版本，可导出对应此快照

### article_sentences 稿件句（稿件本体）
| 字段 | 类型 | 说明 |
|------|------|------|
| id | bigint unsigned PK | |
| article_version_id | bigint unsigned | 所属稿件版本 |
| workspace_id | bigint unsigned | |
| tenant_id | bigint unsigned | |
| section_index | int | 章节序号 |
| paragraph_index | int | 段落序号 |
| sentence_index | int | 句序号 |
| content | text | 句文本 |
| created_at | datetime(3) | |
- 索引：K `(article_version_id)`、K `(workspace_id)`

### evidence_bindings 证据绑定（稿件句 ↔ 文档句）
| 字段 | 类型 | 说明 |
|------|------|------|
| id | bigint unsigned PK | |
| article_version_id | bigint unsigned | |
| article_sentence_id | bigint unsigned | 稿件句 |
| tenant_id | bigint unsigned | |
| source|type | varchar(32) | 证据来源类型：knowledge（知识库）；none 无源 |
| doc_file_id | bigint unsigned | 来源文档（knowledge 时） |
| doc_sentence_id | bigint unsigned | 来源文档句 |
| evidence_status | varchar(32) | bound（已绑定）/ no_source（无源待核对）|
| order_no | int | 多证据排序（引用先后） |
| created_at | datetime(3) | |
- 索引：UK `(article_sentence_id, doc_sentence_id)`、K `(article_version_id)`
- 一句可绑多个文档句（多来源）

## 模块4 · 会话

### conversations 会话（工作区一份）
| 字段 | 类型 | 说明 |
|------|------|------|
| id | bigint unsigned PK | |
| workspace_id | bigint unsigned | 一对一 |
| tenant_id | bigint unsigned | |
| owner_user_id | bigint unsigned | |
| created_at / updated_at | datetime(3) | |
- 索引：K `(workspace_id)`

### conversation_messages 会话消息（带 target 锚点）
| 字段 | 类型 | 说明 |
|------|------|------|
| id | bigint unsigned PK | |
| conversation_id | bigint unsigned | |
| tenant_id | bigint unsigned | |
| owner_user_id | bigint unsigned | |
| role | varchar(16) | user / assistant / tool |
| kind | varchar(16) | question / answer / tool_call / tool_result / system |
| content | text | 消息全文 |
| target_type | varchar(16) | none / sentence / paragraph / requirement_field |
| target_ref | bigint unsigned | 目标句子/消息引用（外键到 article/req） |
| trace_id | varchar(128) | 全链路 trace |
| created_at | datetime(3) | |
- 索引：K `(conversation_id)`、K `(tenant_id, owner_user_id)`

## 模块5 · 系统支撑

### agent_tasks 异步任务
| 字段 | 类型 | 说明 |
|------|------|------|
| id | bigint unsigned PK | |
| tenant_id | bigint unsigned | |
| task_type | varchar(64) | document_parse / article_generate |
| biz_id | bigint unsigned | 关联业务 id |
| status | varchar(32) | pending/processing/success/failed |
| error_msg | text | |
| retry_count | int | |
| created_at / updated_at | datetime(3) | |

### audit_logs 审计日志
| 字段 | 类型 | 说明 |
|------|------|------|
| id | bigint unsigned PK | |
| tenant_id | bigint unsigned | |
| user_id | bigint unsigned | |
| operation | varchar(128) | |
| trace_id | varchar(128) | |
| content | text | |
| created_at | datetime(3) | |

---

## 关键关系图

```
workspaces 1──1 requirements ──1..N─ requirement_scope
   │  1──1 (agent 会话) conversations 1──N conversation_messages
   │
   └── articles 1──N article_versions 1──N article_sentences
                                               │ N──1
                                         evidence_bindings ├─ sourceType=knowledge: doc_sentences ──N──1 doc_chunks ──N──1 doc_versions(version)
```

## 未决/说明

- 18 张表：tenants / users / kbase_dirs / kbase_files / doc_versions / doc_chunks / doc_sentences / workspaces / requirements / requirement_scope / articles / article_versions / article_sentences / evidence_bindings / conversations / conversation_messages / agent_tasks / audit_logs
- 平台枚举（公众号/小红书/单位网站/微博）：存代码常量（features §4.2 约定的枚举写死），requirements.platforms 存 json 引用代码常量值。
- 后续增强预留：article_sentences 及 evidence_bindings 已能支撑"证据是否有新版本"标注（比对 doc_sentences.version 与该文档 current_version）；提示功能二期实现。
