# P11 · 稿件呈现：结构化可读渲染 + 句内证据 tooltip + 版链提示（「可溯源看得见」的地基）

- RFC 出处：rev-2 §9.1 / §9.2; rev-4 W2/W6(相关); README 顺序=P11（在体验层把溯源变成用户可感知）
- 状态：已完成（前端落地+tsc/lint/build 全过，落地与回顾见文末「P11.1」「🧭 回顾」）
- 前置：P03（结构化 response）、P04（sources+has_newer）→ 前端才有真结构与原句可渲染；P09 相关手编不必等,可后叠
- 目标：把当前"整块 `full_content` pre-wrap 文本墙 + 每句一个 '证据 xN' Tag"重做成**"像公文的可读正文 + 句子上悬浮/引用的出处 + 明确 fold 的溯源清单"**——让"可信/可溯源"从藏在一个 tag 里变成用户一看就能感知的词。

---

## 1. 现状痛点（源码佐证）
- `web/src/pages/WorkspaceDetail.tsx` 用整块 `Typography.Paragraph style={{whiteSpace:'pre-wrap'}}` 输出 `article.full_content`；编排标题/段落/首行缩进都糊在一段。
- 每句循环里只有 `Tag color="blue">证据 xN</Tag>`，bindings 是无 source 的 id（P04 后才给原句）。
- 无法让用户知道"这句是从《XX方案》第X章原句来的"——可溯源没被 UI 兑现。

## 2. 交互设计（对齐 RFC §9.2/9.4）
- **结构化渲染**：使用 P03/P04 的 `sentence_views`/`sections→paragraphs→sentences`，由组件渲染成层级标题+段落(缩进/spacing)，而非一个 markdown 长 string。
- **来源在句内呈"悬浮引文"而非旁置 tag**：
  - `bound`句：在标点后给低调小上标（如颜色点/编号）；鼠标 hover/点击弹出 tooltip：展示一条或多条 source（原文引句 source_text、文档名、章节、版本 + `has_newer` 旧版标记 + 「复制引文」）；移开自动收起（用户要求：移入停、移出收）。
  - `plausible-ai / user_draft / no_source`：不同低调标记，区分"无外部引用但 AI 通稿"vs"待人工核对(黄点)"，不能和有据句混。
  - `no_source`(P09/P10)：黄点可 click——给三选（保留标注无源/换原/删除），视觉要与 bound 分离。
- **溯源清晰但 foldable**：一个可折叠的“证据清单/证据密度视图”作为二级（默认收起），导出的仍是审单人能逐条核的清单（来源原句可复制）。

## 3. 可执行步骤（在 Workspace 详情页重做稿件 tab 的渲染）
1. 后端(已 P03/P04)给 `sentence_views`；前端用 antd `Popover`/自定义 tooltip `overlay` 在 hover/focus/click 时展示 source(tooltip 尽量不随鼠标长文本转着跑，做成一卡片)。
2. 句子组件：`BoundSentence`/`PlausibleSentence`/`DirtyNoSourceSentence` 三变体,视觉与交互分离。
3. 提供“只看有据句/倒大导出预览/一次性复制原句引文”的页级入口（证据密度/源导，折叠）。
4. 若篇幅长,前端默认按结构折叠(章节 collapse)给更舒适阅读;保留“铺开全文”选项。

## 4. 依赖 P04 的装配
`has_newer` 与版本/原文来源都来自 P04 拿到的 sources;若 P04 未完成,P11 tooltip 会空壳——所以顺序上 P11 必须在 P04 之后(README 依赖已列)。

## 5. 验收
- 视觉:一个 `bound` 句能 hover 看到“来源原句 / 文档名 / 章节 / 版本”。
- 若 source X 的版本不是当前 doc 最新 → 出现"已有新版"标记(W6 体验兑现)。
- `no_source`/dirty 句不等于 bound 的蓝色显示(黄点可点)。
- 导出视图仍含可复制原句的应证说明(销售在 P04 的导出也带了原句;此处仅是"原文也可点开一条条看")。

## 6. done gate
“P11 done” = 稿件按结构可读渲染;bound 源 hover 可见原句/版本;has_newer/no_source 与 bound 视觉分离;默认 read-only 模式(交互不冲突 P09 的编辑模式开启,平台自动切换)。

---

## P11.1 落地与验收记录（前端完成本包）

**交付形态（经确认取舍）**
- 稿件「页签」沿用既有 workspace 工作区，新增“可读正文视图”，且与现有“逐句编辑/取舍面板”(ArticleSentencesBoard, P08/P09) **并存**，用 Segmented 双向切换；
- 默认 `read`（可读正文 read-only），点「逐句编辑 / 取舍」才进 Board——满足“默认 read-only、编辑不打断”。

**新增/复用的前端代码**
- `web/src/types.ts`：Article 补 `sections?` 结构化字段；新增 `ArticleSection/ArticleParagraph/ArticleSentenceRef` 类型；复用既有 `SentenceView`/`EvidenceSource`（P04 原句+版本+has_newer）。
- 新增 `ArticleSourceTooltip.tsx`：`SourcesCard`（P04 sources 卡：原文/文档/章节/版本/has_newer/已删快照/复制引文/多条时的“复制全部引文”）、`BoundSourceMarker`(低调 ⓘ+Popover overlay 悬浮源卡)、`NoSourceMark`(no_source 橙“待核”可点 / human_kept 绿“人工保留”)。纯展示无网络。
- 新增 `ArticleReadableView.tsx`：按 `sections`(段级分组，不造强标题树，align 选择的 doc 决策)铺排正文；每句用 `sentence_id↔SentenceView` 桥绑；`bound→ⓘ hover source 卡`、`no_source(含 user_draft) → 橙待核，点击回调切去逐句编辑处理`、`human_kept→绿标`、`plausible→不标`；页级：`只看证(淡化无源句)` Switch + `证据密度 · 出处(N)` Collapse(默认收起，可复制引文)；无 sections 的旧线性稿退化到 sentence_views/sentences 兜底。
- `WorkspaceDetail.tsx`：稿件页签改为 `Segmented(可读正文 / 逐句编辑)`；ArticleReadableView 的“去编辑处理”把视图切到 Board。

**验收（当前能力可确定性执行）**
- `cd web && npx tsc -b`：**0 error**；
- `npm run lint`：0 error（8 个均为既有 Knowledge.tsx 等 set-state-in-effect 告警，本包组件无告警）；
- `npx vite build`：产物成功构建。
- 后端零改动（P11 只动前端）；所需数据均由已落地 P03/P04 的 `GET /workspaces/:id/article` 提供，字段名已用 Go handler 返回结构核对一致（`sections[].paragraphs[].sentences[].id/content`、`sentence_views[].sentence_id/claim_type/sources[].has_newer…`）。

**未做/局限（如实）**
- 真实浏览器视觉 smoke 需浏览器/playwright 环境，本会话工具未含(无浏览器工具可用)；以 tsc/lint/build + 前后端字段契约核对替代。
- heading 未以结构化字段落库(P03/P08 存 sec/para/sent、不算标题文本)，故按选定的“段级分组”，不强造“章节标题”折叠树；如需可折叠真实公文大标题，需后续后端把 markdown `#/##` 标题行结构化落骨架。

---

## 🧭 回顾（面试/复盘用）

### 一句话
**把“一句可信/可溯源”这件事从“藏在一个绑定的证据 tag 里”变成用户能一眼看到、一句能 hover 出出处原文/文档/版本的“公文可读正文+句内引文”体验——溯源不再只是后端字段，而是用户可见的体验。**

### 原本的问题（触发场景）
| 场景 | 旧实现 | 落地后 |
|------|--------|--------|
| “这句凭什么这么说” | `WorkspaceDetail` 只有整块 `full_content` pre-wrap 文本墙 + 每句一个“证据 xN”Tag（无原句） | 可读正文一句句渲染，bound 句句尾 ⓘ，hover 悬浮源卡（原文/文件/章节/版本/是否已有新版/一键复制引文） |
| 有据/待核视觉不分 | bound/no_source(用户草稿断言)/human_kept/纯通稿都用一个蓝色 Tag | 蓝ⓘ有据、橙“待核”可点（no_source 与 user_draft 语义统一）、绿“人工保留”、灰/无标纯通稿 |
| 审单人想批量看证据 | source 藏绑定 id、看不出顺序/全部 | “证据密度 · 出处(N)”折叠面板，逐句可展开/复制引文 |
| 老线性稿(P01 之前生成、无结构) | 结构缺失无法渲染 | sections 缺省时按 sentence_views/先进 sentences 线性兜底可读 |

### 宏观方案（精神，非代码）
1. **写稿核心回归可读正文**：渲染主体从“句卡片墙”换回“公文排版的正文”(可读视图),读起来像成稿。“编辑诉求”单放一个独立视图/切入口，互不打扰符合政企文秘的使用习惯。
2. **溯源从字段到体验**：所有 source 都走 P04 已装配好的“可读原句 + 文档/章节/版本 + has_newer + 已删快照”，用句内 ∎/低标 + hover 卡片给出，用户能看懂“这句出自哪、是不是最新、怎么复现引用”。
3. **声明态彻底独立于信息外观**：bound/no_source/human_kept/plausible 四种做到视觉与语义不可混——有据的才当“可溯”，待核的明确提醒用户决策，人工保留不再反复骚扰，通稿句不装成有据。
4. **承接便编辑不乱序**：改字/增删/认可删除仍然只发生在受控的 sentence 面板(复用 P08/09 run 语义,乐观锁+句子稳定 ID)，可读视图只管看得清楚，两者把“看得见”与“敢改”分好。

### 一句话收束
**“可信”要能被人看到、被审单人逐条核，才算真的可信**——把可溯源从“一个 tag”升级为“正文字句 + 飞出出处原文/文档/版本/新旧的交互”，是让 multi-agent 生成结果在政企稿面上“讲得清、指得到、核得动”的直接证据。

---

## ▣ 任务收口（本包交付口径 / 交付出厂）

**一句话状态**：P11（稿件结构化可读渲染 + 句内证据 tooltip/版链提示 + 黄点区分）**已全部完成并通过能确定性执行的验收**：`web` `tsc -b` 0 error、`npm run lint` 0 error（仅既有告警），`npx vite build` 成功；后端零改动，所需数据完全来自已落地 P03(结构化 `sections`)/P04(人读 `sentence_views`+`has_newer`) 的 `GET /workspaces/:id/article`。稿件页签默认进入 `read`（可读正文），逐句编辑/取舍仍复用既有 Board(P08/P09 sequence/run 语义)。

**复盘要点（简历/评审一句话）**
1. 溯源从“后端字段/tag”提到“用户可见的词”：bound 句句尾 ⓘ + hover 源卡暴露原文/文档/章节/**版本与是否为最新**(has_newer)/已删快照/一键复制引文；把“可信”变成可读可核的体验而非藏着。
2. 声明态四态在 UI 不可混用：蓝ⓘ有据 / 橙“待核”可点(no_source 与 user_draft 统一语义) / 绿“人工保留” / 通稿无标；阅读态(read)与编辑态(edit Board)分离，编辑不打断可读。
3. 兼容旧稿 & 审读有力：sections 缺失的旧线性稿兜底可读；“证据密度 · 出处(N)”折叠面板供审单人逐句展开/复制引文。

**遗留 / 与下游衔接（本包不做）**
- heading 未以结构化字段落库存根，故章节折叠按“段级分组”而非“标题树”实现（如需真实大标题可折叠，需后续后端把 `#/##` 标题行结构化落骨架后再叠 UI）。
- 真实浏览器视觉 hover 冒烟需浏览器/playwright 运行（本会话无浏览器工具）——已用 tsc/lint/build + 前后端字段契约核对兜底；建议上简历 Demo 前 `npm run dev` 打开一篇已生成稿目视一遍 tooltip 观感。
- 更完整的“人话结果 / workspace 状态 / 动线（P12）”是把本条 UX 与 workflow 状态语义串起来的下一步包。


