// Package postgres реализует хранилище курсора и подтверждённых переводов TRON.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/renegadik/crypto-payment-gateway/chain-worker/internal/domain"
	"github.com/renegadik/crypto-payment-gateway/chain-worker/internal/repository/postgres/db"
)

// TRONStore сохраняет финализированные блоки и переводы без повторного учёта.
type TRONStore struct {
	pool *pgxpool.Pool
}

var _ domain.TRONTransferStore = (*TRONStore)(nil)

// NewTRONStore создаёт PostgreSQL-пул для индексатора TRON.
func NewTRONStore(ctx context.Context, databaseURL string) (*TRONStore, error) {
	if databaseURL == "" {
		return nil, fmt.Errorf("CHAIN_DATABASE_URL не задан")
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("создать пул PostgreSQL chain-worker: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("подключиться к PostgreSQL chain-worker: %w", err)
	}
	return &TRONStore{pool: pool}, nil
}

// Close освобождает соединения PostgreSQL.
func (s *TRONStore) Close() { s.pool.Close() }

// Check проверяет доступность БД и предварительное применение схемы индексатора.
func (s *TRONStore) Check(ctx context.Context) error {
	_, err := db.New(s.pool).GetTRONScanHeight(ctx)
	if err != nil {
		return fmt.Errorf("проверить схему индексатора TRON: %w", err)
	}
	return nil
}

// InitializeTRONCursor задаёт стартовый последний обработанный блок один раз.
func (s *TRONStore) InitializeTRONCursor(ctx context.Context, height uint64) error {
	if height > math.MaxInt64 {
		return fmt.Errorf("стартовая высота TRON превышает BIGINT")
	}
	if _, err := db.New(s.pool).CreateTRONScanState(ctx, int64(height)); err != nil {
		return fmt.Errorf("инициализировать курсор TRON: %w", err)
	}
	return nil
}

// LastTRONBlock возвращает номер последнего атомарно сохранённого блока.
func (s *TRONStore) LastTRONBlock(ctx context.Context) (uint64, error) {
	height, err := db.New(s.pool).GetTRONScanHeight(ctx)
	if err != nil {
		return 0, err
	}
	if height < 0 {
		return 0, fmt.Errorf("в БД находится отрицательная высота TRON")
	}
	return uint64(height), nil
}

// CommitTRONBlock одной транзакцией записывает блок, переводы и новый курсор.
func (s *TRONStore) CommitTRONBlock(ctx context.Context, number uint64, blockID string, transfers []domain.TRONTransfer) error {
	if number == 0 || number > math.MaxInt64 {
		return fmt.Errorf("высота блока TRON вне допустимого диапазона")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("начать транзакцию индексации TRON: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	last, err := queries.GetTRONScanHeight(ctx)
	if err != nil {
		return fmt.Errorf("прочитать курсор TRON: %w", err)
	}
	if uint64(last) >= number {
		return tx.Commit(ctx)
	}
	if last != int64(number)-1 {
		return fmt.Errorf("нарушена последовательность блоков TRON: курсор %d, блок %d", last, number)
	}
	inserted, err := queries.InsertTRONBlock(ctx, db.InsertTRONBlockParams{BlockNumber: int64(number), BlockID: blockID})
	if err != nil {
		return fmt.Errorf("сохранить блок TRON: %w", err)
	}
	if inserted == 0 {
		storedID, loadErr := queries.GetTRONBlockID(ctx, int64(number))
		if loadErr != nil || storedID != blockID {
			return fmt.Errorf("конфликт идентификатора блока TRON %d", number)
		}
	}
	for _, transfer := range transfers {
		amount, ok := new(big.Int).SetString(transfer.Amount, 10)
		if !ok || amount.Sign() <= 0 {
			return fmt.Errorf("некорректная сумма перевода TRON")
		}
		asset := transfer.Asset
		if asset == "" {
			asset = "TRX"
		}
		if _, err := queries.InsertTRONTransfer(ctx, db.InsertTRONTransferParams{
			TransactionID: transfer.TransactionID, EventIndex: transfer.EventIndex,
			BlockNumber: int64(number), FromAddress: transfer.From, ToAddress: transfer.To,
			Asset: asset, Amount: pgtype.Numeric{Int: amount, Valid: true},
		}); err != nil {
			return fmt.Errorf("сохранить перевод TRON: %w", err)
		}
	}
	advanced, err := queries.AdvanceTRONScanHeight(ctx, db.AdvanceTRONScanHeightParams{
		LastBlockNumber: int64(number), LastBlockNumber_2: last,
	})
	if err != nil {
		return fmt.Errorf("обновить курсор TRON: %w", err)
	}
	if advanced != 1 {
		return errors.New("курсор TRON был конкурентно изменён")
	}
	return tx.Commit(ctx)
}
