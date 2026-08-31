package observability

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Prometheus 指标（云原生监控）。
// 标签必须低基数（用 method/path/status_code/model 等枚举，绝不放 user_id/trace_id 防基数爆炸）。

// 1. HTTP 指标
var HTTPRequestsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{Name: "http_requests_total", Help: "HTTP 请求总数。"},
	[]string{"method", "path", "status_code"},
)

var HTTPRequestDuration = promauto.NewHistogramVec(
	prometheus.HistogramOpts{Name: "http_request_duration_seconds", Help: "HTTP 请求耗时（秒）。"},
	[]string{"method", "path", "status_code"},
)

// 2. LLM 指标
var LLMCallsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{Name: "llm_calls_total", Help: "LLM 调用总数。"},
	[]string{"model", "success"},
)

var LLMTokensTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{Name: "llm_tokens_total", Help: "LLM token 消耗总数。"},
	[]string{"model"},
)

// 3. MQ 指标
var MQMessagesTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{Name: "mq_messages_total", Help: "MQ 消息处理数。"},
	[]string{"queue", "status"},
)

func successLabel(ok bool) string {
	if ok {
		return "true"
	}
	return "false"
}

// IncHTTPRequest 记录一次 HTTP 请求。
func IncHTTPRequest(method, path, statusCode string, durationSeconds float64) {
	HTTPRequestsTotal.With(prometheus.Labels{"method": method, "path": path, "status_code": statusCode}).Inc()
	HTTPRequestDuration.With(prometheus.Labels{"method": method, "path": path, "status_code": statusCode}).Observe(durationSeconds)
}

// IncLLMCall 记录一次 LLM 调用。
func IncLLMCall(model string, success bool) {
	LLMCallsTotal.With(prometheus.Labels{"model": model, "success": successLabel(success)}).Inc()
}

// AddLLMTokens 累计 LLM token 消耗。
func AddLLMTokens(model string, tokenCount float64) {
	LLMTokensTotal.With(prometheus.Labels{"model": model}).Add(tokenCount)
}

// IncMQMessage 记录一次 MQ 消息处理。
func IncMQMessage(queue, status string) {
	MQMessagesTotal.With(prometheus.Labels{"queue": queue, "status": status}).Inc()
}

// MetricsHandler 返回 /metrics 的 HTTP handler。
func MetricsHandler() http.Handler {
	return promhttp.Handler()
}
