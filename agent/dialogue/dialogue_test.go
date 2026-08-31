package dialogue

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/WWaynee/content-hub/config"
	"github.com/WWaynee/content-hub/llmclient"
)

// stubClient 返回固定响应的 llmclient.Client，用于隔离测试机检逻辑。
type stubClient struct {
	jsonResponse string
}

func (s *stubClient) Embed(ctx context.Context, input string) ([]float32, error) {
	return nil, nil
}
func (s *stubClient) EmbedBatch(ctx context.Context, inputs []string) ([][]float32, error) {
	return nil, nil
}
func (s *stubClient) Chat(ctx context.Context, messages []llmclient.ChatMessage) (string, error) {
	return s.jsonResponse, nil
}
func (s *stubClient) ChatWithJSON(ctx context.Context, messages []llmclient.ChatMessage, target interface{}) error {
	// 直接把预置 JSON 解到 target（模拟 LLM 返回）
	return json.Unmarshal([]byte(s.jsonResponse), target)
}

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

// TestParse_RejectsIllegalPlan 用 stub 返回非法计划（禁止字段），验证 Parse 内的机检拦截。
func TestParse_RejectsIllegalPlan(t *testing.T) {
	// stub 返回"试图修改字数（核心禁止字段）"的非法计划
	a := New(&stubClient{jsonResponse: `{"actions":[{"tool":"update_requirement_field","field":"word_count","field_value":"600"}]}`})
	_, err := a.Parse(context.Background(), "把字数改600", "需求单界面")
	if err == nil {
		t.Fatal("非法计划（改 word_count）应被机检拒绝")
	}
	// 合法计划应通过
	a2 := New(&stubClient{jsonResponse: `{"actions":[{"tool":"update_requirement_field","field":"style_tone","field_value":"正式"}]}`})
	if _, err := a2.Parse(context.Background(), "改基调", "需求单界面"); err != nil {
		t.Errorf("合法计划应通过，实际=%v", err)
	}
}
