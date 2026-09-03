package main

import (
	"fmt"
	"log"

	"github.com/WWaynee/content-hub/config"
	"github.com/WWaynee/content-hub/storage"
	"github.com/WWaynee/content-hub/storage/model"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	db, err := storage.InitMySQL(&cfg.MySQL)
	if err != nil {
		log.Fatalf("初始化 MySQL 失败: %v", err)
	}

	// 全部模型（18 张表）
	models := []interface{}{
		// 账号租户
		&model.Tenant{}, &model.User{},
		// 知识库
		&model.KbaseDir{}, &model.KbaseFile{}, &model.DocVersion{},
		&model.DocChunk{}, &model.DocSentence{},
		// 稿件
		&model.Workspace{}, &model.Requirement{}, &model.RequirementScope{},
		&model.Article{}, &model.ArticleVersion{}, &model.ArticleSentence{},
		&model.EvidenceBinding{},
		// 会话
		&model.Conversation{}, &model.ConversationMessage{},
		// 检索快照（阶段6）
		&model.RetrievalBatch{}, &model.RetrievalBatchItem{},
		// 知识库问答（阶段7）
		&model.QASession{}, &model.QAMessage{},
		// agent run/step（P05 一等持久实体）
		&model.AgentRun{}, &model.AgentStep{},
		// 支撑
		&model.AgentTask{}, &model.AuditLog{},
	}

	// 兼容升级：旧版 users.username 是租户内唯一联合索引 idx_tenant_user，
	// 现改用户名全局唯一 idx_username_global。先删旧索引避免冲突，再 AutoMigrate 建新索引。
	// （新装库没有旧索引，删除是幂等的。）
	if db.Migrator().HasIndex(&model.User{}, "idx_tenant_user") {
		if err := db.Migrator().DropIndex(&model.User{}, "idx_tenant_user"); err != nil {
			log.Fatalf("删除旧索引 idx_tenant_user 失败: %v", err)
		}
		fmt.Println("已删除旧索引 idx_tenant_user（用户名改为全局唯一）。")
	}

	if err := db.AutoMigrate(models...); err != nil {
		log.Fatalf("AutoMigrate 失败: %v", err)
	}
	fmt.Println("迁移完成：表已建/已同步。")

	// 列出实际表验证
	var tables []string
	if err := db.Raw("SHOW TABLES").Scan(&tables).Error; err != nil {
		log.Fatalf("查询表清单失败: %v", err)
	}
	fmt.Printf("当前库中表（%d 张）:\n", len(tables))
	for _, t := range tables {
		fmt.Println("  -", t)
	}
}
