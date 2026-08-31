//go:build integration

package dialogue

import (
	"context"
	"testing"

	"github.com/WWaynee/content-hub/config"
	"github.com/WWaynee/content-hub/llmclient"
)

// TestParse_CompositeInstruction 真实 LLM：复合指令应拆解为多动作计划。
func TestParse_CompositeInstruction(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("配置加载失败，跳过: %v", err)
	}
	if cfg.LLM.APIKey == "" {
		t.Skip("LLM 未配置 key，跳过")
	}
	a := New(llmclient.NewClient())

	msg := "把写作基调改正式点，然后字数要多加300字放在最后一段，需要多叙述下昨天的天气情况"
	plan, err := a.Parse(context.Background(), msg, "当前在稿件界面")
	if err != nil {
		t.Fatalf("Parse 失败: %v", err)
	}
	if len(plan.Actions) == 0 {
		t.Fatal("应拆解出至少 1 个动作")
	}
	t.Logf("拆解出 %d 个动作:", len(plan.Actions))
	for i, ac := range plan.Actions {
		t.Logf("  [%d] tool=%s field=%s query=%s", i, ac.Tool, ac.Field, ac.RetrievalQuery)
	}
	hasUpdate := false
	hasRetrieval := false
	for _, ac := range plan.Actions {
		if ac.Tool == "update_requirement_field" && ac.Field == "style_tone" {
			hasUpdate = true
		}
		if ac.Tool == "request_retrieval" {
			hasRetrieval = true
		}
	}
	if !hasUpdate {
		t.Errorf("应包含改基调动作，实际 actions=%+v", plan.Actions)
	}
	if !hasRetrieval {
		t.Errorf("应包含请求检索天气动作，实际 actions=%+v", plan.Actions)
	}
}
