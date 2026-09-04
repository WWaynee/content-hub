# P10 · 拿来稿起稿：draft_assist（外部稿粘贴并入轨，而不是被当需求搜掉/无据打死）

- RFC 出处：rev-4 §13.5 / W5；README 顺序=P10
- 状态：已完成（单测 / 真集成 / 真实 E2E 均已验，回顾见文末「🧭 回顾」）
- 前置：P09（复用 run 治理）、P04
- 目标：给"我手上已有一份类似的稿子"这种高频真实开始方式一条通的路，而不是只能从"需求单→从零生成"。

---

## 1. 为什么需要
现有唯一入口是"新建工作区→填需求单→生成"。对已有一份随手稿的政企文案：粘贴进去该走哪条路？现状会把贴的东西当"需求/素材"检索或空白打断，用户体验是碰壁。需求分析（rev-4 W5）点破这是两套真实入口被塞在一个表单里没走通。

## 2. 方案(data 契约)
- 在需求单层加 `source_kind: build_from_scratch | draft_assist`。
- `draft_assist`：用户贴自己的草稿文本(可选字段) → 系统不把它当 KBase 素材搜,而是：
  1. 先用现有 `splitter.Split`（复用，完整句末断）把草稿切成句/段：
  2. 逐句按 P04 到 KBase 检索能配上的原文 → `source_type=knowledge / bound`；
  3. 匹配不上且是用户自己写的背景内容 → `source_type=user_draft`(新)，默认 `plausible/none` 待人工确认(黄点,P09 语义),既服从"别编 KBase 没有的数字"，也不至于拿"没进库"逼用户放弃原稿。
- 于是 "拿用户自己的稿(skeleton) + KBase 佐证/补漏/去重" 才是目标：对 user_draft 的事实点若用户没有资料，按 no_source 处理；用户的写作优先被保留但不冒充 bound。

## 3. 可执行步骤
1. `model.Requirement` 或 workspace 加 `source_kind`；REST 允许 `draft_input`(text)。
2. 前端新建工作区给两个 radio：`从零生成` / `我有初稿要补充整理`(贴文本或 import)。
3. 后端：`draft_assist` 时用 `splitter.Split(draft)` → 产出 `draft_sentences`；把这些当作 `run:revision` 的特殊 base(不需要基础 article 已存在？若是新建则先建壳),再对每枚跑 P09 式治理：attach knowledge-evidence 或 user_draft/none。
4. 数据呈现：给每个 source 标来源 `user_draft` 或 `knowledge`；tooltip(P11)与导出清单区分显示。
5. 让 draft_assist 与 build_from_scratch 能有**同一套**后续管线(P06/P07/revision)。

## 4. 兼容/开放
- 若新建时没有文章存在要先生成 v0/skeleton(空 article_version)作为 base。需要拍板：v0 可否允许"未生成"由 user_draft + knowledge 直接成型?——实现可选"先建占位 version 再改"。默认走 P08 的 change_list=insert all，base=latest 0 或不要求 base（首次 insert 不冲突）。
- 对粘贴的长篇大段：默认只句段级治理，不做整篇富编辑(与 P09 一致),避免把"带真实来源要求"放得过低。

## 5. 验收
- 端到端:新建一个 draft_assist 工作区贴一段“我司 2023 开展校园招聘共 40 场”并附 KBase 有该 doc:
  - 若 doc 有→bound 到 KBase；
  - doc 无（但草稿写了数字）→这个断言句命 no_source/黄点让用户决定（而不直接丢）。
- 导出时要能在 “来源” 区分 `knowledge` vs `user_draft`。

## 6. done gate
“P10 done” = draft_assist 走通贴稿→ split→治理→v 存稿;user_draft no KBase 也保留并给黄点;导出区分来源;不破坏纯从零路径。

---

## P10.1 落地与验收记录（本会话完成）

**数据/契约**
- `storage/model/workspace.go`：`Requirement` 新增 `source_kind`（默认 `build_from_scratch`）、`draft_input`(longtext)。`cmd/migrate` AutoMigrate 兼容加列（已实跑）。
- `storage/model/agentrun.go`：`RunType` 新增 `RunDraftAssist=draft_assist`（coordinator/状态机不校验 run_type，直接入 run：`agent_runs` 可回放可审）。
- `POST /workspaces` 传 `source_kind`/`draft_input`：`build_from_scratch` 沿用原“需求单初步内容”必填校验；`draft_assist` **放宽**（需求单标题/平台等可空，标题缺省回退工作区标题，仅要求草稿非空）。
- 新端点 `POST /workspaces/:workspace_id/draft-assist`(`api/handler/workspace.go::DraftAssistArticle`)。

**执行链 `api/service/draftassist.go::RunDraftAssist`**
1. 取草稿（次数以请求 text 优先、否则 `req.draft_input`），空 → `ErrDraftEmpty`；
2. **仅对空稿**起稿：尚无 `Article` 先建(current=0)；已有版本 >0 → `ErrDraftAssistOnlyOnce`(需走受控编辑/重生成)，杜绝二次起稿覆盖版本；
3. 建 `run(draft_assist)`(active 排他)→ `parseDraftParagraphs`(空行分段 + 段内多行硬换行聚合 + `#`标题前缀剥除) → 段内 `splitter.Sentences` 完整句末断句(planner step)；
4. 每句走 **P09 真校验链** `GovernManualSentence`(以包级 `draftGovernorFn` 注入、测试脱 LLM/Qdrant 可确定性验证)：
   - `bound`(守到可引知识原文) → 该句绑 `knowledge/bound`，唯一句可溯源；
   - `no_source`(该据断言但库内无据) → 该句落 `source_type=user_draft` + `evidence_status=no_source` 的占位行(黄点、待作者取舍)，不冒充有据、不丢用户文字；
   - `plausible`(纯衔衔接/叙述) → 无绑定、不黄（别把没新事实的句子误标该有据）；
   - 治理链路异常 → 保守按 user_draft/no_source 落黄(fallback)，绝不因治理丢稿或谎称有据。
5. `CAS 0→1` 乐观锁推进 → 事务落 `article_version v1 + article_sentences + bindings`（bound 绑真源；user_draft 占位以 `doc=0` 分离，不与 knowledge 混淆）；`FinishRunOk` + `workspace.status=generated`；失败 `FailRun` 并回退 workspace 状态。
6. 与从零生成共用后续管线：产出的 v1 即普通 `article_version`，P08 受控编辑 / P04 溯源 / P09 取舍 / 导出天然全接。

**导出区分**：`api/service/export.go` 对 `user_draft` 占位（doc=0、evidence_status∈{no_source,human_kept}）输出“来源：用户草稿（无外部依据·待人工复核）”+人话说明，与 `knowledge`(来源文档行) 在清单里并列可区分，交审单人核对。

**前端**：`Workspaces.tsx` 新建弹窗加“起稿方式”Segmented（从零生成 / 我有初稿要整理），draft_assist 收起整块需求单初步内容、展开草稿 textarea；`WorkspaceDetail.tsx` 对 draft_assist 且未成稿的 workspace 展示“从这份草稿整理成稿”入口；`types.ts` 的 `Requirement` 补 `source_kind`/`draft_input`。

**验收（实跑）**
- 纯单测：`go test ./... -count=1` **全绿**（含新增 `TestParseDraftParagraphs`）。
- 集成：新增 `api/service/draftassist_integration_test.go`(真 MySQL + fake governor 注入，确定性)：建 draft_assist workspace → 3 句草稿治理(bound/无据断言/衔接)落 v1 → 断言 source_type 落库区分( `knowledge/bound` vs `user_draft/no_source`、衔接无绑定) → 二次起稿被 `ErrDraftAssistOnlyOnce` 拒 → run success+释放 active → 导出含“用户草稿”标注。`go test -tags=integration ./api/service/ -run TestDraftAssist` **PASS**。
- 真实 E2E（真 DeepSeek+Embedding+Qdrant+worker 向量化+MySQL，脚本 /tmp/p10_e2e.sh）：上传真实 `policy.md`(报名/录取) → 建 draft_assist(只填标题、验证放宽) → 勾选范围锁文件 → `POST draft-assist` 贴草稿：两句复述文档的草稿被真治理判 **bound**，读侧显示来源 `policy.md · ## 报名条件 / ## 录取规则`，导出对 applicatives 行原句可溯；两条“该据断言但库内无源”判 **user_draft** + no_source，导出标“用户草稿（无外部依据·待人工复核）”，与 knowledge 清晰区分。**全链路成稿闭环**。
- 前端：`npx tsc -b` **通过**、`npm run lint` **0 error**（告警均既有）。
- 备注（对"两个既有集成为何红"的追根，含后续修复）：排除了相似度阈值假设（降 `KBE_MIN_SCORE` 至 0.15 仍 0 命中）。**真根因**是它俩（及 `TestAppendArticleContent`）以 `ScopePrivate` 存入仓库、却用**不带 user 身份的 ctx** 检索——P01 可见性平面规定"无身份检索仅见公库"，于是这些私有点从 filter 层面被排除、稳定 0 命中（与 score 无关）。已按 P01 模型给这三处私库检索注入文档 owner=1 身份 ctx（`observability.WithTenantUser`），使其真命中；`TestIngestAndSearch`/`TestRetrievalBatchLifecycle`/`TestAppendArticleContent` 连续多轮全绿，service 集成串行一次整体 `ok`。P10 本身仍不触碰检索/embedding/qdrant 层。

**代码位置**：新增 `storage 字段`、`storage/model`（source_kind/draft_input、RunDraftAssist）、`api/service/draftassist.go(+_test/integration_test)`、`api/handler/workspace.go`、`api/router.go`；改 `storage/model/{workspace,agentrun}.go`、`api/service/{workspace,export}.go`、`web/{Workspaces,WorkspaceDetail,types}.tsx/ts`。

**不改之处**：纯从零路径(生成逻辑/必填校验)完全保持；P09 治理、P08 sequence、P04 sources 均不改其对外行为，仅被 draft_assist 复用与新增导出分支。

---

## 🧭 回顾（面试/复盘用）：P10 到底解决了什么

### 一句话
**给“我手上已经有一份稿子”这种政企文案的真实高频开始方式一条走得通的路：把你贴进来的稿子当成“骨架”，系统先切成完整句，再逐句到知识库找能配上的原文——配得上就标成可溯源的 bound；配不上却是你刻意写的数据/断言，就如实标成“用户草稿·无外部依据·待你复核”的黄点保留，既不冒充有据、也不因为“没进库”就逼你把原稿丢掉。**

### 在什么场景会遇到问题（P10 之前的困境）
| 场景 | 触发 | P10 之前的后果 |
|------|------|------|
| “我有往期范文/模板/自己写的半成品” | 想以此为一版草稿新起一篇 | 系统只有“新建工作区→填需求单→生成”一条路，贴上来的东西会被当“需求/素材”检索、或当空需求打断——有稿要整理，却无路 |
| 草稿里有你自己的数字/背景 | 例“2024 办了 40 场招聘”其实是你自己知道的口径 | 走生成时可能因“知识库里查不到”被拒/被删，或反过来被当成“该有据却没据”整个被打回 |
| 想复用自己语言但又要 KBase 佐证 | 已有一份像样的稿子，只想补漏/去重/加证据 | 没有“把稿子带进系统再和资料核对”的载体，只能从头喂需求让 AI 重写，丢了你自己想要的原话 |
| 起点不同却要被框进同一表单 | “从零写”和“拿稿改”本是两套真实开始方式 | 塞在一个必填表单里，第二种根本没被识别，前端只会让用户在一条死路里转 |

> 本质：**两套真实入口（从零要写 vs. 已有稿要整理）被旧系统塞成一个表单只走通了前者**；“早有一份稿”是更省事、更常见的开始，却因为没有“草稿→证据对齐”的通路而碰壁。

### 宏观方案（精神，非代码）
1. **需求单层显式立起稿来源**：`draft_assist` 与 `build_from_scratch` 是两个一等起点，前端让用户一开始就选，而不是先造一个空对象再猜。
2. **草稿 = 骨架，不是素材**：把你贴的东西按“空行分段 + 完整句末断”切成一段段、一句句，作为文章本体；切完后不是拿去当 KBase 检索 query，而是反过来**逐句去资料库里给它找可引原文**。
3. **三态落地、来源如实分类**：一句配得上 → `knowledge/bound`(可溯源)；配不上但你确有它的断言 → `user_draft/no_source`(黄点、作者取舍)；本就是衔接/叙述 → `plausible`(不黄)——绝不在“我有没有据”上替作者撒谎，也不逼作者为“没进库”放弃自己的话。
4. **拿一张既有 v1 当后续一切路线的入口**：起稿产出的是一个普通 `article_version`，之后受控改句/补检索/导出/溯源/鉴审全部复用同一条既有管线，不造第二次生成体系。
5. **复用 R 09 治理与 run/CAS 地层**：逐句校验走 P09 真治理链（服务端判据、非模型自报），整次过程固化为 `run:draft_assist` 持久体（可回放可审），版本用乐观锁防并发写坏——与 AI 生成一样地基可信。

一句话精神：**“从我这份现成的稿子出发”应当和“从零让 AI 写”一样是受尊重的一等入口；能配上资料的可溯源，配不上但我坚持保留的就明说“用户草稿·待核”，而不是把它当需求搜掉、当无据打死、或假装有据。**

---

## ▣ 任务收口（本包交付口径）

**一句话状态**：P10(draft_assist 拿来稿起稿)已**全部完成并多层通过**：契约/执行/读侧/导出/前端全链路落地；`go test ./...`、service 集成串行一次全程 `ok`、新增集成 `TestDraftAssist_*`、真实 LLM+Embedding+Qdrant+worker+MySQL 端到端起稿均绿；前端 `tsc -b`/`npm run lint` 0 error。对早先误记的"两既有检索测试是否环境性失败"已追根并修复（见上一条备注）：非相似度阈值、非 P10，而是它们以私库文档 + 不带 owner 身份 ctx 检索，被 P01 可见性平面规则排除致 0 命中——已为三处私库检索注入文档 owner=1 身份 ctx 修复，现稳定全绿。

**复盘要点（写在简历/评审里的三条）**
1. "我手上已有一份稿"与"从零要写"是两套真实开始方式——P10 把前者立成一个一等入口(`source_kind=draft_assist`)，而不是塞进必须填需求单的旧表单里当"素材/需求"搜掉。
2. 逐句用 P09 治理真链做"来源如实分类"：`knowledge/bound`(可溯) / `user_draft+no_source`(无据断言留黄点交作者取舍、不冒充不丢稿) / `plausible`(衔接不黄)——绝不让"我有没有据"成一句撒谎，也让"没进库"不逼用户放弃原稿。
3. 复用既有一致性地层：产 v1 即普通 `article_version` 与后续受控编辑/P04 溯源/导出/审计自然接轨；创建放宽校验、治理服务端判据、版本乐观锁 + `run:draft_assist` 可回放——与 AI 生成同一地基，不造第二套体系。

**遗留/后续（明确不做在本包）**：整篇富文档编辑不做(沿用 P09 句/段级受控)；草稿对长篇大段默认只做句段级治理；P11(前端结构化渲染 + 句内 source tooltip / 黄点交互的可视化兑现)、P12(workspace/对话 UX)是其直接下游，工作区 `draft_assist` 挂的后续体验在 P11/P12 收口。


