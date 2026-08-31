package main

import (
	"fmt"
	"log"

	"github.com/WWaynee/content-hub/config"
)

// configtest 配置自检工具：加载 .env 并打印非敏感配置项，验证配置可正确加载。
// 注意：不打印任何敏感字段（密钥/密码），仅本地调试用。
func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	fmt.Printf("Server  HTTPPort=%d\n", cfg.Server.HTTPPort)
	fmt.Printf("MySQL   %s:%d/%s (user=%s)\n", cfg.MySQL.Host, cfg.MySQL.Port, cfg.MySQL.Database, cfg.MySQL.User)
	fmt.Printf("Redis   %s:%d\n", cfg.Redis.Host, cfg.Redis.Port)
	fmt.Printf("Qdrant  %s:%d (gRPC=%d)\n", cfg.Qdrant.Host, cfg.Qdrant.Port, cfg.Qdrant.GRPCPort)
	fmt.Printf("RabbitMQ %s:%d (queue=%s)\n", cfg.RabbitMQ.Host, cfg.RabbitMQ.Port, cfg.RabbitMQ.QueueDocumentParse)
	fmt.Printf("OSS     bucket=%s region=%s\n", cfg.OSS.Bucket, cfg.OSS.Region)
	fmt.Printf("LLM     chat=%s embed=%s\n", cfg.LLM.ChatModel, cfg.Embedding.Model)
	fmt.Printf("Chunk   strategy=%s size=%d\n", cfg.Chunk.Strategy, cfg.Chunk.Size)
	fmt.Println("配置加载成功（敏感字段已隐去）")
}
