# P07 · Verify「先规则、LLM 兜底」+ 删冗余首检 + 弃 rev-3 双实现

- RFC 出处：rev-1 §3.1(RuleVerifier)；rev-3 C5(双实现)/C6(重复首检)；README 顺序=P07
- 状态：**DONE**（规则优先已实现并真验收，见文末"完成记录"与"🧭 回顾"）
- 实现方式简述：新增纯工具 `agent/verifier`(`Rule.Decide`)先做确定性判定(原文包含/数值百分比归一/禁统计求和/歧义保 LowConf)，再被 `censor.FactVerifier` 采纳——实现"先规则、规则判不了才 LLM 低置信近义"，杜绝 LLM 自报 supported；`checkRevisionFactSupport` 无源/链路出错不再静默放行；`Generate` 以 checker 的 claim 检索为唯一证据源(去掉重复首检)；`reviser.go`/`revision_apply.go` 标 legacy 并收敛唯一 revision 链到 `api/service`。
- 前置：P05（Guardian 之后接 verify 结果）；P06
- 目标：
  1. 把"是否可写/这句话值不值得"与"数值/百分比是否真在证据里"**分开**：后者用确定性规则校(number 归一/精确/词覆盖/阈值),只对纯语义等价降级 LLM→不让单 LLM 判真(堵 factverifier 自报坑)；
  2. 一键剔掉 `orchestrator.Generate` 开头那次重复首检(以 claim 检索为准)
  3. 结束 `reviser.go/revision_apply.go`(orchestrator 内,未接 handler)与 `api/service/revise.go` 的双实现摇摆——收敛成唯一 revision 链,disable/删旧。

---

## 1. 现状复盘(拿代码)
- `agent/censor/factverifier.go`：让 LLM 在新 JSON 里自报 `supported/evidence_idx`,系统 `applyFactRefs` 直接采信 → 单点 LLM 判真。
- `api/service/revise.go::checkRevisionFactSupport`：builds StatVerifier 再用上面,失败还默认放行("当无断言") → 更弱。
- `agent/orchestrator/reviser.go + revision_apply.go`:已实现但未接 handler;生产走 `api/service/revise.go` `ReviseSentenceFull`/`AppendArticleContent`;两者并存(C5 漂移源)。
- `orchestrator.go::Generate` 先 `retriever.Retrieve` 再 `checker(claim).Cover`,最终用 claim 那套证据覆盖首检 → 首检白跑(C6 / RFC §0.1 C6)浪费一轮。

## 2. 范围与命中代码
| 文件 | 改动 |
|------|------|
| 新 `agent/verifier/`(非 Agent,纯工具) | `rulebased.go`:对断言的类型(bound/…)归一 & 判 supported/not 的大规则,含 `normalize_num`(`15%`/`百分之十五`/bit)/字符子串/词覆盖 / 分数阈值 |
| `agent/censor/factverifier.go` | 改造为组成规则(normalize)优先,无法定才 `CallLLMNearMatch`,把 `low_confidence` 返回,绝不绕过规则认为 supported |
| `api/service/revise.go::checkRevisionFactSupport` | 改走 RuleVerifier;且失败不允许"当无断言放行",而是返回可读待复核并让调用方(Coordinator/P09)决定丢弃/降级 |
| `agent/orchestrator/orchestrator.go` | 去掉开头 `retriever.Retrieve` 冗余(只保留 claim→cover);把 `persistSequence` 收敛到 claim 那个证据集 |
| `agent/orchestrator/reviser.go`+`revision_apply.go` | 停用并加注释/删除;把唯一 revision 契约放回(已在 `api/service` + P08 的 change_list) |

## 3. 可执行步骤
### 3.1 RuleVerifier
1. 抽断言：允许一次 LLM 把“数据/数字/百分比/日期/条款名”提为 `assertion{kind,text,norm}`(抽取可 LLM)。
2. 规则判定(任一命中→ supported)：
   - 证据 `source_text` 字符包含 / 词集高覆盖该 norm；
   - kind=number/percent/date → `normalize` 后与候选证据点做等价；达成即 supported；
   - 禁止：对多条证据做求和/平均值/统计推断(这类直接判 unsupported,防止 B4 那种"从明细推导晴天天数")。
3. 无规则命中才 `low_confidence`→LLM 近义(允许"年假5天"对"年休假5日"这类纯同义);结果必须带 reason+source(供 P04 P11 显示)。**凡 supported 必须能给出可引证据**,否则不许写 `bound`。
4. 把旧的 `Check`/`applyFactRefs` 收敛为新接口,旧的 bool 不直接的用法移除。

### 3.2 断链（双实现收敛）
- 把 `reviser.go/revision_apply.go` 以分支/注释标 deprecated 且不再编入调用树;若 build 仍可见可加 `//revok·removed`(也可删除文件)。**不要留两套都活着的代码路径**——评审最忌这两种实现漂移。
- 收敛唯一 revision 链到 `api/service` + P08 change_list。

### 3.3 删冗余首检
- `Generate` 里 `retrieve` 那一次直接去掉,只留 claim cover;无 claim 时兜底(当任何证据都没有→ErrNoEvidence),有 checker 已足够。

## 4. 验收标准(可注入 fake LLM / 纯单测)
- RuleVerifier 反例集(curated):统计求和(366天→晴天总数)必 unsupported;近义(年假/年休)经 normal 至少 supported(语义近义只在低置信路径允许);纯公文无断言 pass。
- 确认 `checkRevisionFactSupport` 在新增断言无源时不静默放行(Pack 返回非 nil + 可读)。
- 冗余首检:P 一处 run,断言只跑 claim cover 一次检索(不含首检),日志/调用计数可证明少一次 `SearchKbase...`。

## 5. done gate
“P07 done” = RuleVerifier 判真集 + (not stats)约束单测绿;`reviser/revision_apply` 不再被用;Generate 无首检(计数断言);既有 generation/revise 集成仍绿。— **已达成(见文末)**

---

## ✅ 完成记录（真实验收）
- **已实现**：
  - 新增 `agent/verifier/rulebased.go`(纯工具)：`Rule.Decide` 先判「断言紧凑被证据原文包含」→ supported；含独立数值/百分比者做 canonical 归一 + 仅当某一条证据全覆盖→Cover(带 EvidenceIdx)；>1 条可全覆盖→歧义 LowConf(防各取一半拼证)；含统计词且查不到数→UnsupportedRule;**纯公文/无强数值未包含→LowConf 交 LLM 近义**，绝不被规则盲判 supported。
  - `agent/censor/factverifier.go::Check` 改造：LLM 层只负责把句子的数据/事实断言**抽取**出来;每条断言先用 `verifier.Rule` 定 supported/unsupported，仅 LowConf 才 `nearEqual` 低置信降级给 LLM 近义(命中才 supported 且给 idx+low_confidence)；**凡 supported 必给 Event(EvidenceIdx≥0)，拿不出则不算支撑**。不再采信 LLM 自报 bool。
  - `api/service/revise.go::checkRevisionFactSupport`：链路出错/断言无源不再当"无断言"放行，改返回可读待复核(`ErrRevisionNoSupport`)。
  - `orchestrator.Generate`：有 checker(生产必注入)以 claim 逐点检索为唯一证据源,去掉开头对全需求的重复首检;无 checker 才旧兼容退回 Retriever。
  - `reviser.go`/`revision_apply.go` 顶部标 `DEPRECATED(P07/C5)`；grep 确认无运行期调用，唯一 revision 链在 `api/service` + 后续 P08 change_list。
- **验收(mysql? -NO 纯单测/vet)**：
  - `go test ./agent/verifier/` PASS 5 条：统计求和(366)sars unsupported；近义数值(年休假7 vs 年假7)sars supported；百分比 15 supported；纯公文无强数值不该被规则盲 supported；空断言安全。
  - `go vet ./agent/censor ./agent/orchestrator ./api/service` 零告警；`go build ./...` 通过。
  - `go test ./... -count=1`：全仓无 FAIL(agent/verifier、agent/orchestrator、api/service 均在列)。
- **代码位置**：新增 `agent/verifier/`(rulebased.go+test)；修改 `agent/censor/factverifier.go`、`api/service/revise.go`、`agent/orchestrator/orchestrator.go`、`agent/orchestrator/reviser.go`、`revision_apply.go`(标 deprecated)。

---

## 🧭 回顾（面试/复盘用）：P07 到底改了什么

### 一句话
**把"这句稿子里的数字到底有没有依据"从"让 LLM 自己说有没有"变成"先用确定性的归一规则核对证据原文；规则判不了的纯语义同义，才以 low_confidence 交给 LLM"——凡要标有据，都得拿得出能引证的那一句。**

### 原先在什么场景有问题
| 场景 | 触发 | 后果 |
|------|------|------|
| 真伪由"模型自报"决定 | factverifier 让 LLM 在 JSON 里自己给 `supported/evidence_idx`,系统直接采信 | 校验是"另一个黑盒自己在给结果"，无法反驳、也无法给审单一条看得见的依据 |
| 从明细偷偷求和 | 模型把"各月晴天数"自行合计成"全年晴天 366" | 违反"不编/不做统计推断",还因为它是自报就放过了 |
| revision 断言校验"哑了" | `checkRevisionFactSupport` 在 LLM 校验失败/无断言时默认放行 | 更弱：连这条不设防的第二道也形同虚设 |
| 生成又重复查了一遍 | orchestrator.Generate 先全局 Retrieve 又再逐 claim 检索 | 白跑一轮多花的检索(首检被后一套覆盖丢弃) |
| 两条修订实现并存 | orchestrator.Reviser vs api/service.Revise | 评审最忌"两套都活的代码"，会漂移 |

> 本质：**"校验结论"理应可追溯到一个可执行、可复验的依据，而不应是一句模型的自报。** 数字/百分比在原文里有没有，本来大部分是"能归一比对/能看文本里是否原样出现"的确定性判断题，不该把它都压给不可把握的 LLM 句。

### 宏观方案（精神，非代码）
1. **把"判真"从"问模型的嘴"改成"对一条可执行的规则"**：先从句子抽断言(这一步可让 LLM 帮忙"提出要查的点"，但它不判真)，随后交给确定性规则——原文是否含 / 数值-百分比是否归一等价；规则能判就判，判不了才算"语义近义"才降级一次 LLM 做低置信兜底。
2. **给"支持的结论"上硬约束**：凡 supported 必须落一个可引证据下标(EvidenceIdx)，否则一旦进入 `bound` 就会失去可溯源——因此"拿不出证据"一律不当作有据。
3. **把统计推导当红线**：带合计/据此/推得等、且原文找不到该数的断言 → 规则直接判 unsupported，堵死"拿明细求和"这条路。
4. **砍掉冗余的"重查一遍"**：以逐 claim 检索为唯一证据源,不在其前再对整篇需求多做一次首检。
5. **收敛成一条真的路径**：二选一保留 api/service 的修订链（+后续 P08 change_list），没接生产的 Reviser 显式标 legacy，不再留两套活代码。

一句话精神：**“有没有依据”应先是一道能复验的去重/归一闸门，而不是一翻模型的心；规则判不了的个别近义才给低置信的人工复核机会——从而让“零编造、可溯源”从“prompt 管得住吗”变成“规则拦得住的必然拦,拦不住的显式降级给人审”。**
