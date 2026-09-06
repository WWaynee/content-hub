package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

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
	RateLimit  RateLimit
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
	// MinScore 检索相似度阈值：低于该分数的命中判为「不相关」，直接丢弃。
	// 用于防止把与主题无关的文本（如低相关片段）当作证据返回，进而避免 AI 编造数据。
	MinScore float32

	// ---- P14 检索质量升级（默认全关 = 保持纯向量检索现状，主流程零风险）----
	// Strategy 检索排序策略：dense（默认，纯向量）| hybrid（BM25 稀疏 + Dense，RRF 融合）| hybrid_rerank（再叠加 Rerank 精排）。
	Strategy string
	// HybridK 混合检索候选池大小（先向量召回 top HybridK，再在池内算 BM25 + RRF）。
	HybridK int
	// RRFK RRF（Reciprocal Rank Fusion）的 k 参数。
	RRFK int
	// BM25K1 / BM25B BM25 的 k1 / b 参数。
	BM25K1 float64
	BM25B  float64
	// RerankTopN 重排序只精排前 N 个候选（平衡效果与成本）。
	RerankTopN int
	// RerankModel 重排序模型名。
	RerankModel string
	// RerankBaseURL / RerankAPIKey 重排序服务地址与密钥；为空时回落 Embedding 段（默认同为硅基流动）。
	RerankBaseURL string
	RerankAPIKey  string
	// QueryExpandEnable 是否在 Guardian 首轮检索前用 LLM 把 claim 扩展为多个 query（默认关）。
	QueryExpandEnable bool
	// QueryExpandCount 扩展 query 数量（保守默认 3，控制 token 成本）。
	QueryExpandCount int
}

type RateLimit struct {
	TenantPerMin int
	UserPerMin   int
	NotePerMin   int
	WindowSec    int
	KeyTTLSec    int
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
		TimeoutSeconds: envIntDefault("LLM_TIMEOUT_SECONDS", 120),
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
		Bucket:          envStr("OSS_BUCKET", "my-content-hub"),
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

	c.Retrieval = Retrieval{
		TopK:     envIntDefault("KBE_TOP_K", 20),
		MinScore: float32(envFloat64Default("KBE_MIN_SCORE", 0.6)),
		// P14：默认全部关闭，Strategy=dense 即纯向量现状
		Strategy:           envStr("RETRIEVAL_STRATEGY", "dense"),
		HybridK:            envIntDefault("RETRIEVAL_HYBRID_K", 50),
		RRFK:               envIntDefault("RETRIEVAL_RRF_K", 60),
		BM25K1:             envFloat64Default("RETRIEVAL_BM25_K1", 1.2),
		BM25B:              envFloat64Default("RETRIEVAL_BM25_B", 0.75),
		RerankTopN:         envIntDefault("RETRIEVAL_RERANK_TOP_N", 20),
		RerankModel:        envStr("RETRIEVAL_RERANK_MODEL", "BAAI/bge-reranker-v2-m3"),
		RerankBaseURL:      envStr("RETRIEVAL_RERANK_BASE_URL", ""),
		RerankAPIKey:       envStr("RETRIEVAL_RERANK_API_KEY", ""),
		QueryExpandEnable:  envBoolDefault("RETRIEVAL_QUERY_EXPAND", false),
		QueryExpandCount:   envIntDefault("RETRIEVAL_QUERY_EXPAND_COUNT", 3),
	}

	c.RateLimit = RateLimit{
		TenantPerMin: envIntDefault("RL_TENANT_PER_MIN", 120),
		UserPerMin:   envIntDefault("RL_USER_PER_MIN", 60),
		NotePerMin:   envIntDefault("RL_NOTE_PER_MIN", 20),
		WindowSec:    envIntDefault("RL_WINDOW_SEC", 60),
		KeyTTLSec:    envIntDefault("RL_KEY_TTL_SEC", 120),
	}

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

// envFloat64Default 读取 float64 配置，解析失败回退默认值。
func envFloat64Default(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			return n
		}
	}
	return def
}

// envBoolDefault 读取 bool 配置（1/true/yes 为真），其余（含空）回退默认值。
func envBoolDefault(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		switch strings.ToLower(v) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
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
