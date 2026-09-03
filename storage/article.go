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

// GetLatestArticleVersion 取稿件最新版本快照。
func GetLatestArticleVersion(ctx context.Context, articleID uint64) (*model.ArticleVersion, error) {
	var v model.ArticleVersion
	if err := GetDB().WithContext(ctx).Where("article_id = ?", articleID).
		Order("version_no DESC").First(&v).Error; err != nil {
		return nil, err
	}
	return &v, nil
}

// ListArticleVersions 列出某稿件的全部版本快照（按 version_no 升序）。
func ListArticleVersions(ctx context.Context, articleID uint64) ([]model.ArticleVersion, error) {
	var list []model.ArticleVersion
	if err := GetDB().WithContext(ctx).Where("article_id = ?", articleID).
		Order("version_no ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// CASBumpArticleCurrentVersion 原子地（乐观锁）把某稿件的 current_version_no 从 expected 提升到 next。
// 返回 bool 是否加锁成功：true=该次是唯一把 expected→next 的提交者；false=行已被并发改写或打不到 expected
// （此时调用方应视为版本冲突，不得再落一份与之重复的新 version）。
//
// 依赖 mysql 行更新返回 RowsAffected==1；deleted_at 过滤软删。
func CASBumpArticleCurrentVersion(ctx context.Context, articleID, expected, next uint64) (bool, error) {
	res := GetDB().WithContext(ctx).
		Model(&model.Article{}).
		Where("id = ? AND current_version_no = ?", articleID, expected).
		Update("current_version_no", next)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// ListArticleSentences 列出某版本的全部句子，按结构化顺序 (section, paragraph, sentence) 三元升序。
func ListArticleSentences(ctx context.Context, versionID uint64) ([]model.ArticleSentence, error) {
	var list []model.ArticleSentence
	if err := GetDB().WithContext(ctx).Where("article_version_id = ?", versionID).
		Order("section_index ASC, paragraph_index ASC, sentence_index ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	// 兼容旧数据：若这条旧版本从未落结构（三 index 全 0 单段），退化为按创建序(id)返回即可保持可读
	if len(list) > 1 && allFlat(list) {
		var flat []model.ArticleSentence
		if err := GetDB().WithContext(ctx).Where("article_version_id = ?", versionID).
			Order("id ASC").Find(&flat).Error; err != nil {
			return nil, err
		}
		return flat, nil
	}
	return list, nil
}

// allFlat 判断某版本句子是否全部没有结构信息（生成代码升级前落库的旧版本：sec/para 恒 0，sent 全 0 或 顺序不明）。
// 这类旧版本无法用三元重建真实章节，退化为线性(单段)语义由读取方处理，不强猜。
func allFlat(list []model.ArticleSentence) bool {
	for _, s := range list {
		// 只要存在任一句有非 0 层次即为结构化新版本
		if s.SectionIndex != 0 || s.ParagraphIndex != 0 {
			return false
		}
	}
	// 若存在不止一个句子且全部 paragraph/section=0，则无法仅凭 (0,0,n) 区分"一个真段落内的第 n 句"
	// 与"旧版本的多段都被错误记为 0"。双保险：若 sentence_index 出现相邻不连续(0,1,2...)假设单段可。
	// 由于历史版本 sentence_index 也常为全局递增(旧实现)，这里统一按线性 id 处理。
	return true
}

// ListArticleBindings 列出某版本的全部证据绑定。
func ListArticleBindings(ctx context.Context, versionID uint64) ([]model.EvidenceBinding, error) {
	var list []model.EvidenceBinding
	if err := GetDB().WithContext(ctx).Where("article_version_id = ?", versionID).
		Order("article_sentence_id ASC, order_no ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}
