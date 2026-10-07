// Package domain содержит сущности и порты без внешних зависимостей.
package domain

import "context"

//go:generate mockgen -source=health.go -destination=../usecase/health_mock_test.go -package=usecase

// HealthChecker проверяет доступность обязательной зависимости сервиса.
type HealthChecker interface {
	// Check возвращает ошибку, если зависимость недоступна.
	Check(context.Context) error
}
