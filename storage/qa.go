package storage

import (
	"context"

	"github.com/WWaynee/content-hub/storage/model"
)

// 知识库问答会话存储层（独立于工作区会话）。

func CreateQASession(ctx context.Context, s *model.QASession) error {
	return GetDB().WithContext(ctx).Create(s).Error
}

func GetQASession(ctx context.Context, tenantID, userID, id uint64) (*model.QASession, error) {
	var s model.QASession
	if err := GetDB().WithContext(ctx).
		Where("id = ? AND tenant_id = ? AND user_id = ?", id, tenantID, userID).
		First(&s).Error; err != nil {
		return nil, err
	}
	return &s, nil
}

func ListQASessions(ctx context.Context, tenantID, userID uint64) ([]model.QASession, error) {
	var list []model.QASession
	if err := GetDB().WithContext(ctx).
		Where("tenant_id = ? AND user_id = ?", tenantID, userID).
		Order("updated_at DESC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func AppendQAMessage(ctx context.Context, m *model.QAMessage) error {
	return GetDB().WithContext(ctx).Create(m).Error
}

func ListQAMessages(ctx context.Context, sessionID uint64) ([]model.QAMessage, error) {
	var list []model.QAMessage
	if err := GetDB().WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("id ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func UpdateQASessionTitle(ctx context.Context, sessionID uint64, title string) error {
	return GetDB().WithContext(ctx).Model(&model.QASession{}).
		Where("id = ?", sessionID).Updates(map[string]interface{}{"title": title, "updated_at": nil}).Error
}
