package service

import (
	"context"

	"github.com/WWaynee/content-hub/observability"
	"github.com/WWaynee/content-hub/storage"
	"github.com/WWaynee/content-hub/storage/model"
)

// RecordAudit 记录审计日志到 audit_logs 表（尽力而为，失败仅 warn 不阻断业务）。
func RecordAudit(ctx context.Context, operation, content string) {
	entry := &model.AuditLog{
		TenantID:  observability.TenantIDFromCtx(ctx),
		UserID:    observability.UserIDFromCtx(ctx),
		Operation: operation,
		TraceID:   observability.TraceIDFromCtx(ctx),
		Content:   content,
	}
	if err := storage.GetDB().Create(entry).Error; err != nil {
		observability.WithContext(ctx).Warn("审计日志写入失败", map[string]interface{}{"operation": operation, "error": err.Error()})
	}
}
