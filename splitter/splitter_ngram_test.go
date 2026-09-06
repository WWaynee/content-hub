package splitter

import "testing"

func TestCharNGramTokens(t *testing.T) {
	// 中文 + 数字连续段 → 2-gram
	tokens := CharNGramTokens("报名截止到6月30日", 2)
	want := map[string]int{
		"报名": 1, "名截": 1, "截止": 1, "止到": 1, "到6": 1,
		"6月": 1, "月3": 1, "30": 1, "0日": 1, // 共 10 rune，滑窗出 9 个 2-gram，无落单
	}
	for k, v := range want {
		if tokens[k] != v {
			t.Errorf("term %q 期望 %d 实际 %d", k, v, tokens[k])
		}
	}
	// 标点应作为边界，不允许跨标点成词
	tokens2 := CharNGramTokens("录取，规则。报名；条件", 2)
	if _, ok := tokens2["则报"]; ok {
		t.Error("标点边界不应产生跨标点 n-gram")
	}
	if tokens2["录取"] != 1 || tokens2["规则"] != 1 || tokens2["报名"] != 1 || tokens2["条件"] != 1 {
		t.Errorf("应正确切出各词，实际 %v", tokens2)
	}
}

func TestCharNGramTokensEnglishDigits(t *testing.T) {
	tokens := CharNGramTokens("文件编号 GB2025-001 号", 2)
	// 字母数字段 GB2025-001：- 是边界；数字段与字母段分开
	if tokens["GB"] == 0 || tokens["B2"] == 0 || tokens["20"] == 0 {
		t.Errorf("应能覆盖字母数字串内 2-gram，实际 %v", tokens)
	}
	if _, ok := tokens["5-"]; ok {
		t.Error("连字符应作为边界，不产生跨符号 n-gram")
	}
}

func TestCharNGramTokensParams(t *testing.T) {
	if n := CharNGramTokens("abc", 0); len(n) == 0 {
		t.Error("n<=0 时应回退 2-gram 仍能产出 token")
	}
	empty := CharNGramTokens("！！！。。。", 2)
	if len(empty) != 0 {
		t.Errorf("纯标点不应产出 token，实际 %v", empty)
	}
}
