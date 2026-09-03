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

---

## ✅ 完成记录（真实验收）

> 时序/范围：P01~P07 已并入主干；本包在 P02(乐观锁) 与 P03(三真结构)之上落地。与评审对齐后的三个取舍：
> **纯序列执行器(不内嵌 LLM/检索)、本次仅交付后端 API+执行器+单测+run 化（不抢 P09 的前端句内编辑 UI）、稳定身份以 `article_sentence_id`(PK) 为锚 + 用既有三元(sec,para,sent)就地重排管理顺序（不新增独立全局序号列）。**

- **已实现**：
  - `model.AgentRun` 新增 `RunSequence`,新增 `api/service/sequence.go`:
    - 请求契约 `ChangeListRequest{ ops:[ChangeOp{op: edit|insert|delete|move, target_sentence_id?, anchor_sentence_id?, new_text?, evidence?, clear_evidence?}], base_article_version? }`
    - `applyChangePlan`(**纯函数**，无 DB 无 LLM)：在"有序句集 + 每句绑定"上按 op 依次应用并返回新有序句草稿。不变式即前文 §3/闭包：
      - edit 只改目标句文本、段落归属不动；默认保留原绑定（措辞改动不卸来源），`clear_evidence=true` 或带 `evidence` 才重写该句证据；
      - delete 摘句且其绑定随之消失（不留悬空引用给读取）；
      - insert 新句进 anchor 段、无证据载荷 → 生成 `reviews`(人话级 no_source 待核),正文不回退(new 句仍保留);
      - move 文本+绑定随行、跨段时归属改挂 anchor 段(move≠重写)；
      - 结尾 `reflowSents` 段内句号重排,保证 (sec,para,sent) 三元无歧义升序。
    - `applySequenceVersion`(DB 侧)：以 CAS 把 `current_version_no: base→base+1` 拿真写权，再在一个事务里落 `article_version(新) + article_sentences + evidence_bindings`（bindings 回填新句子 PK）；base 不匹配即 `ErrSequenceConflict`,不产新版本。
  - `api/service/runlink.go::RunSequenceEdit`：把一次受控序列编辑固化为 `run_type=sequence` 的持久 run（active 排他 + 逐步 append + Finish/Fail），新句无来源时还会追加一条 `await_human / no_source_flag` step（P09 治理再续）。reviews 随返回。
  - `api/handler/article.go::HandleArticleSequence` + `api/router.go` 路由 `PATCH /workspaces/:workspace_id/article/sequence`,body = change_list(`base_article_version` 可省略,省略则以当前版本为基准做 CAS)。返回新 `article_version_id` + 人话 `human_text` + `reviews`。
- **验收（本会话可确定性执行部分）**：
  - `go test ./api/service/ -run TestChangePlan` **PASS ×8**：edit 保段落/默认保绑定；binding 策略(clear/override)；insert 无源 → 落段归 anchor+无绑定+no_source review；insert with evidence 绑定新句；delete 关集成"删句消失+未删绑定保留+段内句号重排"；move 跨段文本随行+绑定保留+src 顺序；段内 reflow；未知 op/move 到自己/?anchor 不存在报错。
  - `go vet ./api/... ./storage/...`、`go vet -tags=integration ./api/service/` 零告警；`go build ./...` 通过。
  - `go test ./... -count=1`：除 `cmd/migrate`(其 `migrate_test.go` 直接 Fatal 强连 MySQL，本会话 MySQL daemon 关闭导致连接 refused，属**既有环境性硬依赖、与本包代码无关**，起 MySQL 后同初基线即绿)外，其它包全绿。
- **为校验真实落库/CAS 冲突** 新增 `api/service/sequence_integration_test.go`（`//go:build integration`,真 MySQL）——造 v1 → 提交 change_list(删 A/改 B/在 B 后插无源句) → 断言 v2、顺序、原绑定保留、insert 无源提醒,再用过期 base 提交断言被 CAS 拒绝且版本不推进。⚠️ 本会话 Docker daemon 未运行、MySQL 不可达,该集成路径已通过 `-tags=integration` 编译/vet 校验但**未在本会话实际执行**,应在有 MySQL 的环境上跑一遍(下面给了命令)。
- **代码位置**：新增 `api/service/sequence.go`、`api/service/sequence_test.go`、`api/service/sequence_integration_test.go`；修改 `storage/model/agentrun.go`(RunSequence)、`api/service/runlink.go`、`api/handler/article.go`、`api/router.go`。
- **不改变既有路径**：revision/append/regenerate 仍走它们原来的 run 与落库；未动句读侧(GetArticle 的 `sentence_views`)不受新列/序号字段影响(本包不加列)。换段/富文本编辑不在本包(P09/P11 表达)。

---

## 🧭 回顾（面试/复盘用）：P08 到底改了什么

### 一句话
**把"稿子里我要动的那一句"从"一个会随删/移/插而集体错位的全局下标"换成"一个稳定不变的句子号(article_sentence_id)",并且让"删一句 / 挪一句 / 在某句前插一句"都变成后端能一次落成新版本、还能自动维护证据跟随的三种一等操作——而不再只能改第 i 句/只往末尾追加。**

### 原先在什么场景有问题
| 场景 | 触发 | 后果 |
|------|------|------|
| 引用靠"第 i 句" | 对话或前端要"改目标句"时传 `target_index`(整篇扁平 list 下标) | 一旦前面删/插了句子，所有后续下标集体错位，会冲到别人句子上去 |
| 只能顺/追加 | 旧实现只有"改第 i 句 + 末尾追加 + 全篇重生成" | "想删掉一句、把某段一句挪到别处、在某句前插一句"根本没有对应模型能承接 → 人其实自由不了 |
| 证据随 index 跟错 | 局部链路未提供 delete/move/insert 时"未动句证据继承"只有按序号拷贝 | 删除/换序后，被删句的证据还可能留到别的句子头上 / 挪走的句证据丢失 |
| 没有"提交一批手改"的载体 | 用户在自己的 Word/笔记里可能同时想删+改+插好几处 | 每次只能一个人小步逼近，做不到"把这批编辑一次性落到快照"可回放可审 |

> 本质:**"句子身份"之前不是身份，而是一个位置值**；“我想改的那一句”这种意图只有用稳定 id 表达，才能在被编译成新增/删除/移动的序列 diff 之后仍锚得住，也能让"未授权的句子内容绝不被乱改"成为一条可证明（closed-set）的不变量。开放"人能在稿子上真的删句/移句/在任意位置插句"的地基就在这一层。

### 宏观方案（精神，非代码）
1. **换锚**：每个版本内句子一律用其主键 `article_sentence_id` 指认，接收方/存储都按这个稳定身份走；版本与版本之间的"谁是谁"靠存在性/内容保持一致，而不是靠巧合的下标对齐。
2. **把"编辑"建模成有序的序列 diff**：一次请求带一串原子 op `{edit|insert|delete|move}＋base 版本号`；执行器按顺序把这些 op 应用成一个"新的有序句集"，一次固化为一个新 `article_version`（快照式，可回放）。
3. **交给三层确定性保证**：① 位置顺序用现有三元结构就地重排（不引入会跟读侧/旧数据打架的新全局序号列）；② 证据绑定"随句走"——没被删就继承、move 也原样带过去、被删句才连同绑定一起消失；③ 每次应用都保持 closed-set——除被显式删/改的句子外，其它句文本与来源绝不变化，供单测一条条断言。
4. **不给"新增句"默认假来源**：若没给它可引用的证据，就如实记录"这句没有外部依据(no_source 待核)"并原样保留正文，交 P09 治理层去决定补料/降级/删除，而不是偷偷编成有据或干脆把人家的编辑抹掉。
5. **与既有安全/审计地层合用**：挂在乐观锁(CAS)上防并发写坏，作为一个 `run:sequence` 持久体进 run 状态机(可审计可回放)；纯逻辑抽成无 DB/无 LLM 的执行函数，把顺序/继承/闭包都变成能一键跑绿的不含杂质的单元测试。

一句话精神：**“人能自由、正确地收拾一篇文章”需要先把“哪一句、加还是减还是换位置”这种最底层意图在数据和接口里立成真的一等公民；位置只是顺序的展示，身份稳定、证据随行、未授权句不变，才是把"直接改稿"做成不退化、可溯源的地基。**
