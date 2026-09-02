# P07 · Verify「先规则、LLM 兜底」+ 删冗余首检 + 弃 rev-3 双实现

- RFC 出处：rev-1 §3.1(RuleVerifier)；rev-3 C5(双实现)/C6(重复首检)；README 顺序=P07
- 状态：待开工
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
“P07 done” = RuleVerifier 判真集 + (not stats)约束单测绿;`reviser/revision_apply` 不再被用;Generate 无首检(计数断言);既有 generation/revise 集成仍绿。
