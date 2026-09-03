package verifier

import "testing"

// P07 规则检真的离线反例集(curated)。决策由上层 factverifier.Check 调本规则;此处直接测 Rule.Decide。

// 统计求和(从明细推出"合计晴天 366",原文并没有 366) → unsupported,不可当 supported 写 bound。
func TestRule_StatisticalSumUnsupported(t *testing.T) {
	assertion := "据此合计全年晴天共有366天" // 含统计推导词
	ev := []string{
		"1月晴天10天,2月晴天12天,3月晴天8天……", // 明细,不出现 366
		"如遇阴雨顺延。",
	}
	got := Rule{}.Decide(assertion, ev)
	if got.Verdict != UnsupportedRule {
		t.Fatalf("统计求和应在原文找不到 366 判 unsupported,实得 %v(%s)", got.Verdict, got.Reason)
	}
}

// 数值语义等值(近义但数字一致) → 单条证据覆盖 → supported(给唯一证据下标)。
func TestRule_NumberNearSynonymSupported(t *testing.T) {
	got := Rule{}.Decide("年休假7天", []string{"员工依法享有每年年假7日。"})
	if got.Verdict != Cover || got.EvidenceIdx < 0 {
		t.Fatalf("断言含 7、证据也含 7,应 supported,实得 %v(%s)", got.Verdict, got.Reason)
	}
}

// 纯公文无强数值且原文未包含 → 不应被规则判 supported(应 LowConf/unsupported,交由 LLM/人工)。
func TestRule_PureTextGeneralIsNotBlindSupported(t *testing.T) {
	got := Rule{}.Decide("各单位要高度重视抓好落实", []string{"要把这项工作落到实处见到实效。"})
	if got.Verdict == Cover {
		t.Fatalf("无强数值且原文未包含,不应被规则判 supported")
	}
}

// 百分比里明示数字(15%)→ 规则归一命中 → supported。
func TestRule_PercentSupported(t *testing.T) {
	got := Rule{}.Decide("该项目完成率达到15%", []string{"……完成率为15%,……"})
	if got.Verdict != Cover {
		t.Fatalf("15 数值(百分号形式)应在证据中归一命中并 supported,实得 %v(%s)", got.Verdict, got.Reason)
	}
}

// 空断言不 panic、无输入也安全(规则天然返回 LowConf/never Cover for empty)。
func TestRule_EmptySafe(t *testing.T) {
	_ = Rule{}.Decide("", []string{"有些内容"})
	_ = Rule{}.Decide("2026是明年", nil) // 数值 2026,但无证据 → LowConf,不应 panic
}
