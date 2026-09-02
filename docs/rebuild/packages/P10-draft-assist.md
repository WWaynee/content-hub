# P10 · 拿来稿起稿：draft_assist（外部稿粘贴并入轨，而不是被当需求搜掉/无据打死）

- RFC 出处：rev-4 §13.5 / W5；README 顺序=P10
- 状态：待开工
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
