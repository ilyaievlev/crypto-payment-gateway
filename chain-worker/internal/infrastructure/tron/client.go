// Package tron предоставляет клиент HTTP API сети TRON для chain-worker.
package tron

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	cryptovaultv1 "github.com/renegadik/crypto-payment-gateway/pkg/proto/cryptovault/v1"
	"google.golang.org/protobuf/proto"

	"github.com/renegadik/crypto-payment-gateway/chain-worker/internal/domain"
)

const (
	// nileEndpoint задаёт публичный HTTP endpoint тестовой сети Nile.
	nileEndpoint = "https://nile.trongrid.io"
	// latestBlockPath — путь получения текущего блока через FullNode API.
	latestBlockPath = "/wallet/getnowblock"
	// latestSolidifiedBlockPath возвращает последний финализированный блок.
	latestSolidifiedBlockPath = "/walletsolidity/getnowblock"
	// solidifiedBlockByNumberPath возвращает блок из финализированной цепочки.
	solidifiedBlockByNumberPath = "/walletsolidity/getblockbynum"
	// broadcastHexPath принимает protobuf-представление подписанной транзакции.
	broadcastHexPath = "/wallet/broadcasthex"
	// solidifiedTransactionPath проверяет включение транзакции в финализированную цепь.
	solidifiedTransactionPath = "/walletsolidity/gettransactionbyid"
	// solidifiedTransactionInfoPath возвращает финализированный receipt исполнения.
	solidifiedTransactionInfoPath = "/walletsolidity/gettransactioninfobyid"
	// maxResponseSize ограничивает объём ответа от узла.
	maxResponseSize = 16 << 20
	// transactionLifetime задаёт срок действия создаваемого сетевого контекста.
	transactionLifetime = time.Minute
)

// Client обращается к TRON FullNode API и формирует контекст для транзакций.
type Client struct {
	endpoint string
	apiKey   string
	http     *http.Client
}

var _ domain.HealthChecker = (*Client)(nil)

// NewClient создаёт HTTP-клиент Nile. endpoint должен содержать адрес узла,
// а apiKey может быть пустым для публичного тестового endpoint.
func NewClient(endpoint, apiKey string) (*Client, error) {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return nil, fmt.Errorf("адрес TRON-узла: неверный формат")
	}
	if parsed.Scheme == "http" && !isLoopbackHost(parsed.Hostname()) {
		return nil, fmt.Errorf("для удалённого TRON-узла требуется HTTPS")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("адрес TRON-узла не должен содержать query или fragment")
	}

	return &Client{
		endpoint: strings.TrimRight(parsed.String(), "/"),
		apiKey:   strings.TrimSpace(apiKey),
		http: &http.Client{
			Timeout: 8 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

// DefaultNileEndpoint возвращает официальный публичный HTTP endpoint сети Nile.
func DefaultNileEndpoint() string { return nileEndpoint }

// LatestContext получает текущий блок и кодирует его параметры в TronContextV1.
// feeLimitSun передаётся builder-у и используется для TRC-20 вызовов.
func (c *Client) LatestContext(ctx context.Context, feeLimitSun int64) ([]byte, error) {
	if feeLimitSun < 0 {
		return nil, fmt.Errorf("лимит комиссии TRON не может быть отрицательным")
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.endpoint+latestBlockPath,
		bytes.NewReader([]byte("{}")),
	)
	if err != nil {
		return nil, fmt.Errorf("создать запрос к TRON-узлу: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		request.Header.Set("TRON-PRO-API-KEY", c.apiKey)
	}

	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("запросить текущий блок TRON: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("TRON-узел вернул HTTP %d", response.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseSize+1))
	if err != nil {
		return nil, fmt.Errorf("прочитать ответ TRON-узла: %w", err)
	}
	if len(body) > maxResponseSize {
		return nil, fmt.Errorf("ответ TRON-узла превышает допустимый размер")
	}
	var block latestBlockResponse
	if err := json.Unmarshal(body, &block); err != nil {
		return nil, fmt.Errorf("разобрать ответ TRON-узла: %w", err)
	}
	blockID, err := hex.DecodeString(block.BlockID)
	if err != nil || len(blockID) != 32 {
		return nil, fmt.Errorf("TRON-узел вернул некорректный blockID")
	}
	if block.BlockHeader.RawData.Number == 0 || block.BlockHeader.RawData.Timestamp <= 0 {
		return nil, fmt.Errorf("TRON-узел вернул неполные данные текущего блока")
	}
	if got := decodeBlockNumber(blockID[:8]); got != block.BlockHeader.RawData.Number {
		return nil, fmt.Errorf("номер в TRON blockID не совпадает с номером блока")
	}

	now := time.Now().UnixMilli()
	contextBytes, err := proto.Marshal(&cryptovaultv1.TronContextV1{
		BlockNumber:  block.BlockHeader.RawData.Number,
		BlockId:      blockID,
		TimestampMs:  now,
		ExpirationMs: now + transactionLifetime.Milliseconds(),
		FeeLimitSun:  feeLimitSun,
	})
	if err != nil {
		return nil, fmt.Errorf("закодировать контекст TRON: %w", err)
	}
	return contextBytes, nil
}

// SolidifiedHeight возвращает высоту последнего блока, доступного в SolidityNode.
func (c *Client) SolidifiedHeight(ctx context.Context) (uint64, error) {
	response, err := c.postJSON(ctx, latestSolidifiedBlockPath, map[string]any{})
	if err != nil {
		return 0, err
	}
	var block latestBlockResponse
	if err := json.Unmarshal(response, &block); err != nil {
		return 0, fmt.Errorf("разобрать финализированный блок TRON: %w", err)
	}
	if block.BlockHeader.RawData.Number == 0 || len(block.BlockID) != 64 {
		return 0, fmt.Errorf("узел вернул некорректную высоту финализированного блока")
	}
	return block.BlockHeader.RawData.Number, nil
}

// SolidifiedBlock загружает блок по высоте из финализированной цепочки.
func (c *Client) SolidifiedBlock(ctx context.Context, number uint64) (domain.TRONBlock, error) {
	if number == 0 || number > uint64(^uint32(0)>>1) {
		return domain.TRONBlock{}, fmt.Errorf("высота TRON вне допустимого диапазона")
	}
	payload, err := c.postJSON(ctx, solidifiedBlockByNumberPath, map[string]uint64{"num": number})
	if err != nil {
		return domain.TRONBlock{}, err
	}
	var result struct {
		ID     string `json:"blockID"`
		Header struct {
			Raw struct {
				Number uint64 `json:"number"`
			} `json:"raw_data"`
		} `json:"block_header"`
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		return domain.TRONBlock{}, fmt.Errorf("разобрать блок TRON: %w", err)
	}
	decodedID, err := hex.DecodeString(result.ID)
	if err != nil || len(decodedID) != 32 || result.Header.Raw.Number != number || decodeBlockNumber(decodedID[:8]) != number {
		return domain.TRONBlock{}, fmt.Errorf("TRON-узел вернул некорректный блок %d", number)
	}
	return domain.TRONBlock{Number: number, ID: strings.ToLower(result.ID), Payload: payload}, nil
}

// Broadcast передаёт подписанную транзакцию узлу Nile.
// Подтверждением успеха считается только последующая проверка в SolidityNode.
func (c *Client) Broadcast(ctx context.Context, transaction json.RawMessage) (string, error) {
	if len(transaction) == 0 || len(transaction) > maxResponseSize {
		return "", fmt.Errorf("размер транзакции TRON вне допустимого диапазона")
	}
	response, err := c.postJSON(ctx, broadcastHexPath, map[string]string{
		"transaction": hex.EncodeToString(transaction),
	})
	if err != nil {
		return "", err
	}
	var result struct {
		Result  bool   `json:"result"`
		TxID    string `json:"txid"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(response, &result); err != nil {
		return "", fmt.Errorf("разобрать ответ отправки транзакции TRON: %w", err)
	}
	if !result.Result || len(result.TxID) != 64 {
		return "", fmt.Errorf("узел отклонил транзакцию TRON (код %s)", result.Code)
	}
	if _, err := hex.DecodeString(result.TxID); err != nil {
		return "", fmt.Errorf("узел вернул некорректный txid TRON")
	}
	return strings.ToLower(result.TxID), nil
}

// SolidifiedTransaction проверяет, включена ли транзакция в финализированную цепь.
// Пустой ответ означает, что подтверждение пока не доступно.
func (c *Client) SolidifiedTransaction(ctx context.Context, transactionID string) (json.RawMessage, bool, error) {
	if !validTransactionID(transactionID) {
		return nil, false, fmt.Errorf("некорректный идентификатор транзакции TRON")
	}
	response, err := c.postJSON(ctx, solidifiedTransactionPath, map[string]string{"value": strings.ToLower(transactionID)})
	if err != nil {
		return nil, false, err
	}
	var result struct {
		ID   string          `json:"txID"`
		ID2  string          `json:"txid"`
		Body json.RawMessage `json:"raw_data"`
	}
	if err := json.Unmarshal(response, &result); err != nil {
		return nil, false, fmt.Errorf("разобрать подтверждение транзакции TRON: %w", err)
	}
	if result.ID == "" && result.ID2 == "" {
		return response, false, nil
	}
	got := result.ID
	if got == "" {
		got = result.ID2
	}
	if !strings.EqualFold(got, transactionID) || len(result.Body) == 0 || string(result.Body) == "null" {
		return nil, false, fmt.Errorf("ответ узла не соответствует запрошенному txid TRON")
	}
	return response, true, nil
}

// SolidifiedTransactionInfo возвращает финализированный результат исполнения.
func (c *Client) SolidifiedTransactionInfo(ctx context.Context, transactionID string) (json.RawMessage, bool, error) {
	if !validTransactionID(transactionID) {
		return nil, false, fmt.Errorf("некорректный идентификатор транзакции TRON")
	}
	response, err := c.postJSON(ctx, solidifiedTransactionInfoPath, map[string]string{"value": strings.ToLower(transactionID)})
	if err != nil {
		return nil, false, err
	}
	var result struct {
		ID      string `json:"id"`
		Result  string `json:"result"`
		Receipt struct {
			Result string `json:"result"`
		} `json:"receipt"`
	}
	if err := json.Unmarshal(response, &result); err != nil {
		return nil, false, fmt.Errorf("разобрать receipt транзакции TRON: %w", err)
	}
	if result.ID == "" {
		return response, false, nil
	}
	if !strings.EqualFold(result.ID, transactionID) {
		return nil, false, fmt.Errorf("receipt не соответствует запрошенному txid TRON")
	}
	return response, true, nil
}

// validTransactionID проверяет шестидесятичетырёхзначный hex txid TRON.
func validTransactionID(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// Check проверяет доступность Nile запросом текущего блока.
func (c *Client) Check(ctx context.Context) error {
	_, err := c.LatestContext(ctx, 0)
	return err
}

// postJSON выполняет ограниченный POST-запрос к API TRON и проверяет HTTP-статус.
func (c *Client) postJSON(ctx context.Context, path string, payload any) (json.RawMessage, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("кодировать запрос к TRON-узлу: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("создать запрос к TRON-узлу: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		request.Header.Set("TRON-PRO-API-KEY", c.apiKey)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("запросить TRON-узел: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("TRON-узел вернул HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseSize+1))
	if err != nil {
		return nil, fmt.Errorf("прочитать ответ TRON-узла: %w", err)
	}
	if len(data) > maxResponseSize {
		return nil, fmt.Errorf("ответ TRON-узла превышает допустимый размер")
	}
	var envelope struct {
		Error string `json:"Error"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("разобрать ответ TRON-узла: %w", err)
	}
	if envelope.Error != "" {
		return nil, fmt.Errorf("TRON-узел сообщил об ошибке")
	}
	return data, nil
}

// isLoopbackHost разрешает HTTP только для локального узла разработки.
func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

// latestBlockResponse содержит используемые поля ответа getnowblock.
type latestBlockResponse struct {
	BlockID     string `json:"blockID"`
	BlockHeader struct {
		RawData struct {
			Number    uint64 `json:"number"`
			Timestamp int64  `json:"timestamp"`
		} `json:"raw_data"`
	} `json:"block_header"`
}

// decodeBlockNumber читает первые восемь байт blockID как uint64 в big-endian.
func decodeBlockNumber(value []byte) uint64 {
	var number uint64
	for _, b := range value {
		number = number<<8 | uint64(b)
	}
	return number
}
