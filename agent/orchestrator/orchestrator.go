// Package orchestrator 实现 content-hub 的多 Agent 工作流编排。
//
// 采用「全局编排 + 局部自主」：Orchestrator 按确定的工作流调度各 agent，
// agent 之间通过结构化数据（agent 包的类型）传递，不互相自由指挥。
package orchestrator

import (
	"context"

	"github.com/WWaynee/content-hub/agent"
)

// Retriever 知识检索 agent 接口。
type Retriever interface {
	Retrieve(ctx context.Context, req agent.RetrieveRequest) (*agent.RetrieveResult, error)
}

// Writer 稿件撰写 agent 接口。
type Writer interface {
	Write(ctx context.Context, req agent.WritingRequest) (*agent.Article, error)
}

// EvidenceBuilder 证据整理 agent 接口。
type EvidenceBuilder interface {
	Build(ctx context.Context, article *agent.Article, evidence []agent.Evidence) (*agent.EvidenceManifest, error)
}

// Orchestrator 工作流编排器。
type Orchestrator struct {
	retriever Retriever
	writer    Writer
	evidence  EvidenceBuilder
}

// New 构造编排器。
func New(r Retriever, w Writer, e EvidenceBuilder) *Orchestrator {
	return &Orchestrator{retriever: r, writer: w, evidence: e}
}

// GenerationResult 一次稿件生成（generation）的完整产物。
type GenerationResult struct {
	Article  *agent.Article
	Evidence []agent.Evidence
	Manifest *agent.EvidenceManifest
	Queries  []string
}

// Generate 执行「需求 → 检索 → 撰写 → 证据」完整 generation 工作流。
func (o *Orchestrator) Generate(ctx context.Context, tenantID uint64, req agent.Requirement, fileIDs []uint64) (*GenerationResult, error) {
	// 1. 检索
	ret, err := o.retriever.Retrieve(ctx, agent.RetrieveRequest{
		TenantID:    tenantID,
		Requirement: req,
		FileIDs:     fileIDs,
	})
	if err != nil {
		return nil, err
	}

	// 2. 撰写
	article, err := o.writer.Write(ctx, agent.WritingRequest{
		Requirement: req,
		Evidence:    ret.Evidence,
	})
	if err != nil {
		return nil, err
	}

	// 3. 证据整理
	manifest, err := o.evidence.Build(ctx, article, ret.Evidence)
	if err != nil {
		return nil, err
	}

	return &GenerationResult{
		Article:  article,
		Evidence: ret.Evidence,
		Manifest: manifest,
		Queries:  ret.Queries,
	}, nil
}
