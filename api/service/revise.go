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

// 段落/句子级 AI 修订的落库（未动句继承 + 被改句更新 + 落新快照）。

// ReviseSentenceInput 一次句子修订的输入。
type ReviseSentenceInput struct {
	WorkspaceID     uint64
	TenantID        uint64
	TargetIndex     int               // 被改句序号（在整篇稿件的句子序列中的下标）
	NewText         string            // 新句子文本
	NewEvidence     []agent.Evidence  // 被改句重检测到的证据
	NewEvidenceRefs []uint64          // 被改句新绑定的证据索引（指向 NewEvidence）
}

// sentDraft 句子草稿，记录其原句 index（用于绑定关联）。
type sentDraft struct {
	content string
	// 未动句继承的原 doc_sentence 绑定；被改句为空（用 NewEvidence 重建）
	inheritedBinds []model.EvidenceBinding
}

// ApplyArticleRevision 把一次句子修订应用并落新稿件快照：未动句继承原文本+原证据，被改句更新。
// 返回新 article_version ID。
func ApplyArticleRevision(ctx context.Context, in ReviseSentenceInput) (uint64, error) {
	a, err := storage.GetArticleByWorkspace(ctx, in.TenantID, in.WorkspaceID)
	if err != nil {
		return 0, fmt.Errorf("稿件不存在")
	}
	prev, err := storage.GetLatestArticleVersion(ctx, a.ID)
	if err != nil {
		return 0, fmt.Errorf("稿件版本不存在")
	}
	sents, err := storage.ListArticleSentences(ctx, prev.ID)
	if err != nil {
		return 0, err
	}
	binds, err := storage.ListArticleBindings(ctx, prev.ID)
	if err != nil {
		return 0, err
	}
	if in.TargetIndex < 0 || in.TargetIndex >= len(sents) {
		return 0, fmt.Errorf("目标句子序号越界: %d", in.TargetIndex)
	}

	// 按 sentence_id 分组原绑定
	bindBySent := map[uint64][]model.EvidenceBinding{}
	for _, b := range binds {
		bindBySent[b.ArticleSentenceID] = append(bindBySent[b.ArticleSentenceID], b)
	}

	// 组装新句子草稿：未动句继承内容+绑定，被改句换内容
	drafts := make([]sentDraft, len(sents))
	for i, s := range sents {
		if i == in.TargetIndex {
			drafts[i] = sentDraft{content: in.NewText}
		} else {
			// 继承内容 + 该句原绑定（ID 清零，落新库）
			inherited := bindBySent[s.ID]
			for j := range inherited {
				inherited[j].ID = 0
				inherited[j].ArticleVersionID = 0
				inherited[j].ArticleSentenceID = 0 // 落库时回填新句 ID
			}
			drafts[i] = sentDraft{content: s.Content, inheritedBinds: inherited}
		}
	}

	newVer := &model.ArticleVersion{
		ArticleID:         a.ID,
		WorkspaceID:       in.WorkspaceID,
		TenantID:          in.TenantID,
		VersionNo:         prev.VersionNo + 1,
		FullContent:       buildContentFromDrafts(drafts),
		Status:            "completed",
		ReferencedVersion: int(prev.VersionNo),
	}

	// 事务落库：version → sentences（拿新 ID）→ bindings（按 index 关联 + 被改句新证据）
	err = storage.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(newVer).Error; err != nil {
			return err
		}
		newSents := make([]model.ArticleSentence, len(drafts))
		for i, d := range drafts {
			newSents[i] = model.ArticleSentence{
				ArticleVersionID: newVer.ID,
				WorkspaceID:      in.WorkspaceID,
				TenantID:         in.TenantID,
				SentenceIndex:    i,
				Content:          d.content,
			}
		}
		if err := tx.Create(&newSents).Error; err != nil {
			return err
		}

		// bindings
		for i, d := range drafts {
			if i == in.TargetIndex {
				// 被改句：新证据
				for orderNo, ref := range in.NewEvidenceRefs {
					if int(ref) >= len(in.NewEvidence) {
						continue
					}
					e := in.NewEvidence[ref]
					b := model.EvidenceBinding{
						ArticleVersionID:  newVer.ID,
						ArticleSentenceID: newSents[i].ID,
						TenantID:          in.TenantID,
						SourceType:        "knowledge",
						DocFileID:         e.FileID,
						DocSentenceID:     e.DocSentenceID,
						EvidenceStatus:    "bound",
						OrderNo:           orderNo,
					}
					if err := tx.Create(&b).Error; err != nil {
						return err
					}
				}
			} else {
				// 未动句：继承原绑定（回填新句 ID）
				for _, ib := range d.inheritedBinds {
					ib.ArticleVersionID = newVer.ID
					ib.ArticleSentenceID = newSents[i].ID
					if err := tx.Create(&ib).Error; err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("落修订快照失败: %w", err)
	}
	return newVer.ID, nil
}

func buildContentFromDrafts(drafts []sentDraft) string {
	var sb strings.Builder
	for _, d := range drafts {
		sb.WriteString(d.content)
	}
	return sb.String()
}
