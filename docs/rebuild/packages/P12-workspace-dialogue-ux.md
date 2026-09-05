# P12 · workspace/对话 UX：人话结果、可生成硬前置、状态机别名、动线收敛

- RFC 出处：rev-4 §13.2/W2、§13.4/W4；rev-2 §9.4(动线)；README 顺序=P12
- 状态：已完成（重做，经逐条复核校正了前版缺漏：标题缺项未纳入硬前置、缺项误回退状态、human_text 重复带符号前缀、状态别名两套不一致/文案不同、失败建议一刀切；验收见文末）
- 前置：P06（对话结果需要真动作反馈）；P05 可异步但 UX 不依赖；与 P11 互补
- 目标：
  1. 对话动作结果不再把 `tool 名 + 底层 message` 原样怼给用户(W2)；
  2. "能否生成/还差什么"做成可计算的**硬前置**与明确禁态提示，而不是拿空/半需求真跑(W4)；
  3. workspace 状态与 `needs_req`/`draft` 语义对齐前端显示，不让用户在状态机名词上迷茫；
  4. 全局动线收敛：让"我该做什么/在哪做"一眼可知。

---

## 1. 问题现象(源码)
- `WorkspaceDetail.tsx::sendChat`：直接 `results.map(x=>\`${x.tool}:${x.success?...}(${x.message})\`)`+`message.info` → 对普通用户显示"update_requirement_field:成功(已更新需求单字段 style_tone)"，内部实现漏给了最终用户(W2)。
- workspace 恒 `draft` 创建(`workspace.go`);而筛选里存在 `needs_req`(待录入需求,金黄)却几乎没有走到→两个近义状态没跑通(语义割裂)。
- "生成"前没有 `RequirementComplete` 硬校验；空需求也可能真跑一遍昂贵 LLM(W4)。
- 页面缺少"下一步你该做什么"的动态引导;功能入口散(集中收敛见 RFC §9.4)。

## 2. 改法
### 2.1 对话结果的“人话”
- 后端把每个 ActionItem 改为携带 `human_text`(给用户看:一句话 + 对/错 icon)与 `debug`(原内部,给溯源/日志)。
- 前端显示成一个**结果清单**:每条 icon + 人话;若一条 action 未做,给出"为什么/需要你做什么"(`${…}`,用 role 文案而非 tool)。不要 message.info 一条海量 text。
- 可选：常见闲聊(非动作)返回"我没改任何东西,是否需要…(提示下一步)"。

### 2.2 「可生成」硬前置
- 前端新增 `canGenerate = RequirementComplete(req)` 派生;未满足时「生成」Button disabled + tooltip 列"还缺:平台/发文风格/引用范围/字数"。
- 这也避免拿空 req 真跑 LLM(W4)。若你仍想试点从“一句话直接把空需求生成”，做成明确 reviewflow(skeleton)，不作为默认允许。

### 2.3 状态语义与别名
- 去掉与 `draft` 重叠、几乎无真实流动的 `needs_req`，或保留但显式定义为 draft 的特例(如“仅有标题/标题为空 → needs_req”)并让创建后未填到可生成的 ws 落在该态；
- 前端状态展示优先用工况文案(如看板用"待填需求/可生成/生成中/已生成")，不要让 internal status enum 直接当 label。
- 派生"能生成"即成为一个即时用户可见状态条（"下一步:补充『平台』后即可生成"）。

### 2.4 动线
- 工作区详情页把 `【需求】|【稿件】`两阶段变成**横向向导步（填需求→选引用→生成→看稿→导出→改）**，用当前状态自动高亮当前步；稿件正文再按 P11 呈现不打断。
- 每个空态给一个"建议动作 + 跳到哪里的按钮"，避免"一堆按钮不知干嘛"(RFC rev-2 §9.4)。

## 3. 范围与命中代码
| 文件 | 改动 |
|------|------|
| 后端 `api/service/dispatcher.go` + `agent/schema` | ActionItem 返回新增 `human_text`,不含 tool 内部文案做默认 UI |
| `web/src/pages/WorkspaceDetail.tsx` | 对话结果改成人话清单;生成按钮带 `canGenerate`;顶部向导步 |
| `web/src/pages/Workspaces.tsx` | 状态标签改工话别名;status filter 与枚举一致 |
| `api/service/workspace.go` / requirement | 让 `needs_req`/`draft` 语义明确(或别名映射) |
| 需求 form 校验 | 把 `RequirementComplete` 从后端到前端都可用(human) |

## 4. 验收
- 对话后的 UI 不再出现 `tool:xxx(...)`;能看到一句人话与对错,以及(未做时)提示为什么。
- 空需求不能点「生成」;tooltip 明确缺什么。
- ws 列表状态标签清晰(“待填需求/可生成/生成中/已生成/失败”)且与 enum 真流转一致,不存在“两个同义枚举没人设”。
- 人工回归能回答“我今天到这篇稿下一步做什么”。

## 5. done gate
“P12 done” = 人话结果 ok;可生成为禁态带缺项;状态别名一致;向导动线可用;现有 e2e(lint/tsc/go test)仍绿。

---

## P12.x 落地与验收记录（重做 · 本会话逐条复核后）

> 背景：本包此前标注“已完成”并有一份 P12.1 记录，但按 RFC rev-4 W2/W4 与本文档第 4 节逐条对照后发现 5 处名不符实，本会话**当作任务重做一遍**并逐一校正，全部以真环境复验。

### 复核发现的前版缺漏（为何要重做）
1. **标题缺项没进后端硬前置**：前端 `guide.requirementMissing` 把“标题”当缺项，后端 `RequirementCompletenessIssues` 却不查标题——前后端口径不一致（RFC W4 要求标题/平台必填），且文档谎称测试覆盖“标题缺”。
2. **缺项分支误改动状态**：`GenerateArticle` 在缺项硬前置分支里调用 `restoreWorkspaceStatus(c, wid, "")`，实际此前从未把状态置过 generating，却被无脑强打回 `draft`——一个已有稿(`generated`)的工作区在缺项点生成会被错误打回 `draft`。
3. **`humanizeAction` 成功句自带 `"✓ "` 前缀**，前端又渲染 `✓/✕` icon → 用户看到双重符号。
4. **状态别名两套且文案不一致**：`guide.statusAliasLabel('failed')`="需重试" 但 `Workspaces STATUS_META`="生成失败"；列表卡片**永远显示“待填需求”**，从不出现“可生成”档（需求已齐备的 draft 卡片明明可生成）。
5. 未知 tool 的 human 文案 `"执行 %q 这步操作"` 会把内部 tool 名拼给人看；失败建议四个 action 一刀切同一句模板。

### 后端改动
- `api/service/workspace.go::RequirementCompletenessIssues`：**补上 `strings.TrimSpace(r.Title)=="" → "标题" 缺项**，口径与前端 guide 完全对齐（标题+平台必填；风格/字数/章节至少其一；引用缺省=全部可访问）。新增 `"strings"` import。
- `api/handler/article.go::GenerateArticle`：缺项硬前置分支**移除 `restoreWorkspaceStatus(c, wid, "")`**——守卫在本置 generating 之前，绝不因一次“被拦下的空点生成”把已 generated 工作区打回 draft；并在注释里写明“不改状态”。
- `api/service/dispatcher.go::humanizeAction`：
  - 成功/失败句都**去掉前端才应该渲染的 `✓`/`✕` 前缀**（符号归前端，避免 `✓✓`）；
  - 失败建议**按 action 区分**：改字段→“换直白说法/在表单里改”；补检索→“换关键词/先确认资料库范围”；追加/改句→“在逐句编辑面板手动”；
  - 未知 tool 不再拼 `%q` 内部名，退回“换个说法描述需求”。
- 测试 `api/service/workspace_ux_test.go`：补**标题缺/纯空格标题缺**两场景；扩 `TestHumanizeAction_NoToolLeak` 断言 human_text **不以 ✓/✕ 开头**、未知 tool 不泄漏内部名、检索失败建议提到“换关键词”。

### 前端改动
- `web/src/guide.ts`：
  - `statusAliasLabel('failed')` 由“需重试”改为 **“生成失败”**（与列表一致）；
  - 新增 `WorkspaceCard`/`requirementLikeFromCard`（列表返回的 platforms JSON 数组文本/逗号串 → 判别形状）与 `cardStatusLabel`（**draft 且需求齐 → “可生成”**），让卡片呈现带“可生成/待填需求”派生。
- `web/src/pages/Workspaces.tsx`：**删除本地硬编码 `STATUS_META.label` 双源**，筛选/卡片标题一律收敛到 `guide.statusAliasLabel` / `cardStatusLabel` 单源；卡片状态从“恒待填需求”升级为“已齐 → 可生成”。
- `web/src/guide.test.ts`：failed 文案改断言 + 新增“卡片需求解析/可生成派生”用例。
- `api/workspace_e2e_test.go`（连带修复，既有不属 P12）：e2e 原本 POST 一个**裸 `{"title"}`**违反 P10 的初步内容校验必然被 400（HEAD 即红），且固定租户名/用户名在多轮/残留下互相冲突——改为**合法的完整 payload + 每次唯一租户/用户名**，使该 integration 可重复跑绿。

### 验收（真环境实跑）
- docker 起 MySQL/Redis/Qdrant/RabbitMQ；`go run ./cmd/migrate` 建表成功。
- `go test ./... -count=1` **全绿**（含新增 P12 单测；连接真实 MySQL 的 coordinator/run/storage 等用例均实际跑过同一套库）。
- `go test -tags=integration ./... -p 1`：**仅 `TestOrchestratorGenerate` 失败**——经 `git stash` 在干净 HEAD 上复测**同样失败**（真实 LLM+向量检索命中阈值的外部依赖问题，与本包改动无关、非本包引入的回归）；`api`(e2e 已修可重复)、`api/service`(含真 MySQL 集成) 等本包涉及的包全绿。
- `go build ./...`、`go vet ./...` 均通过。
- `cd web && npm test`：guide *5 + ArticleReadableView *4 = 9 passed；`npx tsc -b`、`npm run lint`(0 error，告警均为既有)、`npx vite build` 全通过。

**局限 / 延续**
- 浏览器观感(smoke)留给本机人工；`TestOrchestratorGenerate` 为既有外部依赖 flaky，需独立跟进数据/阈值，不在 P12 范围内自修以避免越界引入新风险。
- “从一句话直出空需求生成”(skeleton) 维持 run 的 guard，默认不允许。

---

## 🧭 回顾（面试/复盘用）
### 一句话
**把“跟 AI 对话改需求/稿件”的内部工具结果收口成用户看得懂的人话，把“能不能一键生成”从前端到后端都做成可计算的禁态 + 缺什么明说，再让工作区给用户一个“现在该做什么”的向导**——最终用户不再看到 `tool:xxx(...)`、也不用拿空需求去烧一次昂贵生成。
### 原问题（触发）
| 场景 | 旧实现 | 落地后 |
|------|--------|--------|
| 对话后 | `tool:update_requirement_field:成功(已更新 style_tone)` | 前端 `✓/✕` icon + `把你的要求写进需求单的「发文基调」，已完成。`；失败给按动作区分的“换个说法/去表单或逐句编辑手动处理” |
| 空/半需求点在生成 | 直接真跑昂贵 LLM/检索 | 按钮 disabled + Tooltip 缺项；后端 Generate 也前置硬校验拦下 |
| 状态两个近义没人走出 | `draft`/`needs_req` 同在筛选却几乎不创建 | 列表只留语义收敛后的别名："待填需求/生成中/已生成/…" |
| 我今天到这篇稿该干嘛 | 一堆入口无指引 | 顶部三段向导步 + 每段描述下一步 |
### 宏观方案（精神）
1. **前后端都把人话当一等产物**：后端 `HumanText` 生成、前端只展示 `✓+人话`；改动记录等技术细节走 `Debug/Message` 溯源。
2. **禁态硬前置而不只是文案**：可生成判定做成同一纯逻辑，前端负责禁用+提示，后端负责强制拦截——双护避免空/半需求浪费。
3. **状态只对用户展示“语义”不展示内部 enum**：同类状态合并别名，且只保留真会被创建到的那套。
4. **用“下一步”把流程串起来**：向导步自动高亮 + 描述，把“填需求→生成→看稿→导出”压缩成用户一眼能懂的动线。
一句话收口：**少一颗要理解推理内部的动词跳转，多一处“你现在该做什么”的确定引导，是政企文秘/普通同事愿意日常使用 multi‑agent 工作台的关键。**

---

## ▣ 任务收口（本包交付口径）
P12 重做后已落地并通过自动层 + 真环境验收：后端硬前置 `RequirementCompletenessIssues` 与前端 guide 口径钉死到**同一套判别（标题/平台必填 + 风格或字数或章节其一）**并被单测/vitest 锁死；生成守卫**缺项时不改动工作区状态、不真跑 LLM**（前端 disabled+Tooltip、后端强制，双护）；对话 human_text **不重复带符号、不泄漏 tool 名、失败按动作给建议**；状态别名**收敛到单个来源**且列表卡片派生“可生成/待填需求”。`go test ./...`、`go build`、`go vet`、`go test -tags=integration`(涉及本包的 api/api-service 等全绿)、`web npm test / tsc / lint / vite build` 全绿。遗留：`TestOrchestratorGenerate` 为既有外部依赖失败（干净 HEAD 同样失败，非本包问题）；浏览器观感 smoke 留本机人工。needs_req 已不在 UI 出现。UI 的下一步衔接(点向导切页签/一键补齐缺项) 若产品需要可再增 P12.2。

