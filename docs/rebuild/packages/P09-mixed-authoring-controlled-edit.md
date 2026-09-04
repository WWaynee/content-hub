# P09 · 人机混合写作（受控手编 + no_source 黄点 + 每改动跑轻治理）

- RFC 出处：rev-3 §12.5（12.5.1~12.5.4）；rev-4 依赖 W3/P08；README 顺序=P09
- 状态：待开工
- 前置：P08（稳定锚+change_list）、P04（sources 展示）
- 目标：让人直接在稿子上做**句/段粒度编辑/删/移/插/`整段替换` **并能感知系统立场：改完即使没进 KBase，也会标记 `no_source`(黄点/待人工) 而非默默放行，也不因"没进库"直接否决——兑现 rev-3『真实生产可用、溯源不退化为教条』且仍受 rev-1 RuleVerifier 约束。

---

## 1. 现状
禁止"任意字符编辑"，所有内容只能 AI。但对政企文案，日常是"改一句/套自家用语/把已有稿拼进来"，锁定成"只能 AI"会让日常反直觉(rev2-Q3, rev-4 W3)。同时现有 `Article.full_content` 是只读展示;没有一个"句子可编辑/保存即治理"入口。

## 2. 交互与数据契约(与 RFC 12.5)
- 三种「句态」以文本是否为 dirty 决定：
  - `generated`（AI snapshot）：有 sources→ `bound`；无 → `plausible-ai`(纯 AI 通稿,无外部引用标记)。
  - `user_edited (dirty)`：人保存但尚未治理;黄点提醒+保留未动。
  - `accepted`：跑过治理(轻 run)被 RuleVerifier+Guardian 认可,回到近 `bound`/`plausible-ai`。
- 直接编辑的载体沿用 P08 change_list：单句 edit / 追加 / delete / move / 整段替换(text)。
- 保存 = 提交 `run: revision`(change_list+base_version)；管线按 rev-1 跑一轮（P06 Guardian + P07 RuleVerifier），不回退也非一锤:产出 accepted / no_source-flag / ask_human。

## 3. 可执行步骤
1. 前端在稿件 view(见 P11 也做排版)给每个句子一个「直接编辑」态:行内文本可编辑,提供 保留/保存/丢弃。
2. 保存整批用户编辑 = `POST /workspaces/:wid/runs {run_type:'revision', change_list:[...], base_article_version}`。
3. 后端在 `run:revision` 里对每条 `edit/insert/整段替换` 抽断言→ RuleVerifier(P07):
   - 有明确断言且能对到证据 → accepted(bound);
   - 有断言但无源 → `no_source` 黄点(不静默拒绝),**把"这句话没资料，要不要 (a)保留但标‘无外部资料’(b)这里改成引资料(须给我补一段/一句) (c)删除",打包成一句;**（ask_human，见 P06/前端单选）
   - 纯措辞改写(无新数据断言) → 仍给 `plausible-ai`(无源，但因没有数据断言不产生黄点硬告警)。
4. `no_source` 落到 DB：让这**句能显示成一个黄点**并允许用户「标记为人工确认/给其加 source」。为避免污染证据绑定"bound"，给它单独的 `evidence_status='no_source'`（model 已预留该值）或接 `evidence_status` 校验。
5. **绝不以系统无路而硬删用户编辑**（这是 rev-3 的意义）：no_source 句仍保留正文,只是 "无外部依据" 被标出来,是否要删/降级交由用户,而不是系统夹断。

## 4. UX 反习惯收敛（W3/W2）
- 前端不要把"No evidence → 禁止"弹给用户，也不要把 tool 名丢给他；而是用 P12 那套"可读结果 + 黄点 + 一句选项"。

## 5. 范围与命中代码
| 文件/点 | 落点 |
|---------|------|
| `web/src/pages/WorkspaceDetail.tsx` | 每句可编辑入口 + 保存调 run;把结果那处换成 P12 人话 |
| `api/service/seq(run:revision)` P08 执行器 | 接 change_list;`no_source` 标记 |
| `web` tooltip / sentence_views | no_source 显示为黄色警示可点开选择(a/b/c) |
- 与 P11 排序：P11 先做文章可读 + tooltip 底座，P09 在其上加"每句可编辑"。若并行，先完成 P11 的基础渲染——否则"可编辑句子"没地方放。

## 6. 验收
- 集成：对一段有人编辑稿（edit 一句 + insert 无源一句），`run:revision` 后：edit 句若断言有源→ bound；insert 无源句→ 黄点 no_status 但**正文保留**、前端能选 (a 保留 标无源) / (c 删除) 且结果落 article_version。
- 不回归:纯措辞句不弹硬框;未授权句文本不被静默改(RFC 不变式)。

## 7. done gate
“P09 done” = 用户在稿件可对句/段做 edit+insert(+delete/move),保存走 run 得到 accepted/no_source 的语句态;no_source 保留+能手动赋予；界面有“为什么这条无来源 + 三个选项”而非 tool 名/硬拒。

---

## ✅ 完成记录（真实验收）

> P09 本轮交付把"人能在稿面上直接动手、且不把无据内容闷声放行"变成端到端可用：**前端句级受控编辑(改/句后插/删/作者给句子的"认可保留"取舍) → 提交 PATCH sequence(change_list) → 后端对新增/去据却无引用的句落 evidence_status='no_source' 占位 → 读取把 no_source/human_kept 升级成独立 claim_type,前端据此显示黄点 + 人话三选项,永不是透传 tool 名。** 界面/后端对"没进知识库"既不硬拒,也不假装有据。
- **后端**
  - `sequence.go`：`seqSentence.unsourced`；edit(明确 ClearEvidence 且无替换)/insert(拿不到 evidence) 的新句在 plan 里被标记 → 落库时为该句建 `evidence_status='no_source'` 的占位 binding(doc=0,不和真 source 混淆)。
  - `sources.go`：`ClaimTypeNoSource`/`ClaimTypeHumanKept` + `ClaimStatusBySent(binds)`；`BuildSentenceViews` 增加按句占位解析 → claim = no_source/human_kept/bound/plausible-ai 四态。读侧不再把"疑似该有据却无"混进 plausible。
  - 新增 `api/service/manual_keep.go`：`MarkSentenceManual`（作者对无源句做 ack_human 认可保留(解除黄点) / reset_no_source 退回待核；bound 句不可被降级）。
  - 路由/接口：`PATCH /workspaces/:id/article/mark {sentence_id, action}`。
- **前端（web/src/pages）**
  - 新增 `ArticleSentencesBoard.tsx`：每句列表、claim Tag + tooltip 说明；操作=「改这句 / 句后＋插 / 删 / 认可保留(不黄) / 退回待核」；调用 PATCH sequence/mark,操作后父组件重新拉取展示(让 no_source/状态实时可见)。
  - `WorkspaceDetail.tsx`「稿件」页把旧的只读句子 Tag 列表替换成受控编辑区(带 Divider「句子 · 受控编辑 / 可溯源」)。
- **回归/验证(MySQL up)**：`go test ./...` 全仓绿(含 cmd/migrate)；P08 既有集成 `TestSequenceEdit_EndToEndWithCAS` 适配占位语义(真源5002 保留 + insert 无源占位 + ack_human/reset 翻转) **PASS**；纯单测 `no_source_test.go`(claim 优先级 / human_kept / insert 标 unsourced) **PASS**；前端 `npx tsc -b` 通过、`npm run lint` 0 error。
- **诚实边界（已在 doc 与本复核记录）**：本轮把"治理"做成**确定性语义层 + 作者拍板**,而非对每条新句跑一轮需活的 LLM+RuleVerifier/Guardian：即"新增/去据且无外部可引 → 落 no_source 待作者取舍",这在无网/离线/单测可跑时成立,也兑现"不静默、不硬删"。真正的"断言级 AI 治理(P06/P07 链路参与每条手编句)"与 P10(draft_assist 直接并入/给 user_draft 来源)留在后续,不回退本章不含其语义。前端本轮未做 UI 层的"移动 move / 整段替换(富文本)"按钮——后端 sequence 已支持 move,组件下一步补。

---

## 🧭 回顾（面试/复盘用）：P09 到底改了什么

### 一句话
**把"作者可以直接在稿面改它的句子、删掉/插进新的一句、并替某句表态"变成能落到稿子的真实交互,同时守住底线:系统看得出"这句没有外部依据"并如实标黄提醒(no_source),既不放行成伪有据,也不能因为它来源缺就替用户删掉它。**

### 原先在什么场景有问题
| 场景 | 触发 | 后果 |
|------|------|------|
| 只能让 AI 写、AI 管写 | 全库只有"生成/追加/改第 i 句"且都当"AI 产物" | 政企日常"改一句自家话/拼老稿/删冗余"是最高频动作，却被锁住只能交给 AI，反直觉、无法落地 |
| 没给"无据但体面"的出口 | 人硬改/新贴内容时若无 KBase 依据 | 系统只能二选一：要么硬拒(哪怕只是自家措辞)、要么当没看见把无据内容闷进"有据"——都可溯源产品的污点 |
| 无法把'该核却没据'标出来 | claim 只有 bound/plausible-ai，无据一律当通稿 | 审单人分不清"这段是 AI 顺势带过，还是其实是它该有据却给不出(有风险)" |
| UI 只会通报具失败 | sendChat/保存把手编失败 tool+底层消息怼给用户 | 普通用户看到 `sequence:.../ no_source` 不知所措 |

> 本质一句：**"让作者能自由修改一篇稿"不只是加个输入框——它需要"对一句新增/无据内容,系统和作者对一下立场"的通道;系统既不能假装它有据,也不能因它暂时没依据就夺走编辑权。** P09 在 P08(稳定句+change_list)之上把"无外部依据但需作者表态"做成一边是 UI 的受控操作、一边是落库的 `no_source/human_kept` 显式状态的闭环。

### 宏观方案（精神，非代码）
1. **给"句"一颗"声明"而不仅是"内容"**：一句稿不只有文本和它有没有 "sources",还要带"状态"(bound / no_source-待核 / human_kept-作者认可 / plausible-ai)。这样展示时能把"该核却无据"的句子挑出来给黄点。
2. **把"编辑一整批"承载成一次可审的序列变化**：改一句、后插一句、删一句都变成一个原子 change 交给后端;后端统一跑 CAS 与不变式,不担心并发跟错、也不担心"我删了别处把没动句子悄悄改了"。
3. **来源缺失 = 一张待做的黄色表,而不是一道不可完成的禁令**：给不出外部依据的新内容照常被正文保留,但落一个 `no_source` 的显式占位;作者届时可选「认可保留(不再黄,承认自有内容)」/「删除」/「去补资料(通向正文来源管线)」,系统绝不替他夹断。
4. **UI 只给人能懂的话**：黄点、tooltip、三个明文的动作;执行后反馈是"一句人话结果",不是把内部状态/失败原样怼给普通办公用户。
5. **边界诚实**：真正的"它有没有数据断言、要不要逐断言去 LLM/Rule 治理 + Guardian 轮询补料 (P06/P07)、以及外部草稿直接并入(P10)"是和本条相辅相成的进阶——P09 先把"人在稿上改 + 系统如实站立场"的那一地基闭合。

---

## ✅ P09 · 治理补足（把"手编句"接进 P07 真校验链）

> 前一轮把治理做成"确定性占位＋作者拍板"；为回应"既然有 RuleVerifier/Guardian，为何手编不做真校验"，现补一条轻链：
> **保存一句新/改写文本前，先跑一次真校验 → bound / no_source / plausible 三态再组 op 落库。**
- 后端新 `api/service/manual_govern.go::GovernManualSentence`：取该工作区引用范围 → `SearchKbaseSentences` 拉候选 → `agentcensor.FactVerifier.Check`（LLM 只做"拆断言"，supports 交给 P07 Rule/近义判，不采自报布尔）→ 归三态：
  - 有断言且给得出可引 `supported`（带 EvidenceIdx）→ **bound**，并把 sources 传回 op.Evidence；
  - 有断言但规则＋近义都拿不出可引 → **no_source**（黄点,作者三选，正文保留）；
  - 无数据/事实断言（纯措辞/衔接）→ **plausible**（不给证据也不标黄,`AvoidNoSource`）。
  - 链路连不上(离线/服务未配)→ 保守 fallback：按 no_source 保留并给一句人话；不会把无据内容偷偷当有据、也不在链路失败上夹断用户。
  - 边界如实说明：每句新文本用一轮"检索+Full Check"，不额外再跑 Guardian 的多轮换 query/ask_human 编排——那更适合同句有多个值得单独追问的原子断言(P06 claim)；本条做到"与新文本结对的可引证据 + 是否属纯措辞"的真判定,且可回放在后续嵌入 run step,不走死循环/不弹底层工具名。
- 前端 `ArticleSentencesBoard`：改一句/后插一句提交前先 `POST /article/govern` 拿 claim；按 bound(带 evidence) / plausible(avoid_no_source=纯措辞不黄) / no_source(照旧黄) 组织 op → 再 PATCH sequence。
- model/sequence：`ChangeOp.AvoidNoSource`；plan 对"无证据但 AvoidNoSource"的新句不落 no_source（plausible）。纯单测 `TestChangePlan_AvoidNoSourceKeepsPlausible` 通过；`go vet`/`go build ./...`/`web tsc` 0 错。

---

## ✅ P09 · 打磨二：治理统一进 run + 移动落库完成

> 从一个"真在稿面办公"的用户视角再收敛两点体验：**保存不重复等待与移动可落在稿上**。
- **治理统一到序列保存内（而非每次先在点保存前单独查）**：`ChangeListRequest.Govern`(前端 PATCH 带 `govern:true`)。
  `applySequenceVersion` 在 change 落快照前对"带不出可引证据的新句"(即 plan 中标成 no_source 候选)统一跑服务端真治理
  (manual_govern.go::`governSeqSents` 就地改写 plan)：bound→补源去黄、plausible→清 unsourced 不黄、no_source→保持黄点给作者三选。
  好处：用户点一次保存 = 一次性服务端治理+结构落库；治理默认关(`Govern=false`)时纯离线也能落、单测/CI 稳，连不上外部走高底线兜底不夹断。
  前端 `saveEdit/insertAfter` 不再单独 POST `/article/govern`（该端点仍保留作可选 API 方便离线预判），直接 `PATCH sequence + govern:true`。
- **句子可安全"挪"到任意另一句之后**：`ArticleSentencesBoard` 增加两步式移动(每个句头上「把它移到别句后」→ 选中源后其它句子头上出现「放到本句之后」)，
  调 `PATCH {op:'move'}`；后端(P08)移动不动文本、绑定随行、段落号就地重排——移动=排版动作不触发治理。
- 句头加 `#序号` 便于在长稿里指认/校对在哪一句；`govern`+`move`/`delete` 复用在同一 run 的 op 批，页面 reload 即见新版本。
- **验证**：go vet/go build(后端)、`go test ./api/service` 全绿、既有 `TestSequenceEdit_EndToEndWithCAS`(真 MySQL)不回退；
  前端 `npx tsc -b` 0 错、`npm run lint` 0 error(仓库原有 8 条 set-state-in-effect warning 未涉新增)。
