// Package evidence 实现证据整理 agent：把稿件句子 + 证据绑定格式化输出为证据清单。
// 只做格式化，不做重检（证据对应关系在撰写阶段已建立）。
package evidence

import (
	"context"

	"github.com/WWaynee/content-hub/agent"
)

// Builder 证据整理 agent。
type Builder struct{}

// New 构造证据整理 agent。
func New() *Builder { return &Builder{} }

// Build 实现 agent.EvidenceBuilder。
func (b *Builder) Build(ctx context.Context, article *agent.Article, evidence []agent.Evidence) (*agent.EvidenceManifest, error) {
	manifest := &agent.EvidenceManifest{}
	idx := 0
	for _, sec := range article.Sections {
		for _, para := range sec.Paragraphs {
			for _, sent := range para.Sentences {
				for _, ref := range sent.EvidenceRefs {
					if int(ref) >= len(evidence) {
						continue
					}
					e := evidence[ref]
					manifest.Entries = append(manifest.Entries, agent.EvidenceManifestEntry{
						SentenceIndex: idx,
						SentenceText:  sent.Text,
						SourceText:    e.SourceText,
						FileID:        e.FileID,
						VersionMd5:    e.VersionMd5,
						ChapterTitle:  e.ChapterTitle,
					})
				}
				idx++
			}
		}
	}
	return manifest, nil
}
