// Package postgres реализует адаптеры PostgreSQL для payment-core.
package postgres

import (
	"context"

	"github.com/renegadik/crypto-payment-gateway/payment-core/internal/domain"
	"github.com/renegadik/crypto-payment-gateway/payment-core/internal/repository/postgres/db"
)

var _ domain.HealthChecker = (*Health)(nil)

type Health struct{ queries *db.Queries }

// NewHealth создаёт проверку доступности PostgreSQL.
func NewHealth(conn db.DBTX) *Health { return &Health{queries: db.New(conn)} }

// Check выполняет минимальный запрос для проверки соединения с PostgreSQL.
func (h *Health) Check(ctx context.Context) error {
	_, err := h.queries.Ping(ctx)
	return err
}
