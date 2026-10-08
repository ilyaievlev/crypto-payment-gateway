package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/renegadik/crypto-payment-gateway/crypto-vault/internal/domain"
	"github.com/renegadik/crypto-payment-gateway/crypto-vault/internal/repository/postgres/db"
)

// SeedStore сохраняет только зашифрованные корневые сиды Crypto Vault.
type SeedStore struct {
	queries *db.Queries
}

var _ domain.SeedRepository = (*SeedStore)(nil)

// NewSeedStore открывает пул соединений PostgreSQL и создаёт репозиторий сидов.
func NewSeedStore(ctx context.Context, databaseURL string) (*SeedStore, *pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("создать пул PostgreSQL Crypto Vault: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("подключиться к PostgreSQL Crypto Vault: %w", err)
	}
	return &SeedStore{queries: db.New(pool)}, pool, nil
}

// LoadEncryptedSeed загружает зашифрованный сид для указанной сети.
func (s *SeedStore) LoadEncryptedSeed(ctx context.Context, network domain.Network) ([]byte, error) {
	ciphertext, err := s.queries.LoadEncryptedSeed(ctx, string(network))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrSeedUnavailable
	}
	if err != nil {
		return nil, fmt.Errorf("загрузить ciphertext сида из PostgreSQL: %w", domain.ErrSeedStoreFailed)
	}
	return ciphertext, nil
}

// CreateEncryptedSeed сохраняет ciphertext, если для сети ещё нет корневого сида.
func (s *SeedStore) CreateEncryptedSeed(ctx context.Context, network domain.Network, ciphertext []byte) error {
	_, err := s.queries.CreateEncryptedSeed(ctx, db.CreateEncryptedSeedParams{
		Network:    string(network),
		Ciphertext: ciphertext,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrSeedAlreadyInitialized
	}
	if err != nil {
		return fmt.Errorf("сохранить ciphertext сида в PostgreSQL: %w", domain.ErrSeedStoreFailed)
	}
	return nil
}

// Check проверяет соединение с PostgreSQL и наличие зашифрованного TRON-сида.
func (s *SeedStore) Check(ctx context.Context) error {
	if _, err := s.queries.Ping(ctx); err != nil {
		return fmt.Errorf("проверить PostgreSQL Crypto Vault: %w", err)
	}
	exists, err := s.queries.SeedExists(ctx, string(domain.NetworkTRON))
	if err != nil {
		return fmt.Errorf("проверить наличие TRON-сида: %w", domain.ErrSeedStoreFailed)
	}
	if !exists {
		return domain.ErrSeedUnavailable
	}
	return nil
}
