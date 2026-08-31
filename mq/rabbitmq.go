// Package mq 提供 RabbitMQ 客户端封装，用于文档解析异步任务。
package mq

import (
	"context"
	"fmt"
	"sync"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/WWaynee/content-hub/config"
)

var (
	conn *amqp.Connection
	ch   *amqp.Channel
	mu   sync.Mutex
)

// InitRabbitMQ 初始化连接 + Channel + 声明队列。
func InitRabbitMQ() error {
	mu.Lock()
	defer mu.Unlock()
	cfg := config.Get().RabbitMQ

	dsn := fmt.Sprintf("amqp://%s:%s@%s:%d/", cfg.User, cfg.Password, cfg.Host, cfg.Port)
	c, err := amqp.Dial(dsn)
	if err != nil {
		return fmt.Errorf("连接 RabbitMQ 失败: %w", err)
	}
	c2, err := c.Channel()
	if err != nil {
		c.Close()
		return fmt.Errorf("创建 Channel 失败: %w", err)
	}
	for _, q := range []string{cfg.QueueDocumentParse, cfg.QueueArticleGenerate} {
		if _, err := c2.QueueDeclare(q, true, false, false, false, nil); err != nil {
			c2.Close()
			c.Close()
			return fmt.Errorf("声明队列 %s 失败: %w", q, err)
		}
	}
	conn = c
	ch = c2
	return nil
}

// Publish 向队列发布一条消息（持久化）。
func Publish(queue string, body []byte) error {
	if ch == nil {
		return fmt.Errorf("RabbitMQ 未初始化")
	}
	return ch.PublishWithContext(context.Background(), "", queue, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         body,
	})
}

// IsReady 判断 RabbitMQ 连接是否已初始化可用。
func IsReady() bool {
	return conn != nil && !conn.IsClosed()
}

// Consume 消费队列，对每条消息调用 handler；handler 返回 nil 则 ACK，否则 Nack(requeue)。
func Consume(queue string, handler func(body []byte) error) error {
	if ch == nil {
		return fmt.Errorf("RabbitMQ 未初始化")
	}
	msgs, err := ch.Consume(queue, "", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("消费队列失败: %w", err)
	}
	for m := range msgs {
		if err := handler(m.Body); err != nil {
			_ = m.Nack(false, true) // 失败重入队
		} else {
			_ = m.Ack(false)
		}
	}
	return nil
}
