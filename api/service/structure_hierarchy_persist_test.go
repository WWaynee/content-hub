package service

import (
	"context"
	"testing"

	"github.com/WWaynee/content-hub/agent"
	"github.com/WWaynee/content-hub/config"
	"github.com/WWaynee/content-hub/storage"
)

// TestPersistStructuredHierarchy P03：验证 PersistArticleSnapshot 真的把章节/段落/句内三段结构
// 写进 article_sentences，并能按 (section,paragraph,sentence) 三元精确读回重建（不再全 0/扁平）。
func TestPersistStructuredHierarchy(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("配置加载失败: %v", err)
	}
	if _, err := storage.InitMySQL(&cfg.MySQL); err != nil {
		t.Skipf("MySQL 不可用: %v", err)
	}
	ctx := context.Background()
	tenantID := uint64(99990604)

	w, err := CreateWorkspace(ctx, tenantID, 1, "结构化测试", nil)
	if err != nil {
		t.Fatalf("创建 workspace 失败: %v", err)
	}

	// 2 个 section、多个 paragraph、多句
	art := &agent.Article{
		Title: "结构稿",
		Sections: []agent.Section{
			{
				Heading: "背景",
				Paragraphs: []agent.Paragraph{
					{Sentences: []agent.Sentence{{Text: "概述A。", EvidenceRefs: []uint64{}}, {Text: "概述B。", EvidenceRefs: []uint64{}}}},
					{Sentences: []agent.Sentence{{Text: "概述C。", EvidenceRefs: []uint64{}}}},
				},
			},
			{
				Heading: "做法",
				Paragraphs: []agent.Paragraph{
					{Sentences: []agent.Sentence{{Text: "步骤甲。", EvidenceRefs: []uint64{}}}},
					{Sentences: []agent.Sentence{{Text: "步骤乙。", EvidenceRefs: []uint64{}}, {Text: "步骤丙。", EvidenceRefs: []uint64{}}}},
				},
			},
		},
	}

	if _, err := PersistArticleSnapshot(ctx, tenantID, w.ID, art, nil); err != nil {
		t.Fatalf("落库结构稿失败: %v", err)
	}
	aa, gErr := storage.GetArticleByWorkspace(ctx, tenantID, w.ID)
	if gErr != nil || aa == nil {
		t.Fatalf("article 不存在: %v", gErr)
	}
	ver, err := storage.GetLatestArticleVersion(ctx, aa.ID)
	if err != nil {
		t.Fatalf("读最新版本失败: %v", err)
	}
	sents, err := storage.ListArticleSentences(ctx, ver.ID)
	if err != nil {
		t.Fatalf("读句子失败: %v", err)
	}

	// 期望的 (section, paragraph, sentenceInPara)
	want := [][3]int{
		{0, 0, 0}, {0, 0, 1},
		{0, 1, 0},
		{1, 0, 0},
		{1, 1, 0}, {1, 1, 1},
	}
	if len(sents) != len(want) {
		t.Fatalf("句子数应=%d 实得 %d len", len(want), len(sents))
	}
	for i, s := range sents {
		w := want[i]
		if s.SectionIndex != w[0] || s.ParagraphIndex != w[1] || s.SentenceIndex != w[2] {
			t.Fatalf("句[%d] 结构应 (%d,%d,%d) 实得 (%d,%d,%d) content=%q",
				i, w[0], w[1], w[2], s.SectionIndex, s.ParagraphIndex, s.SentenceIndex, s.Content)
		}
	}
	t.Logf("P03 结构化落库验证通过: %d 句按 (section,paragraph,sentence) 精确重建", len(sents))
}
