# P12 · workspace/对话 UX：人话结果、可生成硬前置、状态机别名、动线收敛

- RFC 出处：rev-4 §13.2/W2、§13.4/W4；rev-2 §9.4(动线)；README 顺序=P12
- 状态：待开工
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
