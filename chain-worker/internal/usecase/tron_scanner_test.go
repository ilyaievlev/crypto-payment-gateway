package usecase

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"

	"github.com/renegadik/crypto-payment-gateway/chain-worker/internal/domain"
	"github.com/stretchr/testify/require"
)

type tronChainStub struct {
	tip    uint64
	blocks []uint64
	failAt uint64
}

func (s *tronChainStub) SolidifiedHeight(context.Context) (uint64, error) { return s.tip, nil }

func (s *tronChainStub) SolidifiedBlock(_ context.Context, number uint64) (domain.TRONBlock, error) {
	if number == s.failAt {
		return domain.TRONBlock{}, errors.New("node unavailable")
	}
	s.blocks = append(s.blocks, number)
	blockID := make([]byte, 32)
	for i := 7; i >= 0; i-- {
		blockID[i] = byte(number)
		number >>= 8
	}
	return domain.TRONBlock{Number: s.blocks[len(s.blocks)-1], ID: hex.EncodeToString(blockID)}, nil
}

func (s *tronChainStub) ConfirmedTransfers(_ context.Context, block domain.TRONBlock) ([]domain.TRONTransfer, error) {
	return []domain.TRONTransfer{{
		TransactionID: fmt.Sprintf("%064x", block.Number), BlockNumber: block.Number,
		From: "from", To: "to", Amount: "100",
	}}, nil
}

type tronStoreStub struct {
	last      uint64
	start     uint64
	committed []uint64
	failAt    uint64
}

func (s *tronStoreStub) InitializeTRONCursor(_ context.Context, height uint64) error {
	s.start = height
	if s.last == 0 {
		s.last = height
	}
	return nil
}

func (s *tronStoreStub) LastTRONBlock(context.Context) (uint64, error) { return s.last, nil }

func (s *tronStoreStub) CommitTRONBlock(_ context.Context, number uint64, _ string, _ []domain.TRONTransfer) error {
	if number == s.failAt {
		return errors.New("database unavailable")
	}
	s.last = number
	s.committed = append(s.committed, number)
	return nil
}

// TestTRONScannerProcessesOnlyFinalizedBlocks проверяет catch-up и атомарную последовательность.
func TestTRONScannerProcessesOnlyFinalizedBlocks(t *testing.T) {
	chain := &tronChainStub{tip: 105}
	store := &tronStoreStub{}
	scanner, err := NewTRONScanner(chain, store)
	require.NoError(t, err)
	require.NoError(t, scanner.Initialize(context.Background(), 100))
	processed, err := scanner.SyncOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 5, processed)
	require.Equal(t, []uint64{101, 102, 103, 104, 105}, store.committed)
	processed, err = scanner.SyncOnce(context.Background())
	require.NoError(t, err)
	require.Zero(t, processed)
}

// TestTRONScannerStopsAtFirstFailure проверяет, что курсор не перескакивает проблемный блок.
func TestTRONScannerStopsAtFirstFailure(t *testing.T) {
	chain := &tronChainStub{tip: 3, failAt: 2}
	store := &tronStoreStub{last: 0}
	scanner, err := NewTRONScanner(chain, store)
	require.NoError(t, err)
	processed, err := scanner.SyncOnce(context.Background())
	require.Error(t, err)
	require.Equal(t, 1, processed)
	require.Equal(t, uint64(1), store.last)
}

// TestTRONScannerInitializesAtConfiguredHeight проверяет стартовую точку без сканирования истории.
func TestTRONScannerInitializesAtConfiguredHeight(t *testing.T) {
	chain := &tronChainStub{tip: 1000}
	store := &tronStoreStub{}
	scanner, err := NewTRONScanner(chain, store)
	require.NoError(t, err)
	require.NoError(t, scanner.Initialize(context.Background(), 999))
	processed, err := scanner.SyncOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Equal(t, uint64(1000), store.last)
}
