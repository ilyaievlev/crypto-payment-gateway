// Package http реализует HTTP-обработчики мониторинга сервиса.
package http

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"time"

	"github.com/renegadik/crypto-payment-gateway/crypto-vault/internal/domain"
)

// NewHandler создаёт HTTP-маршрутизатор для health, readiness и Swagger.
func NewHandler(checker domain.HealthChecker, engine domain.Engine) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health)
	mux.HandleFunc("GET /readyz", readiness(checker))
	var wallet domain.Wallet
	if engine != nil {
		wallet = engine.Wallet()
	}
	mux.HandleFunc("POST /internal/v1/tron/addresses", deriveTRONAddress(wallet))
	mux.HandleFunc("POST /internal/v1/tron/sign-transfer", signTRONTransfer(engine))
	return mux
}

// deriveTRONAddress возвращает дочерний публичный адрес, не раскрывая ключ или сид.
func deriveTRONAddress(wallet domain.Wallet) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if wallet == nil {
			http.Error(w, "TRON wallet unavailable", http.StatusServiceUnavailable)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1024)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var request struct {
			Index uint32 `json:"index"`
		}
		if err := decoder.Decode(&request); err != nil {
			http.Error(w, "invalid address request", http.StatusBadRequest)
			return
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			http.Error(w, "invalid address request", http.StatusBadRequest)
			return
		}
		address, err := wallet.DeriveAddress(r.Context(), domain.KeyRef{Index: request.Index})
		if err != nil {
			http.Error(w, "address derivation failed", http.StatusUnprocessableEntity)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(struct {
			Address string `json:"address"`
			Index   uint32 `json:"index"`
		}{Address: string(address), Index: request.Index})
	}
}

// signTRONTransfer проверяет intent, строит транзакцию и возвращает только её подпись.
func signTRONTransfer(engine domain.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if engine == nil || engine.Network() != domain.NetworkTRON {
			http.Error(w, "TRON engine unavailable", http.StatusServiceUnavailable)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var request struct {
			Index   uint32  `json:"index"`
			From    string  `json:"from"`
			To      string  `json:"to"`
			Amount  string  `json:"amount_sun"`
			Asset   *string `json:"asset_contract,omitempty"`
			Context string  `json:"network_context_hex"`
		}
		if err := decoder.Decode(&request); err != nil {
			http.Error(w, "invalid transfer request", http.StatusBadRequest)
			return
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			http.Error(w, "invalid transfer request", http.StatusBadRequest)
			return
		}
		amount, ok := new(big.Int).SetString(request.Amount, 10)
		if !ok || amount.Sign() <= 0 || amount.BitLen() > 256 || len(request.Amount) > 78 || !engine.Wallet().ValidateAddress(domain.Address(request.From)) || !engine.Wallet().ValidateAddress(domain.Address(request.To)) {
			http.Error(w, "invalid transfer intent", http.StatusBadRequest)
			return
		}
		contextBytes, err := hex.DecodeString(request.Context)
		if err != nil || len(contextBytes) > 4096 {
			http.Error(w, "invalid network context", http.StatusBadRequest)
			return
		}
		var asset *domain.AssetID
		if request.Asset != nil {
			value := domain.AssetID(*request.Asset)
			asset = &value
		}
		intent := domain.TransferIntent{
			From: domain.Address(request.From), To: domain.Address(request.To), Amount: amount, Asset: asset,
		}
		unsigned, err := engine.Builder().Build(r.Context(), intent, contextBytes)
		if err != nil {
			http.Error(w, "could not build TRON transaction", http.StatusUnprocessableEntity)
			return
		}
		signed, err := engine.Signer().Sign(r.Context(), unsigned, domain.KeyRef{Index: request.Index})
		if err != nil {
			http.Error(w, "could not sign TRON transaction", http.StatusUnprocessableEntity)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			SignedTransactionHex string `json:"signed_transaction_hex"`
		}{SignedTransactionHex: hex.EncodeToString(signed)})
	}
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
