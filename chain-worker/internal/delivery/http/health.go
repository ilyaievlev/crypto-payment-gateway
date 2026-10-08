// Package http реализует HTTP-обработчики мониторинга сервиса.
package http

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/renegadik/crypto-payment-gateway/chain-worker/internal/domain"
	tronclient "github.com/renegadik/crypto-payment-gateway/chain-worker/internal/infrastructure/tron"
)

// NewHandler создаёт HTTP-маршрутизатор для health, readiness и Swagger.
func NewHandler(checker domain.HealthChecker, client *tronclient.Client) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health)
	mux.HandleFunc("GET /readyz", readiness(checker))
	mux.HandleFunc("POST /internal/v1/tron/context", tronContext(client))
	mux.HandleFunc("POST /internal/v1/tron/broadcast", broadcastTRON(client))
	mux.HandleFunc("GET /internal/v1/tron/transactions/{id}", tronTransactionStatus(client))
	return mux
}

// tronContext выдаёт краткоживущий контекст, привязанный к актуальному блоку Nile.
func tronContext(client *tronclient.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if client == nil {
			http.Error(w, "TRON node unavailable", http.StatusServiceUnavailable)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1024)
		var request struct {
			FeeLimitSun int64 `json:"fee_limit_sun"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil || request.FeeLimitSun < 0 || request.FeeLimitSun > 1_000_000_000 {
			http.Error(w, "invalid TRON fee limit", http.StatusBadRequest)
			return
		}
		encoded, err := client.LatestContext(r.Context(), request.FeeLimitSun)
		if err != nil {
			http.Error(w, "could not get TRON network context", http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"network_context_hex": hex.EncodeToString(encoded)})
	}
}

// broadcastTRON принимает сериализованную подписанную protobuf-транзакцию.
func broadcastTRON(client *tronclient.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if client == nil {
			http.Error(w, "TRON node unavailable", http.StatusServiceUnavailable)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
		var request struct {
			TransactionHex string `json:"signed_transaction_hex"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil || len(request.TransactionHex) == 0 || len(request.TransactionHex) > 2<<20 {
			http.Error(w, "invalid signed transaction", http.StatusBadRequest)
			return
		}
		encoded, err := hex.DecodeString(request.TransactionHex)
		if err != nil {
			http.Error(w, "invalid signed transaction", http.StatusBadRequest)
			return
		}
		id, err := client.Broadcast(r.Context(), encoded)
		if err != nil {
			http.Error(w, "TRON node rejected broadcast", http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"transaction_id": id, "status": "broadcast"})
	}
}

// tronTransactionStatus сообщает о финализированном включении и результате исполнения.
func tronTransactionStatus(client *tronclient.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if client == nil {
			http.Error(w, "TRON node unavailable", http.StatusServiceUnavailable)
			return
		}
		id := strings.ToLower(r.PathValue("id"))
		body, found, err := client.SolidifiedTransaction(r.Context(), id)
		if err != nil {
			http.Error(w, "could not query TRON transaction", http.StatusBadGateway)
			return
		}
		if !found {
			writeJSON(w, http.StatusOK, map[string]any{"transaction_id": id, "status": "pending"})
			return
		}
		var transaction struct {
			Results []struct {
				ContractResult string `json:"contractRet"`
			} `json:"ret"`
		}
		if err := json.Unmarshal(body, &transaction); err != nil {
			http.Error(w, "invalid TRON transaction response", http.StatusBadGateway)
			return
		}
		if len(transaction.Results) == 0 || !strings.EqualFold(transaction.Results[0].ContractResult, "SUCCESS") {
			writeJSON(w, http.StatusOK, map[string]any{"transaction_id": id, "status": "failed"})
			return
		}
		info, hasReceipt, err := client.SolidifiedTransactionInfo(r.Context(), id)
		if err != nil {
			http.Error(w, "could not query TRON transaction receipt", http.StatusBadGateway)
			return
		}
		var receipt struct {
			Result  string `json:"result"`
			Receipt struct {
				Result string `json:"result"`
			} `json:"receipt"`
		}
		if hasReceipt {
			if err := json.Unmarshal(info, &receipt); err != nil {
				http.Error(w, "invalid TRON receipt response", http.StatusBadGateway)
				return
			}
			if !strings.EqualFold(receipt.Result, "SUCCESS") || !strings.EqualFold(receipt.Receipt.Result, "SUCCESS") {
				writeJSON(w, http.StatusOK, map[string]any{"transaction_id": id, "status": "failed"})
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"transaction_id": id, "status": "confirmed", "receipt_available": hasReceipt})
	}
}

// writeJSON кодирует внутренний ответ JSON.
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
