# P12 · workspace/对话 UX：人话结果、可生成硬前置、状态机别名、动线收敛

- RFC 出处：rev-4 §13.2/W2、§13.4/W4；rev-2 §9.4(动线)；README 顺序=P12
- 状态：已完成（后端人话/debug + 硬前置 + 前端别名/向导/禁态均已落地，验收见文末）
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

## P12.1 落地与验收记录（本会话完成）

**后端**
- `api/service/workspace.go`：新增 `RequirementCompletenessIssues(req)`——把“能否一键生成”收敛为 发布平台 + （风格/字数/章节至少其一），引用范围缺省=全部可访问允许；返回人话缺项清单（空=可生成）。
- `api/handler/article.go::GenerateArticle`：生成前先做硬前置——缺项时不动 run/不落库/不真跑 LLM，直接响应“还缺：…”（W4 禁态到后端强约束）。
- `api/service/dispatcher.go`：`ActionResult` 拆出 `HumanText`(人话)/`Debug`，保留 `Message` 内部文案；成功/失败句一律不给用户展示 tool 名（更新字段用中文“发文基调”等 `fieldCN`；失败给“能否换个说法或去对应板块”）。原有 unit 兼容。
- 新单测 `api/service/workspace_ux_test.go`（纯单测，无外部）：
  - `TestRequirementCompletenessIssues`：缺平台/缺规格/标题缺/具备→可生成共 5 场景；
  - `TestHumanizeAction_NoToolLeak`：成功句与失败句都不泄漏 `update_requirement_field`/`style_tone` 等内部名。

**前端**
- 新纯逻辑模块 `web/src/guide.ts` + 单测 `guide.test.ts`：`requirementMissing`/`isRequirementReady`（判别与后端口径一致）、`splitPlatforms`、`statusAliasLabel`（收敛 draft/needs_req  为“待填需求”/“可生成”）。
  - vitest 覆盖 4 组：缺项判定、字数/章节视为规格、逗号平台串、别名收敛。
- `web/src/pages/WorkspaceDetail.tsx`：
  - “可生成”派生 `canGen`；「生成稿件」在缺项时 **disabled + Tooltip 列出还缺项**（不误点、不在前端放空跑 LLM）；即使用户强点，前端先 warning、后端 Generate 也会硬拦，双护。
  - 需求对话结果不再 `tool:xxx(mesg)`；改为「结果清单」逐条 `✓/✕ + human_text`（来自后端 HumanText），空 results(闲聊)给“这条我没动手，需要我改字段/加段落/补检索就说”。Divider 文案改成面向用户。
  - 顶部新增 **三段横向向导步**：`1 填需求 → 2 生成稿件 → 3 看稿/导出`，按 `canGen`/是否有稿自动高亮并给出“下一步”（如“还缺发布平台”）。
- `web/src/pages/Workspaces.tsx`：去掉几乎不走的 `needs_req` 选项；draft 卡片名从“草稿”改为“待填需求”（两个近义不再并存、筛选/标签与 enum 一致）。

**验收（本会话实跑）**
- `cd web && npm test`：guide *4 + ArticleReadableView *4 全绿（8 passed）。
- `npx tsc -b`、`npm run lint`(0 error，8 均为既有告警)、`npx vite build` 均通过。
- `go test ./... -count=1` 全绿（含新增 P12 单测）。
- `go build ./...` 通过。

**局限 / 延续**
- 无浏览器环境的走查留给本机：`npm run dev` 打开一个 draft 工作区确认向导步、禁态 tooltip、对话“人话结果”观感；纯逻辑与编译均已由上面覆盖。
- “从一句话直出空需求生成”(skeleton) 维持 run 的 guard，默认不允许，如需试点另开 reviewflow。

---

## 🧭 回顾（面试/复盘用）
### 一句话
**把“跟 AI 对话改需求/稿件”的内部工具结果收口成用户看得懂的人话，把“能不能一键生成”从前端到后端都做成可计算的禁态 + 缺什么明说，再让工作区给用户一个“现在该做什么”的向导**——最终用户不再看到 `tool:xxx(...)`、也不用拿空需求去烧一次昂贵生成。
### 原问题（触发）
| 场景 | 旧实现 | 落地后 |
|------|--------|--------|
| 对话后 | `tool:update_requirement_field:成功(已更新 style_tone)` | `✓ 把你的要求写进需求单的「发文基调」`；失败给“换个说法/去对应板块” |
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
P12 已全部落地并交给自动层可确定性验收：后端 2 条新单测覆盖缺项判定与人话无泄漏；前端 guide 纯逻辑被 vitest 锁死（冗余收敛/可生成/分隔平台）；detail 接了“禁态+Tooltip+三段向导+对话人话清单”，workspaces 别名收敛。`go test ./...`、`go build`、`web npm test / tsc / lint / vite build` 全绿。浏览器观感(smoke)与从空需求一句话试点属后续/本地人工；needs_req 已不再在 UI 出现。UI 的下一步衔接(如点向导切页签/一键补齐缺项) 若产品需要可再 增 to P12.2。

