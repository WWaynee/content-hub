package service

import (
	"strings"
	"testing"

	"gorm.io/datatypes"

	"github.com/WWaynee/content-hub/agent"
	"github.com/WWaynee/content-hub/storage/model"
)

// P12/W4 硬前置：RequirementCompletenessIssues。
func TestRequirementCompletenessIssues(t *testing.T) {
	cases := []struct {
		name string
		req  *model.Requirement
		// wantAny 为 nil 时要求不返回缺项；否则要包含给定子句。
		wantAny string
	}{
		{"nilReq导致缺项", nil, "需求单"},
		{"缺平台", &model.Requirement{Platforms: datatypes.JSON(`[]`)}, "发布平台"},
		{"有平台却无风格/字数/章节", &model.Requirement{Platforms: datatypes.JSON(`["官网"]`)}, "发文风格"},
		{"平台+风格齐备可生成", &model.Requirement{Platforms: datatypes.JSON(`["官网"]`), StyleTone: "严谨"}, "无"},
		{"平台+字数齐备可生成", &model.Requirement{Platforms: datatypes.JSON(`["官网"]`), WordCount: 500}, "无"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RequirementCompletenessIssues(tc.req)
			if tc.wantAny == "无" {
				if len(got) != 0 {
					t.Fatalf("应无缺项，实得 %v", got)
				}
				return
			}
			found := false
			for _, m := range got {
				if strings.Contains(m, tc.wantAny) {
					found = true
				}
			}
			if !found {
				t.Fatalf("应包含 %q，实得 %v", tc.wantAny, got)
			}
		})
	}
}

// P12/W2 对话结果人话不暴露 tool 名。
func TestHumanizeAction_NoToolLeak(t *testing.T) {
	ok := humanizeAction(agent.DialogueAction{Tool: "update_requirement_field", Field: "style_tone", FieldValue: "xx"}, ActionResult{Success: true})
	if !strings.Contains(ok, "需求单") || !strings.Contains(ok, "发文基调") {
		t.Fatalf("成功句应为可读中文，实得: %s", ok)
	}
	if strings.Contains(ok, "update_requirement_field") || strings.Contains(ok, "style_tone") {
		t.Fatalf("不应泄漏工具/字段名: %s", ok)
	}

	bad := humanizeAction(agent.DialogueAction{Tool: "update_requirement_field"}, ActionResult{Success: false})
	if !strings.Contains(bad, "未能") {
		t.Fatalf("失败句应委婉提示, 实得: %s", bad)
	}
	if strings.Contains(bad, "update_requirement_field") {
		t.Fatalf("失败句不应泄漏工具名: %s", bad)
	}
}
