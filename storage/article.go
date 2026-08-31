package storage

import (
	"context"

	"gorm.io/gorm"

	"github.com/WWaynee/content-hub/storage/model"
)

// 稿件快照存储层（generation/revision 产物落库）。

func CreateArticle(ctx context.Context, a *model.Article) error {
	return GetDB().WithContext(ctx).Create(a).Error
}

func GetArticleByWorkspace(ctx context.Context, tenantID, workspaceID uint64) (*model.Article, error) {
	var a model.Article
	if err := GetDB().WithContext(ctx).
		Where("tenant_id = ? AND workspace_id = ?", tenantID, workspaceID).
		First(&a).Error; err != nil {
		return nil, err
	}
	return &a, nil
}

// SaveArticleVersion 事务落一份稿件快照：article_version + article_sentences + evidence_bindings。
// 每次 generation / 每次修订完成都形成一个新的 article_version。
func SaveArticleVersion(ctx context.Context, ver *model.ArticleVersion, sentences []model.ArticleSentence, bindings []model.EvidenceBinding) error {
	return GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(ver).Error; err != nil {
			return err
		}
		for i := range sentences {
			sentences[i].ArticleVersionID = ver.ID
			sentences[i].WorkspaceID = ver.WorkspaceID
			sentences[i].TenantID = ver.TenantID
		}
		if len(sentences) > 0 {
			if err := tx.Create(&sentences).Error; err != nil {
				return err
			}
		}
		for i := range bindings {
			bindings[i].ArticleVersionID = ver.ID
			bindings[i].TenantID = ver.TenantID
		}
		if len(bindings) > 0 {
			if err := tx.Create(&bindings).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
