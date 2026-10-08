package domain

import "context"

// TRONTransfer описывает подтверждённый перевод нативной монеты или токена.
type TRONTransfer struct {
	TransactionID string
	EventIndex    int32
	BlockNumber   uint64
	From          string
	To            string
	Asset         string
	Amount        string
}

// TRONBlock содержит метаданные и исходный ответ узла для одного solidified-блока.
type TRONBlock struct {
	Number  uint64
	ID      string
	Payload []byte
}

// TRONChain предоставляет чтение только из финализированной цепочки.
type TRONChain interface {
	// SolidifiedHeight возвращает высоту последнего финализированного блока.
	SolidifiedHeight(context.Context) (uint64, error)
	// SolidifiedBlock возвращает блок по высоте из финализированной цепочки.
	SolidifiedBlock(context.Context, uint64) (TRONBlock, error)
	// ConfirmedTransfers разбирает успешные переводы из финализированного блока.
	ConfirmedTransfers(context.Context, TRONBlock) ([]TRONTransfer, error)
}

// TRONTransferStore сохраняет блоки и переводы атомарно с курсором сканирования.
type TRONTransferStore interface {
	// InitializeTRONCursor создаёт начальный курсор, не меняя уже существующий.
	InitializeTRONCursor(context.Context, uint64) error
	// LastTRONBlock возвращает высоту последнего атомарно обработанного блока.
	LastTRONBlock(context.Context) (uint64, error)
	// CommitTRONBlock идемпотентно записывает блок и все его переводы.
	CommitTRONBlock(context.Context, uint64, string, []TRONTransfer) error
}
