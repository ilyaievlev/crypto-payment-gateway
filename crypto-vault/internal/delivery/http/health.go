// Package http реализует HTTP-обработчики мониторинга сервиса.
package http

import (
	"context"
	"net/http"
	"time"

	"github.com/renegadik/crypto-payment-gateway/crypto-vault/internal/domain"
)

// NewHandler создаёт HTTP-маршрутизатор для health, readiness и Swagger.
func NewHandler(checker domain.HealthChecker) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health)
	mux.HandleFunc("GET /readyz", readiness(checker))
	return mux
}

// health сообщает, что процесс запущен.
// @Summary Проверка жизни процесса
// @Tags monitoring
// @Produce json
// @Success 200 {object} map[string]string
// @Router /healthz [get]
func health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// readiness сообщает о доступности подключённых зависимостей.
// @Summary Проверка готовности процесса
// @Tags monitoring
// @Produce json
// @Success 200 {object} map[string]string
// @Failure 503 {string} string
// @Router /readyz [get]
func readiness(checker domain.HealthChecker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := checker.Check(ctx); err != nil {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	}
}
