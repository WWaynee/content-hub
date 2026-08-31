package main

import (
	"context"
	"encoding/json"
	"log"

	"github.com/WWaynee/content-hub/api/service"
	"github.com/WWaynee/content-hub/config"
	"github.com/WWaynee/content-hub/mq"
	"github.com/WWaynee/content-hub/storage"
)

// worker 进程：消费 document_parse 队列，执行文档解析向量化。
func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	if _, err := storage.InitMySQL(&cfg.MySQL); err != nil {
		log.Fatalf("初始化 MySQL 失败: %v", err)
	}
	if err := storage.InitOSS(); err != nil {
		log.Fatalf("初始化 OSS 失败: %v", err)
	}
	if err := storage.InitQdrant(4096); err != nil {
		log.Fatalf("初始化 Qdrant 失败: %v", err)
	}
	if err := mq.InitRabbitMQ(); err != nil {
		log.Fatalf("初始化 RabbitMQ 失败: %v", err)
	}

	log.Println("worker 启动，消费文档解析队列...")

	err = mq.Consume(cfg.RabbitMQ.QueueDocumentParse, func(body []byte) error {
		var msg mq.DocumentParseMsg
		if err := json.Unmarshal(body, &msg); err != nil {
			log.Printf("解析消息失败（丢弃）: %v", err)
			return nil // 无法解析的消息直接 ACK，避免死循环
		}
		log.Printf("处理文档解析: file=%d version=%d", msg.FileID, msg.VersionID)
		if err := service.ProcessDocument(context.Background(), msg.TenantID, msg.FileID, msg.VersionID); err != nil {
			// ProcessDocument 内部已将版本状态置为 fail，这里 Ack（不重入队），
			// 避免"落库部分完成 + embedding 失败"重跑撞唯一索引造成无限死循环。
			log.Printf("处理失败（已置 fail，Ack 不重入队）: %v", err)
			return nil
		}
		log.Printf("处理成功: file=%d version=%d", msg.FileID, msg.VersionID)
		return nil
	})
	if err != nil {
		log.Fatalf("消费失败: %v", err)
	}
}
