package progress

import (
	"testing"
	"time"
)

// P13：Broker 按 run 广播、不因无人订阅/消费慢而阻塞 worker。
func TestBrokerBasic(t *testing.T) {
	b := NewBroker()
	ch := b.Subscribe(7)

	b.Emit(Event{RunID: 7, Type: EvStepBegin, StepNo: 1})
	b.Emit(Event{RunID: 7, Type: EvStepDone, StepNo: 1})

	select {
	case e := <-ch:
		if e.StepNo != 1 || e.Type != EvStepBegin {
			t.Fatalf("期望首个 begin，实得 %v/%v", e.Type, e.StepNo)
		}
	case <-time.After(time.Second):
		t.Fatal("未收到 begin 事件")
	}
	b.Unsubscribe(7, ch)
}

// 无人订阅 / 订阅被移除，也不 panic、不阻塞（worker 可安全 Emit）。
func TestBrokerNoSubscriberOrSlowDoesNotBlock(t *testing.T) {
	b := NewBroker()
	// 无人订阅
	b.Emit(Event{RunID: 1, Type: EvDetail, StepNo: 2, Payload: "x"})

	// 慢消费者：先补发未读 recent，再投递实时也不应阻塞 caller、且总量有界。
	for i := 0; i < 30; i++ {
		b.Emit(Event{RunID: 2, Type: EvDetail, StepNo: i})
	}
	slow := b.Subscribe(2) // 自动补发最近 Recent(=48)条里未消费的部分
	// 再发一批实时仍不阻塞
	for i := 0; i < 30; i++ {
		b.Emit(Event{RunID: 2, Type: EvStepDone, StepNo: i})
	}
	select {
	case e, ok := <-slow:
		if !ok {
			t.Fatal("通道被关闭")
		}
		_ = e
	case <-time.After(time.Second):
		t.Fatal("慢消费者连不上事件")
	}
	b.Unsubscribe(2, slow)
}

// 不同 run 互不串扰。
func TestBrokerIsolateRuns(t *testing.T) {
	b := NewBroker()
	chA := b.Subscribe(11)
	chB := b.Subscribe(22)
	b.Emit(Event{RunID: 11, Type: EvRunDone})
	select {
	case e := <-chA:
		if e.RunID != 11 || e.Type != EvRunDone {
			t.Fatalf("A 收错: run=%d type=%v", e.RunID, e.Type)
		}
	default:
		t.Fatal("A 未收到")
	}
	select {
	case <-chB:
		t.Fatal("B 不应收到 A 的 run 事件")
	default:
	}
	b.Unsubscribe(11, chA)
	b.Unsubscribe(22, chB)
}
