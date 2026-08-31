// Package schema 实现对话动作计划（DialoguePlan）的机检与字段白名单硬拦截。
//
// 核心安全目标：LLM 输出的动作计划必须经过本包校验，任何不在白名单内的
// 工具 / 字段都不放行，防止 LLM 幻觉修改核心字段（如 reference_scope、word_count）。
package schema

import (
	"fmt"

	"github.com/WWaynee/content-hub/agent"
)

// 允许的工具（白名单）。
var allowedTools = map[string]bool{
	"update_requirement_field": true,
	"request_retrieval":        true,
	"append_article_content":   true,
	"revise_article_sentence":  true,
}

// 允许对话修改的需求单字段白名单（硬编码，不含核心范围字段）。
var allowedRequirementFields = map[string]bool{
	"style_tone":           true,
	"style_emotion":        true,
	"style_audience":       true,
	"style_purpose":        true,
	"style_taboo":          true,
	"style_subject":        true,
	"chapter_requirement":  true,
}

// Validate 机检整个动作计划。
// 返回错误时，plan 整体不可用（调用方应拒绝执行）。
func Validate(plan *agent.DialoguePlan) error {
	if plan == nil {
		return fmt.Errorf("动作计划为空")
	}
	if len(plan.Actions) == 0 {
		return fmt.Errorf("动作计划不含任何动作")
	}
	for i, a := range plan.Actions {
		if err := validateAction(a); err != nil {
			return fmt.Errorf("action[%d] 校验失败: %w", i, err)
		}
	}
	return nil
}

func validateAction(a agent.DialogueAction) error {
	if !allowedTools[a.Tool] {
		return fmt.Errorf("工具 %q 不在白名单内", a.Tool)
	}
	switch a.Tool {
	case "update_requirement_field":
		if !allowedRequirementFields[a.Field] {
			return fmt.Errorf("字段 %q 不在可对话修改白名单内（禁止修改核心范围/约束字段）", a.Field)
		}
		if a.FieldValue == "" {
			return fmt.Errorf("update_requirement_field 的 field_value 不能为空")
		}
	case "request_retrieval":
		if a.RetrievalQuery == "" {
			return fmt.Errorf("request_retrieval 的 retrieval_query 不能为空")
		}
	case "append_article_content":
		if a.Instruction == "" {
			return fmt.Errorf("append_article_content 的 instruction 不能为空")
		}
	case "revise_article_sentence":
		if a.TargetSentenceIndex < 0 {
			return fmt.Errorf("revise_article_sentence 的 target_sentence_index 非法")
		}
		if a.Instruction == "" {
			return fmt.Errorf("revise_article_sentence 的 instruction 不能为空")
		}
	}
	return nil
}

// AllowedRequirementFields 返回白名单字段集合（供 UI/工具组装侧复用）。
func AllowedRequirementFields() []string {
	out := make([]string, 0, len(allowedRequirementFields))
	for k := range allowedRequirementFields {
		out = append(out, k)
	}
	return out
}
