package domain

import (
	"context"
	"errors"
	"time"
)

//go:generate mockgen -source=tron_invoice.go -destination=../usecase/tron_invoice_mock_test.go -package=usecase

var (
	ErrTRONInvoiceNotFound = errors.New("TRON-инвойс не найден")
	ErrInvalidTRONInvoice  = errors.New("некорректные параметры TRON-инвойса")
)

// TRONInvoice содержит публичные реквизиты инвойса и наблюдаемые финализированные платежи.
type TRONInvoice struct {
	ID             string
	Address        string
	Asset          string
	ExpectedAmount string
	ReceivedAmount string
	Status         string
	CreatedAt      time.Time
	Transfers      []TRONInvoiceTransfer
}

// TRONInvoiceTransfer описывает финализированный перевод, зачисленный на инвойс.
type TRONInvoiceTransfer struct {
	TransactionID string
	EventIndex    int32
	BlockNumber   int64
	From          string
	Amount        string
}

// TRONAddressProvider выводит публичный адрес по индексу HD-кошелька.
type TRONAddressProvider interface {
	// DeriveAddress возвращает адрес, не раскрывая приватные данные.
	DeriveAddress(context.Context, uint32) (string, error)
}

// TRONInvoiceRepository хранит инвойсы и читает связанные подтверждённые переводы.
type TRONInvoiceRepository interface {
	// FindByIdempotencyKey возвращает ранее созданный инвойс, если ключ уже использовался.
	FindByIdempotencyKey(context.Context, string) (TRONInvoice, error)
	// NextAddressIndex резервирует новый индекс детерминированного кошелька.
	NextAddressIndex(context.Context) (uint64, error)
	// Create сохраняет инвойс или возвращает результат конкурентного идемпотентного запроса.
	Create(context.Context, string, uint64, string, string) (TRONInvoice, error)
	// GetWithTransfers возвращает инвойс и только финализированные поступления.
	GetWithTransfers(context.Context, string) (TRONInvoice, error)
}
