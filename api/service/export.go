package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/WWaynee/content-hub/storage"
	"github.com/WWaynee/content-hub/storage/model"
)

// 导出：合并 md（正文 + 证据清单）。

// ExportArticle 导出某稿件版本为合并 md（正文在前，证据清单在后）。
func ExportArticle(ctx context.Context, articleVersionID uint64) (string, error) {
	var ver model.ArticleVersion
	if err := storage.GetDB().WithContext(ctx).Where("id = ?", articleVersionID).First(&ver).Error; err != nil {
		return "", fmt.Errorf("查稿件版本失败: %w", err)
	}

	// 1. 该版本的句子（按 sentence_index 排序）
	var sents []model.ArticleSentence
	storage.GetDB().WithContext(ctx).Where("article_version_id = ?", articleVersionID).
		Order("sentence_index ASC").Find(&sents)

	// 2. 证据绑定（按 sentence + order_no）
	var binds []model.EvidenceBinding
	storage.GetDB().WithContext(ctx).Where("article_version_id = ?", articleVersionID).
		Order("article_sentence_id ASC, order_no ASC").Find(&binds)

	// 3. 组装证据清单（回溯 doc_sentence 原文）
	var sb strings.Builder
	sb.WriteString(ver.FullContent)
	sb.WriteString("\n\n---\n\n# 证据清单\n\n")

	// 按 article_sentence_id 分组 bindings
	bindMap := map[uint64][]model.EvidenceBinding{}
	for _, b := range binds {
		bindMap[b.ArticleSentenceID] = append(bindMap[b.ArticleSentenceID], b)
	}

	hasEvidence := false
	for _, s := range sents {
		bs := bindMap[s.ID]
		if len(bs) == 0 {
			continue
		}
		hasEvidence = true
		sb.WriteString(fmt.Sprintf("【句子】%s\n", s.Content))
		for _, b := range bs {
			docSentence, err := storage.GetSentenceByID(ctx, b.DocSentenceID)
			if err != nil {
				continue
			}
			sb.WriteString(fmt.Sprintf("  - 来源：文档 %d（版本 %s）\n", b.DocFileID, docSentence.VersionMd5))
			sb.WriteString(fmt.Sprintf("    原文：%s\n", docSentence.Content))
		}
		sb.WriteString("\n")
	}
	if !hasEvidence {
		sb.WriteString("（无证据）\n")
	}
	return sb.String(), nil
}
