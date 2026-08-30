package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/joho/godotenv"
)

// Config 聚合所有环境配置。
// 注意：包含敏感字段（Password/Key/Secret），不应整体打日志。
type Config struct {
	Server     Server
	MySQL      MySQL
	Redis      Redis
	Qdrant     Qdrant
	RabbitMQ   RabbitMQ
	JWT        JWT
	LLM        LLM
	Embedding  Embedding
	OSS        OSS
	Log        Log
	Chunk      Chunk
	Retrieval  Retrieval
}

type Server struct {
	HTTPPort int
}

type MySQL struct {
	Host     string
	Port     int
	User     string
	Password string // 敏感
	Database string
}

type Redis struct {
	Host     string
	Port     int
	Password string // 敏感
}

type Qdrant struct {
	Host string
	Port int
	GRPCPort int
}

type RabbitMQ struct {
	Host            string
	Port            int
	ManagementPort  int
	User            string
	Password        string // 敏感
	QueueDocumentParse string
	QueueArticleGenerate string
}

type JWT struct {
	Secret        string // 敏感
	ExpireSeconds int
}

type LLM struct {
	APIKey        string // 敏感
	BaseURL       string
	ChatModel     string
	TimeoutSeconds int
	MaxRetry      int
}

type Embedding struct {
	APIKey  string // 敏感
	BaseURL string
	Model   string
}

type OSS struct {
	Region          string
	Endpoint        string
	AccessKeyID     string // 敏感
	AccessKeySecret string // 敏感
	Bucket          string
}

type Log struct {
	Level string
	File  string
}

type Chunk struct {
	Strategy string
	Size     int
	Overlap  int
}

type Retrieval struct {
	TopK int
}

var cfg Config

// Load 读取 .env 并解析到全局 Config。可被 api/worker/migrate 复用。
func Load() (*Config, error) {
	loadEnvFile()

	c := Config{}
	var err error

	if c.Server.HTTPPort, err = envInt("SERVER_HTTP_PORT", 8181); err != nil {
		return nil, err
	}

	c.MySQL = MySQL{
		Host:     envStr("MYSQL_HOST", "127.0.0.1"),
		Database: envStr("MYSQL_DB", "content_hub"),
		User:     envStr("MYSQL_USER", "root"),
		Password: envStr("MYSQL_ROOT_PWD", ""),
	}
	if c.MySQL.Port, err = envInt("MYSQL_PORT", 4833); err != nil {
		return nil, err
	}

	c.Redis = Redis{
		Host:     envStr("REDIS_HOST", "127.0.0.1"),
		Password: envStr("REDIS_PASSWORD", ""),
	}
	if c.Redis.Port, err = envInt("REDIS_PORT", 8943); err != nil {
		return nil, err
	}

	c.Qdrant = Qdrant{Host: envStr("QDRANT_HOST", "127.0.0.1")}
	if c.Qdrant.Port, err = envInt("QDRANT_PORT", 6433); err != nil {
		return nil, err
	}
	if c.Qdrant.GRPCPort, err = envInt("QDRANT_GRPC_PORT", 6434); err != nil {
		return nil, err
	}

	c.RabbitMQ = RabbitMQ{
		Host:                envStr("RABBITMQ_HOST", "127.0.0.1"),
		User:                envStr("RABBITMQ_DEFAULT_USER", "content_admin"),
		Password:            envStr("RABBITMQ_DEFAULT_PASS", ""),
		QueueDocumentParse:  envStr("RABBITMQ_QUEUE_DOCUMENT_PARSE", "content_document_parse"),
		QueueArticleGenerate: envStr("RABBITMQ_QUEUE_ARTICLE_GENERATE", "content_article_generate"),
	}
	if c.RabbitMQ.Port, err = envInt("RABBITMQ_PORT", 5673); err != nil {
		return nil, err
	}
	if c.RabbitMQ.ManagementPort, err = envInt("RABBITMQ_MANAGEMENT_PORT", 15673); err != nil {
		return nil, err
	}

	c.JWT = JWT{
		Secret:        envStr("JWT_SECRET", "change-me"),
		ExpireSeconds: envIntDefault("JWT_EXPIRE_SECONDS", 86400),
	}

	c.LLM = LLM{
		APIKey:         envStr("LLM_API_KEY", ""),
		BaseURL:        envStr("LLM_BASE_URL", "https://api.deepseek.com"),
		ChatModel:      envStr("LLM_CHAT_MODEL", "deepseek-v4-flash"),
		TimeoutSeconds: envIntDefault("LLM_TIMEOUT_SECONDS", 30),
		MaxRetry:       envIntDefault("LLM_MAX_RETRY", 3),
	}

	c.Embedding = Embedding{
		APIKey:  envStr("LLM_EMBED_API_KEY", ""),
		BaseURL: envStr("LLM_EMBED_BASE_URL", ""),
		Model:   envStr("LLM_EMBEDDING_MODEL", "Qwen/Qwen3-VL-Embedding-8B"),
	}

	c.OSS = OSS{
		Region:          envStr("OSS_REGION", "cn-shenzhen"),
		Endpoint:        envStr("OSS_ENDPOINT", ""),
		AccessKeyID:     envStr("OSS_ACCESS_KEY_ID", ""),
		AccessKeySecret: envStr("OSS_ACCESS_KEY_SECRET", ""),
		Bucket:          envStr("OSS_BUCKET", "content-hub-file"),
	}

	c.Log = Log{
		Level: envStr("LOG_LEVEL", "info"),
		File:  envStr("LOG_FILE", ""),
	}

	c.Chunk = Chunk{
		Strategy: envStr("CHUNK_STRATEGY", "structured"),
		Size:     envIntDefault("CHUNK_SIZE", 300),
		Overlap:  envIntDefault("CHUNK_OVERLAP", 0),
	}

	c.Retrieval = Retrieval{TopK: envIntDefault("KBE_TOP_K", 20)}

	cfg = c
	return &c, nil
}

// Get 返回已加载的全局配置（Load 之后再调用）。
func Get() *Config { return &cfg }

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envIntDefault(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// envInt 与 envIntDefault 相同，但用于 load 时可能失败的字段。
func envInt(key string, def int) (int, error) {
	if v := os.Getenv(key); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return 0, fmt.Errorf("config: %s 必须是整数，实际=%q", key, v)
		}
		return n, nil
	}
	return def, nil
}

// loadEnvFile 加载项目根目录的 .env（优先），回退到当前工作目录的 .env。
// 通过 config.go 自身位置向上查找 go.mod，得到项目根，避免依赖 cwd。
func loadEnvFile() {
	root := projectRoot()
	if root != "" {
		envPath := filepath.Join(root, ".env")
		if _, err := os.Stat(envPath); err == nil {
			_ = godotenv.Load(envPath)
			return
		}
	}
	// 回退：cwd 下的 .env
	_ = godotenv.Load()
}

// projectRoot 返回包含 go.mod 的项目根目录；找不到返回空串。
func projectRoot() string {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	dir := filepath.Dir(thisFile) // <root>/config
	// 向上最多 5 层找 go.mod
	for i := 0; i < 5; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}
