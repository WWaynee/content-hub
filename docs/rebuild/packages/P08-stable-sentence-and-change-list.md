# P08 · 稳定句身份 + 序列表单（增・删・改・前插・移动）

- RFC 出处：rev-4 §13.3 / W3；README 顺序=P08（在 P09 手编之前做）
- 状态：待开工
- 前置：P02(乐观锁)、P03(真结构)
- 目标：把"用户要动的那一句"从脆弱的「第 i 句（整数全局顺序）」改成**稳定 `sentence_id` 锚 + 明确的序列 diff(insert/delete/move/edit)**——否则一旦加入"删句中两句话/挪到别处/前插"，所有按 index 的引用与继承就错位；它是 rev-3"允许人直编"的真正地基。

---

## 1. 问题
现在 `article_sentences.sentence_id` 是全局序数列；revise/append 的 target 是整数；(内部旧绑定继承仍按 `ArticleSentenceID`)但没有 `delete/move/insert`。系统只支持"线性整篇/改第i句/末尾追加"。若按 rev-3 让人在稿子里自由地删、移、插一句，后端没有对应模型/动作能承接；且"未动句继承"的边界会乱(W3)。

## 2. 模型与契约设计
- **稳定身份**：不把 `sentence_index` 当稳定身份。改用 `article_versions` 下真正稳定的 `article_sentence_id`(PK) 作为**跨 tooltip/change 时的引用锚**；给顺序时另设 `ord`(或直接由 (section,paragraph,sentence) 排序 + 允许改序)。
- **序列 op**（change_list）：
  - `edit { target: sentence_id, new_text }`
  - `insert { at: sentence_id | (section,paragraph,pos), text[], inherit_source? }`（新的最好仍要证据/标注）
  - `delete { target: sentence_id }`（删除该句与绑定；保留审计，不物理销毁）
  - `move { target, new_pos }`（调序）
  - 可选 `replace_paragraph { section,paragraph,text }`（整段替换时最外一层，下面句子视统一处理）
- 提交载体 `change_list` 与 `base_article_version` 一起发给后端（P02 的乐观锁正好用上 base），一次形成新 `article_version`。

## 3. 可执行步骤
1. 给 `article_sentences` 的排序字段"ord"/归一：P03 已让结构真落库；此处保证 `跨版本稳定性`(sentence 就是该版本 snapshot 的一行,id 稳定)。
2. **实现执行器**：新增 `sequence.go`(api/service) `ApplyChangedSeq(ctx, workspace, baseVersion, changeList)`：
   - 先照 P02 CAS base；再在当前 sentence set 上按 op 应用,保持 (section,paragraph,sentence) 序一致；
   - 对 `edit`：仅 target 句更新文本,且它原先 binding 若后续缺失源 → 标记需治理(P09 的黄点/no_source);
   - `insert/delete/move`：重排其余句的排序字段;已存在句的 evidence_binding 继承(键到新旧 sentence_id)。
3. handle 层新增 route 把 change_list 接进来(如 `PATCH /workspaces/:id/article/sequence`,body 含 base_article_version + ops)。
4. **不变式校验**：执行后 assert——`delete` 句其 binding 到新版本不再引用被删句;insert 句若不带显式 source 落入 `no_source` 待核;move 不重写文本。
5. 把现有 `target_index` 调用(revise 等)改走 stable 锚,不再按全局 index。

## 4. 兼容
- 已存 version：既有 `sentence_id(自增PK)` 天然稳,但旧版本是"线性全平"，结构 index 升到 0 处(P03 迁移)已给;对目标句定位建议转成"provide原 PK 或 (sec,para,sent)"两选渐进。需在接口层决定:高层 version 建议只开放 `article_sentence_id` 稳定锚;低层为 UI 顺手保留位置表示并有「重解析为锚」工具。

## 5. 验收
- 单元:给定一集合,apply change_list(edit+insert+delete+move) 后顺序与漏句正确;绑定继承关系正确(删句子绑定点移除、未动保持)。
- 冲突:一个 change_list 基于过期 base 提交 → CAS 拒且不产出新 version。
- 对 rev-3 无偏袒印证:insert 无源句返回 no_source 待核而非静默放 A(Gate to P09)。

## 6. done gate
“P08 done” = 稳定锚 + change_list 增删改移可落一个新 version;CAS/继承/校验都有单测;UI 层(Workspace)还能稳定打开不是 index 引用旧稿。
