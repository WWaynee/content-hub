// Package progress 定义"生成进度事件"，在 handler 的后台生成作业与前端(经 SSE)之间传递
// "进行到第几步/这步在干嘛/发了什么、回了什么/完成还是失败"。
//
// 设计坐标（见 docs/rebuild/packages/P13）：
//   - 数据真源仍是 agent_runs/agent_steps（DB，可回放、可断线重建）；
//   - 本包只是"运行期事件类型 + 进程内按 run 的广播 Topic"，让 worker 落库的同时把
//     增量推给正连着的 SSE，也让前端能收到"正在做第几不卡死"。
//   - 它不 import agent/storage，仅依赖 model 的 AgentStep 视图，避免与 orchestrator 成环，
//     也不被 storage 反向依赖。
package progress

import (
	"sync"
)

// EventType 一步生命周期内可推给前端的动作。
type EventType string

const (
	// EvStepBegin 该步已开始（落库一条 done=false 的步，前端点亮"进行中"）。
	EvStepBegin EventType = "step_begin"
	// EvDetail 该步进行中补充的明细（如检索到第几条、命中哪几个文档）。可多条。
	EvDetail EventType = "step_detail"
	// EvStepDone 该步完成（done=true；带耗时/回执摘要）。
	EvStepDone EventType = "step_done"
	// EvStepFail 该步失败（done=true + failure 原因）。
	EvStepFail EventType = "step_fail"
	// EvRunDone 整个 run 成功产出（终端；SSE 收到后应收场）。
	EvRunDone EventType = "run_done"
	// EvRunFailed 整个 run 失败（终端）。
	EvRunFailed EventType = "run_failed"
)

// Step 用户视角的一步（与 agent_steps 对应列一一映射）。
type Step struct {
	No         int    `json:"no"`          // 1-based
	Total      int    `json:"total"`       // 全程总步数
	Role       string `json:"role"`        // 执行者
	Title      string `json:"title"`       // 人话标题（前端卡片名）
	Done       bool   `json:"done"`        // 已结束
	Failed     bool   `json:"failed"`      // 是否失败
	Failure    string `json:"failure"`     // 失败原因
	DurationMs int64  `json:"duration_ms"` // 耗时
	// Detail 本步"对 LLM/检索发什么/收什么"脱敏摘要（默认折叠展示）
	Detail string `json:"detail,omitempty"`
}

// Event 广播给某个 run 订阅者的一条事件。
type Event struct {
	// RunID 归属的 run（路由给订阅该 run 的 SSE）。
	RunID uint64 `json:"run_id"`
	Type  EventType `json:"type"`
	// StepNo 若非终端事件则该事件作用于第几号 step（>=1）；终端事件可为0。
	StepNo int `json:"step_no"`
	// Payload 具体载荷：
	//   - step_begin / step_done / step_fail: Step 精简（前端可直接覆写该卡状态）
	//   - step_detail: 一行 detail 文本
	//   - run_done/run_failed: 结束说明字符串
	Payload interface{} `json:"payload,omitempty"`
}

// Broker 进程内按 run 广播的 Topic。同一进程里 handler 的生成 worker(写方) 与 SSE(读方)
// 各自持有；跨进程分发不在本 P13 范围（P13b worker/MQ 演进时换实现）。
type Broker struct {
	mu   sync.Mutex
	subs map[uint64][]chan Event // runID -> 订阅 chan（容量>=1，防 worker 阻塞）
}

// NewBroker 构造。
func NewBroker() *Broker {
	return &Broker{subs: map[uint64][]chan Event{}}
}

// Subscribe 注册对某 run 的事件订阅，返回读通道。调用方应在 run 终态或客户端断开时 Unsubscribe。
func (b *Broker) Subscribe(runID uint64) <-chan Event {
	ch := make(chan Event, 32)
	b.mu.Lock()
	b.subs[runID] = append(b.subs[runID], ch)
	b.mu.Unlock()
	return ch
}

// Unsubscribe 移除某 run 的指定订阅。
func (b *Broker) Unsubscribe(runID uint64, ch <-chan Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	chs := b.subs[runID]
	for i, c := range chs {
		if c == ch {
			b.subs[runID] = append(chs[:i], chs[i+1:]...)
			break
		}
	}
	if len(b.subs[runID]) == 0 {
		delete(b.subs, runID)
	}
}

// Emit 向某 run 所有订阅者投递事件（非阻塞：chan 满则丢弃保 worker 不卡；丢包可由前端
// 用 DB 回放兜底补齐，P13 语义前提"DB 是事实源"）。
func (b *Broker) Emit(ev Event) {
	b.mu.Lock()
	chs := append([]chan Event(nil), b.subs[ev.RunID]...)
	b.mu.Unlock()
	for _, ch := range chs {
		select {
		case ch <- ev:
		default:
			// 丢弃：订阅端消费慢。前端仅丢"中间 detail"，不丢 done/终态；即便全丢也能 DB 回放。
		}
	}
}
