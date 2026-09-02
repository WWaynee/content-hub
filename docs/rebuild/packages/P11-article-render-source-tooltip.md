# P11 · 稿件呈现：结构化可读渲染 + 句内证据 tooltip + 版链提示（「可溯源看得见」的地基）

- RFC 出处：rev-2 §9.1 / §9.2; rev-4 W2/W6(相关); README 顺序=P11（在体验层把溯源变成用户可感知）
- 状态：待开工
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
