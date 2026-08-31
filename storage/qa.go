package storage

import (
	"context"

	"gorm.io/gorm"

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

// RenameQASession 改会话标题（校验归属 租户+用户，返回 gorm.ErrRecordNotFound 若不归属）。
func RenameQASession(ctx context.Context, tenantID, userID, sessionID uint64, title string) error {
	res := GetDB().WithContext(ctx).Model(&model.QASession{}).
		Where("id = ? AND tenant_id = ? AND user_id = ?", sessionID, tenantID, userID).
		Updates(map[string]interface{}{"title": title})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// DeleteQASession 删除会话及其消息（软删会话，仅校验归属 租户+用户）。
func DeleteQASession(ctx context.Context, tenantID, userID, sessionID uint64) error {
	res := GetDB().WithContext(ctx).Where("id = ? AND tenant_id = ? AND user_id = ?", sessionID, tenantID, userID).
		Delete(&model.QASession{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	// 逻辑删除消息（保留审计）
	return GetDB().WithContext(ctx).Where("session_id = ? AND tenant_id = ? AND user_id = ?", sessionID, tenantID, userID).
		Delete(&model.QAMessage{}).Error
}
