// Package kafka предоставляет безопасные базовые настройки клиентов Kafka.
package kafka

import (
	"time"

	"github.com/segmentio/kafka-go"
)

// NewWriter создаёт синхронный producer; при остановке сервиса его нужно закрыть.
func NewWriter(brokers []string, topic string) *kafka.Writer {
	return &kafka.Writer{
		Addr: kafka.TCP(brokers...), Topic: topic, Balancer: &kafka.Hash{},
		RequiredAcks: kafka.RequireAll, Async: false,
		WriteTimeout: 10 * time.Second, ReadTimeout: 10 * time.Second,
		AllowAutoTopicCreation: false,
	}
}

// NewReader создаёт consumer с ручным CommitMessages после успешной обработки.
// Доставка выполняется как минимум один раз, поэтому обработчики обязаны быть
// идемпотентными.
func NewReader(brokers []string, topic, groupID string) *kafka.Reader {
	return kafka.NewReader(kafka.ReaderConfig{
		Brokers: brokers, Topic: topic, GroupID: groupID,
		MinBytes: 1, MaxBytes: 10 * 1024 * 1024, MaxWait: time.Second,
		CommitInterval: 0,
	})
}
