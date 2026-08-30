package llmclient

import (
	"errors"
	"sync"
	"time"
)

// ErrCircuitOpen 熔断打开时的哨兵错误。
var ErrCircuitOpen = errors.New("熔断器已打开：LLM 服务疑似不可用")

// CircuitBreakerState 熔断器状态。
type CircuitBreakerState int

const (
	StateClosed CircuitBreakerState = iota
	StateOpen
	StateHalfOpen
)

// CircuitBreakerConfig 熔断器配置。
type CircuitBreakerConfig struct {
	FailureThreshold float64       // 失败率阈值（0~1）
	MinRequests      int           // 触发评估的最少请求数
	Window           time.Duration // 统计窗口
	OpenTimeout      time.Duration // 熔断持续时间
}

// CircuitBreaker 三态熔断器（Closed → Open → HalfOpen → Closed）。
type CircuitBreaker struct {
	failureThreshold float64
	minRequests      int
	window           time.Duration
	openTimeout      time.Duration

	mu            sync.Mutex
	state         CircuitBreakerState
	requestCount  int
	failureCount  int
	windowStart   time.Time
	openSince     time.Time
	halfProbeSent bool
}

// NewCircuitBreaker 构造熔断器（未配置字段用默认值）。
func NewCircuitBreaker(cfg CircuitBreakerConfig) *CircuitBreaker {
	cb := &CircuitBreaker{state: StateClosed}
	if cfg.FailureThreshold > 0 {
		cb.failureThreshold = cfg.FailureThreshold
	} else {
		cb.failureThreshold = 0.5
	}
	if cfg.MinRequests > 0 {
		cb.minRequests = cfg.MinRequests
	} else {
		cb.minRequests = 5
	}
	if cfg.Window > 0 {
		cb.window = cfg.Window
	} else {
		cb.window = 10 * time.Second
	}
	if cfg.OpenTimeout > 0 {
		cb.openTimeout = cfg.OpenTimeout
	} else {
		cb.openTimeout = 30 * time.Second
	}
	cb.windowStart = time.Now()
	return cb
}

// allow 请求前判定是否放行。
func (cb *CircuitBreaker) allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.advance()
	switch cb.state {
	case StateClosed:
		return true
	case StateHalfOpen:
		if !cb.halfProbeSent {
			cb.halfProbeSent = true
			return true
		}
		return false
	default:
		return false
	}
}

// record 请求完成后记录结果。
func (cb *CircuitBreaker) record(success bool) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.advance()
	switch cb.state {
	case StateHalfOpen:
		if success {
			cb.state = StateClosed
			cb.reset(time.Now())
		} else {
			cb.state = StateOpen
			cb.openSince = time.Now()
		}
	case StateClosed:
		cb.requestCount++
		if !success {
			cb.failureCount++
		}
		if cb.requestCount >= cb.minRequests &&
			float64(cb.failureCount)/float64(cb.requestCount) > cb.failureThreshold {
			cb.state = StateOpen
			cb.openSince = time.Now()
		}
	}
}

func (cb *CircuitBreaker) advance() {
	now := time.Now()
	switch cb.state {
	case StateClosed:
		if now.Sub(cb.windowStart) > cb.window {
			cb.reset(now)
		}
	case StateOpen:
		if now.Sub(cb.openSince) >= cb.openTimeout {
			cb.state = StateHalfOpen
			cb.halfProbeSent = false
		}
	}
}

func (cb *CircuitBreaker) reset(now time.Time) {
	cb.requestCount = 0
	cb.failureCount = 0
	cb.windowStart = now
}
