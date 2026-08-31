package dialogue

import (
	"context"
	"encoding/json"
	"testing"

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
	return json.Unmarshal([]byte(s.jsonResponse), target)
}

// TestParse_RejectsIllegalPlan 用 stub 返回非法计划（禁止字段），验证 Parse 内的机检拦截。
func TestParse_RejectsIllegalPlan(t *testing.T) {
	a := New(&stubClient{jsonResponse: `{"actions":[{"tool":"update_requirement_field","field":"word_count","field_value":"600"}]}`})
	if _, err := a.Parse(context.Background(), "把字数改600", "需求单界面"); err == nil {
		t.Fatal("非法计划（改 word_count）应被机检拒绝")
	}
	a2 := New(&stubClient{jsonResponse: `{"actions":[{"tool":"update_requirement_field","field":"style_tone","field_value":"正式"}]}`})
	if _, err := a2.Parse(context.Background(), "改基调", "需求单界面"); err != nil {
		t.Errorf("合法计划应通过，实际=%v", err)
	}
}
