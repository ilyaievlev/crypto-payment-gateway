// Package http реализует HTTP-обработчики мониторинга сервиса.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/renegadik/crypto-payment-gateway/payment-core/internal/domain"
	"github.com/renegadik/crypto-payment-gateway/payment-core/internal/usecase"
)

// NewHandler создаёт HTTP-маршрутизатор для health, readiness и Swagger.
func NewHandler(checker domain.HealthChecker, invoices *usecase.TRONInvoices) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health)
	mux.HandleFunc("GET /readyz", readiness(checker))
	mux.HandleFunc("POST /v1/tron/invoices", createTRONInvoice(invoices))
	mux.HandleFunc("GET /v1/tron/invoices/{id}", getTRONInvoice(invoices))
	return mux
}

// createTRONInvoice создаёт уникальный HD-адрес и инвойс в минимальных единицах TRX.
func createTRONInvoice(invoices *usecase.TRONInvoices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if invoices == nil {
			http.Error(w, "TRON invoices unavailable", http.StatusServiceUnavailable)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var request struct {
			Amount string `json:"amount_sun"`
		}
		if err := decoder.Decode(&request); err != nil {
			http.Error(w, "amount_sun is required", http.StatusBadRequest)
			return
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		invoice, err := invoices.Create(r.Context(), r.Header.Get("Idempotency-Key"), request.Amount)
		if err != nil {
			if errors.Is(err, domain.ErrInvalidTRONInvoice) {
				http.Error(w, "amount_sun or idempotency key is invalid", http.StatusBadRequest)
				return
			}
			http.Error(w, "could not create TRON invoice", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusCreated, invoice)
	}
}

// getTRONInvoice показывает поступления, обнаруженные в финализированной цепочке.
func getTRONInvoice(invoices *usecase.TRONInvoices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if invoices == nil {
			http.Error(w, "TRON invoices unavailable", http.StatusServiceUnavailable)
			return
		}
		invoice, err := invoices.Get(r.Context(), r.PathValue("id"))
		if errors.Is(err, domain.ErrTRONInvoiceNotFound) || errors.Is(err, domain.ErrInvalidTRONInvoice) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "could not read TRON invoice", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, invoice)
	}
}

// writeJSON кодирует HTTP-ответ JSON с указанным статусом.
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
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
