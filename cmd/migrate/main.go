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
		// 支撑
		&model.AgentTask{}, &model.AuditLog{},
	}

	if err := db.AutoMigrate(models...); err != nil {
		log.Fatalf("AutoMigrate 失败: %v", err)
	}
	fmt.Println("迁移完成：18 张表已建/已同步。")

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
