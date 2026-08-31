//go:build integration

package service

import (
	"context"
	"testing"

	"github.com/WWaynee/content-hub/config"
	"github.com/WWaynee/content-hub/storage"
)

// TestQABotFlow 验证：上传文档→建问答会话→提问→检索回答→消息落库。
func TestQABotFlow(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("配置加载失败: %v", err)
	}
	if cfg.OSS.AccessKeyID == "" || cfg.Embedding.APIKey == "" || cfg.LLM.APIKey == "" {
		t.Skip("OSS/Embedding/LLM 未配置，跳过")
	}
	if _, err := storage.InitMySQL(&cfg.MySQL); err != nil {
		t.Skipf("MySQL 不可用: %v", err)
	}
	if storage.OSSClient == nil {
		_ = storage.InitOSS()
	}
	if storage.QdrantClient == nil {
		_ = storage.InitQdrant(4096)
	}
	ctx := context.Background()
	tenantID := uint64(99990060)

	// 1. 上传资料
	content := "# 招生简章\n\n## 报名条件\n本年度面向应届高中毕业生，年龄不超过25周岁。\n\n## 录取规则\n按高考总成绩从高到低依次录取。"
	_, err = IngestDocument(ctx, IngestParams{
		TenantID: tenantID, Scope: storage.ScopePrivate, OwnerUserID: 1, DirID: 0,
		FileName: "招生简章.md", Content: []byte(content),
	})
	if err != nil {
		t.Fatalf("IngestDocument 失败: %v", err)
	}

	// 2. 建会话
	sess, err := CreateQASession(ctx, tenantID, 1)
	if err != nil {
		t.Fatalf("CreateQASession 失败: %v", err)
	}

	// 3. 提问
	msg, err := AskQABot(ctx, tenantID, 1, sess.ID, "报名条件是什么？")
	if err != nil {
		t.Fatalf("AskQABot 失败: %v", err)
	}
	if msg.Role != "assistant" || msg.Content == "" {
		t.Fatalf("应返回 assistant 回答，实际 role=%s content=%q", msg.Role, msg.Content)
	}
	t.Logf("回答：%s", msg.Content)

	// 4. 验证消息落库（应 2 条：user + assistant）
	msgs, _ := storage.ListQAMessages(ctx, sess.ID)
	if len(msgs) != 2 {
		t.Fatalf("应落 2 条消息，实际 %d", len(msgs))
	}

	// 清理
	db := storage.GetDB()
	db.Exec("DELETE FROM qa_messages WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM qa_sessions WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM doc_sentences WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM doc_chunks WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM doc_versions WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM kbase_files WHERE tenant_id = ?", tenantID)
}
