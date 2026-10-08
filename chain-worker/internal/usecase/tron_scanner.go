package usecase

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"

	"github.com/renegadik/crypto-payment-gateway/chain-worker/internal/domain"
)

const maxBlocksPerSync = 100

// TRONScanner сканирует solidified-блоки и атомарно сохраняет переводы с курсором.
type TRONScanner struct {
	chain domain.TRONChain
	store domain.TRONTransferStore
}

// NewTRONScanner собирает сканер финализированной цепочки TRON.
func NewTRONScanner(chain domain.TRONChain, store domain.TRONTransferStore) (*TRONScanner, error) {
	if chain == nil || store == nil {
		return nil, fmt.Errorf("TRON-сканеру нужны источник блоков и хранилище")
	}
	return &TRONScanner{chain: chain, store: store}, nil
}

// Initialize устанавливает стартовую высоту только при первом запуске индексатора.
func (s *TRONScanner) Initialize(ctx context.Context, startHeight uint64) error {
	return s.store.InitializeTRONCursor(ctx, startHeight)
}

// SyncOnce последовательно обрабатывает не более 100 новых финализированных блоков.
func (s *TRONScanner) SyncOnce(ctx context.Context) (int, error) {
	last, err := s.store.LastTRONBlock(ctx)
	if err != nil {
		return 0, fmt.Errorf("прочитать курсор TRON: %w", err)
	}
	tip, err := s.chain.SolidifiedHeight(ctx)
	if err != nil {
		return 0, fmt.Errorf("получить финализированную высоту TRON: %w", err)
	}
	if last >= tip {
		return 0, nil
	}
	count := 0
	for number := last + 1; number <= tip && count < maxBlocksPerSync; number++ {
		if err := ctx.Err(); err != nil {
			return count, err
		}
		block, err := s.chain.SolidifiedBlock(ctx, number)
		if err != nil {
			return count, fmt.Errorf("загрузить финализированный блок %d: %w", number, err)
		}
		if block.Number != number || !validBlockID(block.ID, number) {
			return count, fmt.Errorf("TRON-узел вернул несогласованный блок %d", number)
		}
		transfers, err := s.chain.ConfirmedTransfers(ctx, block)
		if err != nil {
			return count, fmt.Errorf("разобрать переводы блока %d: %w", number, err)
		}
		for _, transfer := range transfers {
			if transfer.BlockNumber != number || !validTransfer(transfer) {
				return count, fmt.Errorf("в блоке %d обнаружены некорректные данные перевода", number)
			}
		}
		if err := s.store.CommitTRONBlock(ctx, number, block.ID, transfers); err != nil {
			return count, fmt.Errorf("сохранить блок %d: %w", number, err)
		}
		count++
	}
	return count, nil
}

// validBlockID проверяет hex-формат идентификатора и номер, закодированный в его префиксе.
func validBlockID(value string, number uint64) bool {
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != 32 {
		return false
	}
	var got uint64
	for _, b := range decoded[:8] {
		got = got<<8 | uint64(b)
	}
	return got == number
}

// validTransfer защищает хранилище от некорректных значений сетевого адаптера.
func validTransfer(value domain.TRONTransfer) bool {
	if len(value.TransactionID) != 64 || value.EventIndex < 0 || value.From == "" || value.To == "" {
		return false
	}
	if _, err := hex.DecodeString(value.TransactionID); err != nil {
		return false
	}
	amount, ok := new(big.Int).SetString(value.Amount, 10)
	return ok && amount.Sign() > 0
}
