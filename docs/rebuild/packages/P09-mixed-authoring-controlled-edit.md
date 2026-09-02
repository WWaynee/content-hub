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
