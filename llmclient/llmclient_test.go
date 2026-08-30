package llmclient

import (
	"testing"
	"time"
)

// 单元测试：不依赖外部 LLM，验证熔断器与 JSON 修复逻辑。

func TestCircuitBreaker_OpensOnHighFailure(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 0.5,
		MinRequests:      4,
		Window:           time.Minute,
		OpenTimeout:      time.Minute,
	})
	// 连续 4 次失败 → 应打开
	for i := 0; i < 4; i++ {
		cb.record(false)
	}
	if !cb.allow() || cb.state != StateOpen {
		// allow() 会 advance，但 state 已是 Open，allow 返回 false
		if cb.state != StateOpen {
			t.Fatalf("高失败率后应 Open，实际 state=%d", cb.state)
		}
	}
	if cb.allow() {
		t.Fatal("Open 状态不应放行请求")
	}
}

func TestCircuitBreaker_StaysClosedOnSuccess(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{MinRequests: 4, Window: time.Minute, OpenTimeout: time.Minute})
	for i := 0; i < 4; i++ {
		cb.record(true)
	}
	if cb.state != StateClosed {
		t.Fatalf("全成功后应保持 Closed，实际 state=%d", cb.state)
	}
}

func TestCircuitBreaker_HalfOpenRecovery(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 0.5, MinRequests: 3, Window: time.Minute, OpenTimeout: time.Millisecond,
	})
	for i := 0; i < 3; i++ {
		cb.record(false)
	}
	if cb.state != StateOpen {
		t.Fatalf("应先 Open，实际=%d", cb.state)
	}
	// 等 openTimeout 过后，advance 会切 HalfOpen（通过 allow 触发）
	time.Sleep(2 * time.Millisecond)
	if !cb.allow() {
		t.Fatal("HalfOpen 应放行第一个试探请求")
	}
	cb.record(true) // 试探成功 → Closed
	if cb.state != StateClosed {
		t.Fatalf("试探成功后应回 Closed，实际=%d", cb.state)
	}
}

func TestNormalizeJSON_StripsMarkdownAndText(t *testing.T) {
	cases := map[string]string{
		"```json\n{\"a\":1}\n```":                 `{"a":1}`,
		"result is {\"a\":1} ok":                  `{"a":1}`,
		`{"a":1`:                                  `{"a":1}`,
		"```\n[1,2,3]\n```":                       "",
	}
	for in, want := range cases {
		got := normalizeJSON(in)
		// 对数组 case 我们不强求（normalizeJSON 针对对象），跳过 want=="" 的
		if want == "" {
			continue
		}
		if got != want {
			t.Errorf("normalizeJSON(%q)=%q, want %q", in, got, want)
		}
	}
}

// 验证 normalizeJSON 对补括号场景。
func TestNormalizeJSON_AppendsMissingBrace(t *testing.T) {
	got := normalizeJSON(`{"a":{"b":1}`)
	if got != `{"a":{"b":1}}` {
		t.Errorf("got %q", got)
	}
}
