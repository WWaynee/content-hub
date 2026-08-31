package service

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/WWaynee/content-hub/agent"
	"github.com/WWaynee/content-hub/storage"
	"github.com/WWaynee/content-hub/storage/model"
)

// 稿件生成产物落库：把 agent.Article（句级证据绑定）转成 DB 快照。

// bindingDraft 落库前的证据绑定草稿（记录它属于哪个稿件的句子 index）。
type bindingDraft struct {
	sentenceIndex int
	binding       model.EvidenceBinding
}

// PersistArticleSnapshot 把一次 generation/revision 的稿件落成 article_version 快照。
func PersistArticleSnapshot(ctx context.Context, tenantID, workspaceID uint64, versionNo int, article *agent.Article, evidence []agent.Evidence) (uint64, error) {
	// 1. 找到或创建 article 主记录
	a, err := storage.GetArticleByWorkspace(ctx, tenantID, workspaceID)
	if err != nil {
		a = &model.Article{
			WorkspaceID:      workspaceID,
			TenantID:         tenantID,
			CurrentVersionNo: versionNo,
			Title:            article.Title,
			Status:           "generated",
		}
		if cerr := storage.CreateArticle(ctx, a); cerr != nil {
			return 0, fmt.Errorf("创建稿件记录失败: %w", cerr)
		}
	} else {
		storage.GetDB().WithContext(ctx).Model(&model.Article{}).Where("id = ?", a.ID).
			Updates(map[string]interface{}{"current_version_no": versionNo, "title": article.Title})
	}

	// 2. 展开 article → sentences + binding 草稿
	var sentences []model.ArticleSentence
	var bindingDrafts []bindingDraft
	sentSeq := 0
	for _, sec := range article.Sections {
		for _, para := range sec.Paragraphs {
			for _, s := range para.Sentences {
				sentences = append(sentences, model.ArticleSentence{
					WorkspaceID:   workspaceID,
					TenantID:      tenantID,
					SentenceIndex: sentSeq,
					Content:       s.Text,
				})
				for orderNo, ref := range s.EvidenceRefs {
					if int(ref) >= len(evidence) {
						continue
					}
					e := evidence[ref]
					bindingDrafts = append(bindingDrafts, bindingDraft{
						sentenceIndex: sentSeq,
						binding: model.EvidenceBinding{
							TenantID:       tenantID,
							SourceType:     "knowledge",
							DocFileID:      e.FileID,
							DocSentenceID:  e.DocSentenceID,
							EvidenceStatus: "bound",
							OrderNo:        orderNo,
						},
					})
				}
				sentSeq++
			}
		}
	}

	full := buildArticleMarkdown(article)
	ver := &model.ArticleVersion{
		ArticleID:         a.ID,
		WorkspaceID:       workspaceID,
		TenantID:          tenantID,
		VersionNo:         versionNo,
		FullContent:       full,
		Status:            "completed",
		ReferencedVersion: 0,
	}

	// 3. 事务落库：article_version → sentences → bindings（按 sentenceIndex 关联句 ID）
	err = storage.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(ver).Error; err != nil {
			return err
		}
		for i := range sentences {
			sentences[i].ArticleVersionID = ver.ID
		}
		if len(sentences) > 0 {
			if err := tx.Create(&sentences).Error; err != nil {
				return err
			}
		}
		// 现在 sentences 已有自增 ID，按 sentenceIndex 回填 article_sentence_id
		for _, d := range bindingDrafts {
			if d.sentenceIndex >= len(sentences) {
				continue
			}
			b := d.binding
			b.ArticleVersionID = ver.ID
			b.ArticleSentenceID = sentences[d.sentenceIndex].ID
			if err := tx.Create(&b).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("稿件快照落库失败: %w", err)
	}
	return ver.ID, nil
}

// buildArticleMarkdown 把结构化稿件拼成 markdown。
func buildArticleMarkdown(a *agent.Article) string {
	var sb strings.Builder
	if a.Title != "" {
		sb.WriteString("# " + a.Title + "\n\n")
	}
	for _, sec := range a.Sections {
		if sec.Heading != "" {
			sb.WriteString("## " + sec.Heading + "\n\n")
		}
		for _, para := range sec.Paragraphs {
			var parts []string
			for _, s := range para.Sentences {
				parts = append(parts, s.Text)
			}
			sb.WriteString(strings.Join(parts, "") + "\n\n")
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}
