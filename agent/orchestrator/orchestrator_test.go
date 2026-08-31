//go:build integration

package orchestrator

import (
	"context"
	"testing"

	"github.com/WWaynee/content-hub/agent"
	"github.com/WWaynee/content-hub/agent/evidence"
	"github.com/WWaynee/content-hub/agent/retrieve"
	"github.com/WWaynee/content-hub/agent/writing"
	"github.com/WWaynee/content-hub/api/service"
	"github.com/WWaynee/content-hub/config"
	"github.com/WWaynee/content-hub/llmclient"
	"github.com/WWaynee/content-hub/storage"
)

// TestOrchestratorGenerate 真实端到端：上传文档 → generation（检索→撰写→证据）。
// 依赖真实 LLM（DeepSeek）+ embedding（硅基流动）+ OSS + Qdrant + MySQL。
func TestOrchestratorGenerate(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("配置加载失败: %v", err)
	}
	if cfg.LLM.APIKey == "" || cfg.Embedding.APIKey == "" || cfg.OSS.AccessKeyID == "" {
		t.Skip("LLM/Embedding/OSS 未配置真实 key，跳过")
	}
	if storage.OSSClient == nil {
		_ = storage.InitOSS()
	}
	if storage.QdrantClient == nil {
		_ = storage.InitQdrant(4096)
	}
	_, _ = storage.InitMySQL(&cfg.MySQL)

	ctx := context.Background()
	tenantID := uint64(99990002) // 独立测试租户

	// 1. 先上传一份资料
	content := `# 招生简章

## 报名条件
本年度面向应届高中毕业生，年龄不超过 25 周岁。

## 录取规则
按高考总成绩从高到低依次录取，同等分数依次比较语文、数学成绩。`
	_, err = service.IngestAndParse(ctx, service.IngestParams{
		TenantID:    tenantID,
		Scope:       storage.ScopePrivate,
		OwnerUserID: 1,
		DirID:       0,
		FileName:    "招生简章.md",
		Content:     []byte(content),
	})
	if err != nil {
		t.Fatalf("IngestDocument 失败: %v", err)
	}

	// 2. 组装 agent
	llm := llmclient.NewClient()
	o := New(retrieve.New(llm), writing.New(llm), evidence.New())
	req := agent.Requirement{
		Title:              "2026 年招生简章发布",
		StyleSubject:       "学校",
		StylePurpose:        "向社会公布招生政策",
		StyleAudience:       "应届高中毕业生及家长",
		ChapterRequirement:  "包含报名条件、录取规则两部分",
		WordCount:           300,
	}

	// 3. 执行 generation
	res, err := o.Generate(ctx, tenantID, req, nil)
	if err != nil {
		t.Fatalf("Generate 失败: %v", err)
	}
	if res.Article == nil || len(res.Article.Sections) == 0 {
		t.Fatal("应生成有章节的稿件")
	}
	t.Logf("检索 query 数=%d, 证据数=%d, 稿件章节数=%d, 证据清单条目=%d",
		len(res.Queries), len(res.Evidence), len(res.Article.Sections), len(res.Manifest.Entries))

	// 4. 基本断言
	if len(res.Queries) == 0 {
		t.Fatal("应提炼出至少 1 个检索 query")
	}
	if len(res.Evidence) == 0 {
		t.Fatal("应检回到至少 1 条证据")
	}
	if len(res.Article.Sections) == 0 {
		t.Fatal("稿件应有章节")
	}

	// 清理
	cleanupOrch(ctx, tenantID)
}

func cleanupOrch(ctx context.Context, tenantID uint64) {
	db := storage.GetDB()
	db.Exec("DELETE FROM doc_sentences WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM doc_chunks WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM doc_versions WHERE tenant_id = ?", tenantID)
	db.Exec("DELETE FROM kbase_files WHERE tenant_id = ?", tenantID)
}
