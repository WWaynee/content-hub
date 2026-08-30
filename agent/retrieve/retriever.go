// Package retrieve 实现知识检索 agent（方案乙：LLM 提炼检索 query → 每个 query 单次检索 → 汇总）。
package retrieve

import (
	"context"
	"fmt"

	"github.com/WWaynee/content-hub/agent"
	"github.com/WWaynee/content-hub/api/service"
	"github.com/WWaynee/content-hub/llmclient"
)

// Retriever 知识检索 agent。
type Retriever struct {
	llm llmclient.Client
}

// New 构造检索 agent。
func New(llm llmclient.Client) *Retriever { return &Retriever{llm: llm} }

// retrievePlan LLM 输出的检索计划。
type retrievePlan struct {
	Queries []string `json:"queries"`
}

// Retrieve 实现 agent.Retriever：先把需求单提炼成检索 query，再对每个 query 单次检索。
func (r *Retriever) Retrieve(ctx context.Context, req agent.RetrieveRequest) (*agent.RetrieveResult, error) {
	// 1. LLM 提炼检索 query
	prompt := buildPlanPrompt(req.Requirement)
	var plan retrievePlan
	if err := r.llm.ChatWithJSON(ctx, []llmclient.ChatMessage{{Role: "user", Content: prompt}}, &plan); err != nil {
		return nil, fmt.Errorf("提炼检索计划失败: %w", err)
	}
	if len(plan.Queries) == 0 {
		plan.Queries = []string{req.Requirement.Title + " " + req.Requirement.ChapterRequirement}
	}

	// 2. 每个 query 单次检索 + 去重
	seen := map[string]bool{}
	var evidence []agent.Evidence
	for _, q := range plan.Queries {
		evs, err := service.SearchKbase(ctx, req.TenantID, q, req.FileIDs...)
		if err != nil {
			return nil, fmt.Errorf("检索 query %q 失败: %w", q, err)
		}
		for _, e := range evs {
			key := fmt.Sprintf("%d:%s:%d", e.FileID, e.VersionMd5, e.ChunkIndex)
			if seen[key] {
				continue
			}
			seen[key] = true
			evidence = append(evidence, agent.Evidence{
				FileID:       e.FileID,
				VersionMd5:   e.VersionMd5,
				ChunkIndex:   e.ChunkIndex,
				ChapterTitle: e.ChapterTitle,
				SourceText:   e.SourceText,
				Score:        e.Score,
			})
		}
	}

	return &agent.RetrieveResult{Evidence: evidence, Queries: plan.Queries}, nil
}

func buildPlanPrompt(r agent.Requirement) string {
	return fmt.Sprintf(`你是一个政企内容检索助手。请根据下面的稿件需求，提炼出需要从知识库检索的若干个检索关键词/问题（queries），用于检索支撑稿件的资料。

稿件需求：
- 标题：%s
- 发文主体：%s
- 发文目的：%s
- 目标受众：%s
- 章节要求：%s
- 字数要求：%d

只返回 JSON，格式：{"queries":["检索词1","检索词2",...]}，3~5 个即可。`,
		r.Title, r.StyleSubject, r.StylePurpose, r.StyleAudience, r.ChapterRequirement, r.WordCount)
}
