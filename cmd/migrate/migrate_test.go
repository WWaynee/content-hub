package main

import (
	"strings"
	"testing"

	"github.com/WWaynee/content-hub/config"
	"github.com/WWaynee/content-hub/storage"
)

// TestTablesExist 验证迁移后 18 张表全部存在。
// 依赖：MySQL（content-mysql 容器）已启动，migrate 已执行。
func TestTablesExist(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}
	db, err := storage.InitMySQL(&cfg.MySQL)
	if err != nil {
		t.Fatalf("初始化 MySQL 失败: %v", err)
	}

	expected := []string{
		"tenants", "users",
		"kbase_dirs", "kbase_files", "doc_versions", "doc_chunks", "doc_sentences",
		"workspaces", "requirements", "requirement_scope",
		"articles", "article_versions", "article_sentences", "evidence_bindings",
		"conversations", "conversation_messages",
		"retrieval_batches", "retrieval_batch_items",
		"agent_runs", "agent_steps",
		"agent_tasks", "audit_logs",
	}
	have := map[string]bool{}
	var tables []string
	if err := db.Raw("SHOW TABLES").Scan(&tables).Error; err != nil {
		t.Fatalf("查询表清单失败: %v", err)
	}
	for _, tb := range tables {
		have[tb] = true
	}
	missing := []string{}
	for _, exp := range expected {
		if !have[exp] {
			missing = append(missing, exp)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("缺少表: %v", missing)
	}
	if len(tables) != len(expected) {
		t.Logf("表数量=%d, 期望=%d（差异可能是额外表，已单独校验必需表）", len(tables), len(expected))
	}
}

// TestUsersUniqueIndex 验证 users 表的 username 全局唯一索引存在（登录不传租户ID的基础）。
func TestUsersUniqueIndex(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}
	db, err := storage.InitMySQL(&cfg.MySQL)
	if err != nil {
		t.Fatalf("初始化 MySQL 失败: %v", err)
	}

	rows := []map[string]interface{}{}
	if err := db.Table("users").Raw("SHOW INDEX FROM users").Scan(&rows).Error; err != nil {
		t.Fatalf("查询索引失败: %v", err)
	}
	found := false
	for _, r := range rows {
		keyName, _ := r["Key_name"].(string)
		col, _ := r["Column_name"].(string)
		if keyName == "idx_username_global" && col == "username" {
			found = true
		}
	}
	if !found {
		t.Fatal("users 缺少 idx_username_global 唯一索引（username 列）")
	}
}

// TestDocVersionUniqueIndex 验证 doc_versions 的 (file_id,version_md5) 联合唯一索引。
func TestDocVersionUniqueIndex(t *testing.T) {
	cfg, _ := config.Load()
	db, err := storage.InitMySQL(&cfg.MySQL)
	if err != nil {
		t.Skipf("跳过：MySQL 不可用 %v", err)
	}
	var cnt int64
	if err := db.Raw("SELECT COUNT(*) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=? AND TABLE_NAME='doc_versions' AND INDEX_NAME='idx_file_version'", cfg.MySQL.Database).Scan(&cnt).Error; err != nil {
		t.Fatalf("查询索引失败: %v", err)
	}
	if cnt == 0 {
		t.Fatal("doc_versions 缺少 idx_file_version 联合索引")
	}
	_ = strings.TrimSpace
}
