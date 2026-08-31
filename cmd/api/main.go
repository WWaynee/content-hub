package main

import (
	"fmt"
	"log"

	"github.com/WWaynee/content-hub/api"
	"github.com/WWaynee/content-hub/config"
	"github.com/WWaynee/content-hub/mq"
	"github.com/WWaynee/content-hub/storage"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 连接 MySQL（必需）
	if _, err := storage.InitMySQL(&cfg.MySQL); err != nil {
		log.Fatalf("初始化 MySQL 失败: %v", err)
	}
	// 连接 Redis（必需，限流依赖）
	if _, err := storage.InitRedis(cfg.Redis.Host, cfg.Redis.Port, cfg.Redis.Password, 0); err != nil {
		log.Fatalf("初始化 Redis 失败: %v", err)
	}
	// 连接 RabbitMQ（上传时投递文档解析任务）
	if err := mq.InitRabbitMQ(); err != nil {
		log.Fatalf("初始化 RabbitMQ 失败: %v", err)
	}
	// 连接 Qdrant（检索/生成/问答在 API 进程会用到）
	if err := storage.InitQdrant(4096); err != nil {
		log.Fatalf("初始化 Qdrant 失败: %v", err)
	}
	// 连接 OSS（上传/预览/下载在 API 进程会用到）
	if err := storage.InitOSS(); err != nil {
		log.Fatalf("初始化 OSS 失败: %v", err)
	}

	r := api.NewRouter()
	addr := fmt.Sprintf(":%d", cfg.Server.HTTPPort)
	log.Printf("content-hub API 启动于 %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}
