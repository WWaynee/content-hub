//go:build integration

package mq

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/WWaynee/content-hub/config"
)

// TestMQPublishConsume 验证 RabbitMQ 发布/消费往返（用独立临时队列）。
func TestMQPublishConsume(t *testing.T) {
	_, err := config.Load()
	if err != nil {
		t.Skipf("配置加载失败: %v", err)
	}
	if err := InitRabbitMQ(); err != nil {
		t.Skipf("RabbitMQ 不可用: %v", err)
	}

	const q = "content_test_mq"
	// 声明临时队列
	if _, err := ch.QueueDeclare(q, false, false, false, false, nil); err != nil {
		t.Fatalf("声明队列失败: %v", err)
	}

	msg := DocumentParseMsg{MsgID: "test-1", TenantID: 123, FileID: 456, VersionID: 789}
	body, _ := json.Marshal(msg)
	if err := Publish(q, body); err != nil {
		t.Fatalf("Publish 失败: %v", err)
	}

	received := make(chan DocumentParseMsg, 1)
	go func() {
		_ = Consume(q, func(b []byte) error {
			var got DocumentParseMsg
			json.Unmarshal(b, &got)
			received <- got
			return nil
		})
	}()

	select {
	case got := <-received:
		if got.TenantID != 123 || got.FileID != 456 || got.VersionID != 789 {
			t.Fatalf("消息内容不符: %+v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("3 秒内未收到消息")
	}

	// 清理队列
	_, _ = ch.QueueDelete(q, false, false, false)
}
