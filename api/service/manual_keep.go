package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/WWaynee/content-hub/storage"
	"github.com/WWaynee/content-hub/storage/model"
)

// manual_keep.go — P09 的"对无源句子作者拍板"落库端：
//
// 用在稿面上被标成 no_source(黄点、待人工) 的一句上。此处不改变正文在稿里的位置(删句要删走它以
// P08 sequence.delete 表达)，而只切这一句的"声明"：作者认可它"是我自己的内容、可有外部依据缺失"
// (→ evidence_status: no_source → human_kept，不再黄) 或反悔退回 (→ no_source)。
// bound 句(有真实 doc 引用，DocSentenceID != 0)不允许被这处被动降级。

var (
	ErrMarkNotExist   = errors.New("该句在当前稿件版本不存在")
	ErrMarkHasRealSrc = errors.New("该句已有可引用来源（bound），不允许直接降二级为'无外部依据'。若确要改写可用编辑并显式去掉来源后再标注")
)

// MarkSentenceManual 对最新稿件版本的某句做人工“无源取舍”标记。
// action ∈ { keep_no_source, ack_human, reset_no_source }：
//   - ack_human：作者把这句话当成“无外部依据但由我创作/特有措辞”保留 → 解除黄点(human_kept)；
//   - reset_no_source / keep_no_source：退回黄点待核态(no_source)。
//
// 会复用该句若已存在的无源占位行(create-or-update)，不新增冗余行。
func MarkSentenceManual(ctx context.Context, tenantID, workspaceID, sentenceID uint64, action string) error {
	a, err := storage.GetArticleByWorkspace(ctx, tenantID, workspaceID)
	if err != nil {
		return fmt.Errorf("稿件不存在")
	}
	ver, err := storage.GetLatestArticleVersion(ctx, a.ID)
	if err != nil {
		return fmt.Errorf("稿件版本不存在")
	}
	if !sentenceExists(ctx, ver.ID, sentenceID) {
		return ErrMarkNotExist
	}
	// 确认没有真实来源（存在会拒绝的占位才允许），保留 bound 不动 → 报错
	hasReal, pidx := findSentenceSourcedOrPlaceholder(ctx, ver.ID, sentenceID)
	if hasReal {
		return ErrMarkHasRealSrc
	}
	desired, err := markActionToStatus(action)
	if err != nil {
		return err
	}
	// 已存在占位：原地更新；不存在：新建占位行（此句无任何绑定 → 待作者表态）。
	db := storage.GetDB().WithContext(ctx)
	if pidx >= 0 {
		return db.Model(&model.EvidenceBinding{}).
			Where("id = ?", pidx).
			Update("evidence_status", desired).Error
	}
	return db.Create(&model.EvidenceBinding{
		ArticleVersionID:  ver.ID,
		ArticleSentenceID: sentenceID,
		TenantID:          tenantID,
		SourceType:        "knowledge",
		EvidenceStatus:    desired,
		OrderNo:           0,
	}).Error
}

func markActionToStatus(action string) (string, error) {
	switch action {
	case "keep_no_source", "reset_no_source":
		return ClaimTypeNoSource, nil
	case "ack_human":
		return ClaimTypeHumanKept, nil
	default:
		return "", fmt.Errorf("未知动作: %s", action)
	}
}

// findSentenceSourcedOrPlaceholder 返回 (hasRealSource, placeholderBindingID)。
func findSentenceSourcedOrPlaceholder(ctx context.Context, versionID, sentenceID uint64) (bool, int) {
	binds, err := storage.ListArticleBindings(ctx, versionID)
	if err != nil {
		return false, -1
	}
	var pid = int64(-1)
	hasReal := false
	for i := range binds {
		if binds[i].ArticleSentenceID != sentenceID {
			continue
		}
		if binds[i].DocSentenceID != 0 {
			hasReal = true
			continue
		}
		pid = int64(binds[i].ID)
	}
	return hasReal, int(pid)
}

func sentenceExists(ctx context.Context, versionID, sentenceID uint64) bool {
	sents, err := storage.ListArticleSentences(ctx, versionID)
	if err != nil {
		return false
	}
	for _, s := range sents {
		if s.ID == sentenceID {
			return true
		}
	}
	return false
}
