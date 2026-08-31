package schema

import (
	"strings"
	"testing"

	"github.com/WWaynee/content-hub/agent"
)

// 机检单元测试（纯逻辑，不依赖 LLM）：验证白名单硬拦截。

func TestValidate_EmptyPlan(t *testing.T) {
	if err := Validate(nil); err == nil {
		t.Fatal("nil plan 应报错")
	}
	if err := Validate(&agent.DialoguePlan{}); err == nil {
		t.Fatal("空 actions 应报错")
	}
}

func TestValidate_UnknownTool(t *testing.T) {
	p := &agent.DialoguePlan{Actions: []agent.DialogueAction{{Tool: "delete_database"}}}
	if err := Validate(p); err == nil {
		t.Fatal("非白名单工具应被拒绝")
	}
}

func TestValidate_ForbiddenField(t *testing.T) {
	// 核心范围字段不能被对话修改
	for _, field := range []string{"reference_scope", "word_count", "platforms"} {
		p := &agent.DialoguePlan{Actions: []agent.DialogueAction{{
			Tool: "update_requirement_field", Field: field, FieldValue: "x",
		}}}
		if err := Validate(p); err == nil {
			t.Errorf("字段 %q 应被白名单拒绝", field)
		}
	}
}

func TestValidate_AllowedField(t *testing.T) {
	p := &agent.DialoguePlan{Actions: []agent.DialogueAction{{
		Tool: "update_requirement_field", Field: "style_tone", FieldValue: "正式",
	}}}
	if err := Validate(p); err != nil {
		t.Errorf("合法字段应通过，实际=%v", err)
	}
}

func TestValidate_RequiredFields(t *testing.T) {
	cases := []agent.DialogueAction{
		{Tool: "update_requirement_field", Field: "style_tone"},             // 缺 field_value
		{Tool: "request_retrieval"},                                        // 缺 retrieval_query
		{Tool: "append_article_content"},                                   // 缺 instruction
		{Tool: "revise_article_sentence", TargetSentenceIndex: 0},          // 缺 instruction
		{Tool: "revise_article_sentence", Instruction: "x", TargetSentenceIndex: -1}, // 非法 index
	}
	for i, a := range cases {
		p := &agent.DialoguePlan{Actions: []agent.DialogueAction{a}}
		if err := Validate(p); err == nil {
			t.Errorf("case[%d] 缺必填字段应被拒绝", i)
		}
	}
}

func TestValidate_ValidCompositePlan(t *testing.T) {
	p := &agent.DialoguePlan{Actions: []agent.DialogueAction{
		{Tool: "update_requirement_field", Field: "word_count", FieldValue: "600"},
	}}
	// word_count 在禁止字段里，应被拒
	if err := Validate(p); err == nil {
		t.Fatal("word_count 应被拒")
	}
	// 复合合法计划
	valid := &agent.DialoguePlan{Actions: []agent.DialogueAction{
		{Tool: "update_requirement_field", Field: "style_tone", FieldValue: "正式"},
		{Tool: "request_retrieval", RetrievalQuery: "昨天天气"},
		{Tool: "append_article_content", Instruction: "多写天气", Position: "last"},
	}}
	if err := Validate(valid); err != nil {
		t.Errorf("复合合法计划应通过，实际=%v", err)
	}
}

func TestAllowedRequirementFields_ExcludesCore(t *testing.T) {
	fields := AllowedRequirementFields()
	joined := strings.Join(fields, ",")
	if strings.Contains(joined, "reference_scope") || strings.Contains(joined, "word_count") {
		t.Errorf("白名单不应包含核心字段，实际=%v", fields)
	}
}
