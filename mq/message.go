package mq

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"

	"github.com/WWaynee/content-hub/config"
)

// DocumentParseMsg 文档解析任务消息体（只放 ID，不放大文件内容）。
type DocumentParseMsg struct {
	MsgID      string `json:"msg_id"`
	TraceID    string `json:"trace_id"`
	TenantID   uint64 `json:"tenant_id"`
	FileID     uint64 `json:"file_id"`
	VersionID  uint64 `json:"version_id"`
}

// PublishDocumentParseTask 投递文档解析任务到队列。
func PublishDocumentParseTask(ctx context.Context, tenantID, fileID, versionID uint64) error {
	msg := DocumentParseMsg{
		MsgID:     genMsgID(),
		TenantID:  tenantID,
		FileID:    fileID,
		VersionID: versionID,
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return Publish(config.Get().RabbitMQ.QueueDocumentParse, body)
}

func genMsgID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "msg-fallback"
	}
	return hex.EncodeToString(b)
}
