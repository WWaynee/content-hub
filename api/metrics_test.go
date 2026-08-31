package api

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// 验证 /metrics 端点暴露 Prometheus 指标（HTTP 请求指标在打点后出现）。
func TestMetricsEndpoint(t *testing.T) {
	r := NewRouter()

	// 先打一个 /health 请求，触发 http_requests_total 的实例
	hw := httptest.NewRecorder()
	hreq := httptest.NewRequest("GET", "/health", nil)
	r.ServeHTTP(hw, hreq)
	if hw.Code != 200 {
		t.Fatalf("/health 应返回 200，实际 %d", hw.Code)
	}

	// 再请求 /metrics
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("/metrics 应返回 200，实际 %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "http_requests_total") {
		t.Errorf("/metrics 应包含 http_requests_total 指标，实际缺失")
	}
	// llm_calls_total 是 CounterVec，需真实 LLM 调用才有实例；此处只验证 http 指标已打点
	if !strings.Contains(body, "http_request_duration_seconds") {
		t.Errorf("/metrics 应包含 http_request_duration_seconds 指标")
	}
}
