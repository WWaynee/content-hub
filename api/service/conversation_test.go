package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/WWaynee/content-hub/agent"
	"github.com/WWaynee/content-hub/config"
	"github.com/WWaynee/content-hub/storage"
)

// TestConversationPersistence 验证会话 + 消息(存 action plan JSON + target 锚点)落库。
func TestConversationPersistence(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("配置加载失败: %v", err)
	}
	if _, err := storage.InitMySQL(&cfg.MySQL); err != nil {
		t.Skipf("MySQL 不可用: %v", err)
	}
	ctx := context.Background()
	tenantID := uint64(99990040)

	w, _ := CreateWorkspace(ctx, tenantID, 1, "会话测试", nil)

	// 确保会话
	conv, err := EnsureConversation(ctx, tenantID, w.ID, 1)
	if err != nil {
		t.Fatalf("EnsureConversation 失败: %v", err)
	}

	// 追加用户消息（带 target 锚点）
	if err := AppendUserMessage(ctx, conv.ID, tenantID, 1, "改第2句", "sentence", 1, "trace-123"); err != nil {
		t.Fatalf("AppendUserMessage 失败: %v", err)
	}

	// 追加 action plan 消息
	plan := &agent.DialoguePlan{Actions: []agent.DialogueAction{
		{Tool: "revise_article_sentence", TargetSentenceIndex: 1, Instruction: "改正式点"},
	}}
	if err := AppendPlanMessage(ctx, conv.ID, tenantID, 1, plan, "trace-123"); err != nil {
		t.Fatalf("AppendPlanMessage 失败: %v", err)
	}

	// 验证消息落库
	msgs, err := storage.ListConversationMessages(ctx, conv.ID)
	if err != nil {
		t.Fatalf("ListConversationMessages 失败: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("应落 2 条消息，实际 %d", len(msgs))
	}
	if msgs[1].Kind != "tool_call" {
		t.Fatalf("第2条应为 tool_call，实际 %q", msgs[1].Kind)
	}
	// 验证 action plan JSON 可反序列化
	var back agent.DialoguePlan
	if err := json.Unmarshal([]byte(msgs[1].Content), &back); err != nil {
		t.Fatalf("action plan JSON 无法解析: %v", err)
	}
	if len(back.Actions) != 1 || back.Actions[0].Tool != "revise_article_sentence" {
		t.Fatalf("action plan 内容不符: %+v", back)
	}

	// 清理
	db := storage.GetDB()
	db.Exec("DELETE FROM conversation_messages WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM conversations WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM requirements WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM workspaces WHERE tenant_id = ?", tenantID)
}
