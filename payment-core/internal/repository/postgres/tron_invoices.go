package postgres

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/renegadik/crypto-payment-gateway/payment-core/internal/domain"
	"github.com/renegadik/crypto-payment-gateway/payment-core/internal/repository/postgres/db"
)

// TRONInvoices хранит HD-инвойсы и читает соответствующие записи индексатора.
type TRONInvoices struct {
	pool *pgxpool.Pool
}

var _ domain.TRONInvoiceRepository = (*TRONInvoices)(nil)

// NewTRONInvoices создаёт пул PostgreSQL для инвойсов.
func NewTRONInvoices(ctx context.Context, databaseURL string) (*TRONInvoices, error) {
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL не задан")
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("создать пул PostgreSQL payment-core: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("подключиться к PostgreSQL payment-core: %w", err)
	}
	return &TRONInvoices{pool: pool}, nil
}

// Close освобождает соединения PostgreSQL.
func (r *TRONInvoices) Close() { r.pool.Close() }

// Check проверяет доступность таблиц инвойсов и финализированных переводов.
func (r *TRONInvoices) Check(ctx context.Context) error {
	_, err := db.New(r.pool).Ping(ctx)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `SELECT 1 FROM payment_core.tron_invoices i LEFT JOIN chain_worker.tron_transfers t ON FALSE LIMIT 0`)
	if err != nil {
		return fmt.Errorf("проверить схему TRON-инвойсов: %w", err)
	}
	return nil
}

// FindByIdempotencyKey находит ранее созданный инвойс для повторного запроса.
func (r *TRONInvoices) FindByIdempotencyKey(ctx context.Context, key string) (domain.TRONInvoice, error) {
	if key == "" {
		return domain.TRONInvoice{}, domain.ErrTRONInvoiceNotFound
	}
	row, err := db.New(r.pool).FindTRONInvoiceByIdempotencyKey(ctx, pgtype.Text{String: key, Valid: true})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.TRONInvoice{}, domain.ErrTRONInvoiceNotFound
	}
	if err != nil {
		return domain.TRONInvoice{}, err
	}
	return domain.TRONInvoice{ID: row.ID, Address: row.Address, Asset: row.Asset, ExpectedAmount: row.ExpectedAmount, CreatedAt: row.CreatedAt.Time}, nil
}

// NextAddressIndex атомарно резервирует следующий индекс HD-пути.
func (r *TRONInvoices) NextAddressIndex(ctx context.Context) (uint64, error) {
	index, err := db.New(r.pool).GetNextTRONAddressIndex(ctx)
	if err != nil {
		return 0, err
	}
	if index < 0 {
		return 0, fmt.Errorf("последовательность HD-адресов вернула отрицательный индекс")
	}
	return uint64(index), nil
}

// Create сохраняет инвойс и при гонке идемпотентности возвращает прежнюю запись.
func (r *TRONInvoices) Create(ctx context.Context, key string, index uint64, address, amount string) (domain.TRONInvoice, error) {
	parsedAmount, ok := new(big.Int).SetString(amount, 10)
	if !ok || parsedAmount.Sign() <= 0 || index > uint64(^uint64(0)>>1) {
		return domain.TRONInvoice{}, domain.ErrInvalidTRONInvoice
	}
	row, err := db.New(r.pool).CreateTRONInvoice(ctx, db.CreateTRONInvoiceParams{
		Column1: key, KeyIndex: int64(index), Address: address,
		ExpectedAmount: pgtype.Numeric{Int: parsedAmount, Valid: true},
	})
	if err != nil {
		return domain.TRONInvoice{}, fmt.Errorf("сохранить TRON-инвойс: %w", err)
	}
	return domain.TRONInvoice{ID: row.ID, Address: row.Address, Asset: row.Asset, ExpectedAmount: row.ExpectedAmount, CreatedAt: row.CreatedAt.Time}, nil
}

// GetWithTransfers возвращает инвойс и переводы из подтверждённой таблицы chain-worker.
func (r *TRONInvoices) GetWithTransfers(ctx context.Context, id string) (domain.TRONInvoice, error) {
	var uuid pgtype.UUID
	if err := uuid.Scan(id); err != nil {
		return domain.TRONInvoice{}, domain.ErrTRONInvoiceNotFound
	}
	queries := db.New(r.pool)
	row, err := queries.GetTRONInvoiceByID(ctx, uuid)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.TRONInvoice{}, domain.ErrTRONInvoiceNotFound
	}
	if err != nil {
		return domain.TRONInvoice{}, err
	}
	transferRows, err := queries.ListTRONInvoiceTransfers(ctx, uuid)
	if err != nil {
		return domain.TRONInvoice{}, err
	}
	invoice := domain.TRONInvoice{
		ID: row.ID, Address: row.Address, Asset: row.Asset,
		ExpectedAmount: row.ExpectedAmount, CreatedAt: row.CreatedAt.Time,
		Transfers: make([]domain.TRONInvoiceTransfer, 0, len(transferRows)),
	}
	for _, transfer := range transferRows {
		invoice.Transfers = append(invoice.Transfers, domain.TRONInvoiceTransfer{
			TransactionID: transfer.TransactionID, EventIndex: transfer.EventIndex,
			BlockNumber: transfer.BlockNumber, From: transfer.FromAddress, Amount: transfer.TAmount,
		})
	}
	return invoice, nil
}
