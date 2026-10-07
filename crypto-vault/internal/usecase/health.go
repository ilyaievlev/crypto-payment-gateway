// Package usecase координирует сценарии приложения через доменные порты.
package usecase

import (
	"context"
	"fmt"

	"github.com/renegadik/crypto-payment-gateway/crypto-vault/internal/domain"
)

type Health struct{ dependencies []domain.HealthChecker }

// NewHealth создаёт проверку готовности с указанными зависимостями.
func NewHealth(dependencies ...domain.HealthChecker) *Health {
	return &Health{dependencies: append([]domain.HealthChecker(nil), dependencies...)}
}

// Check возвращает ошибку первой недоступной зависимости. Если зависимости не
// переданы, результат описывает только готовность запущенного процесса.
func (h *Health) Check(ctx context.Context) error {
	for _, dependency := range h.dependencies {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := dependency.Check(ctx); err != nil {
			return fmt.Errorf("dependency unavailable: %w", err)
		}
	}
	return ctx.Err()
}
