// Package tronvault обращается к закрытому HTTP API Crypto Vault.
package tronvault

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/renegadik/crypto-payment-gateway/payment-core/internal/domain"
)

// Client запрашивает у Crypto Vault производные публичные адреса TRON.
type Client struct {
	endpoint string
	http     *http.Client
}

var _ domain.TRONAddressProvider = (*Client)(nil)
var _ domain.HealthChecker = (*Client)(nil)

// New создаёт клиент закрытого API vault с ограниченным сетевым временем ожидания.
func New(endpoint string) (*Client, error) {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("неверный адрес Crypto Vault")
	}
	allowedHTTP := parsed.Scheme == "http" && (isLoopback(parsed.Hostname()) || parsed.Hostname() == "crypto-vault")
	if parsed.Scheme != "https" && !allowedHTTP {
		return nil, fmt.Errorf("endpoint Crypto Vault должен использовать HTTPS или закрытый Docker endpoint")
	}
	return &Client{endpoint: strings.TrimRight(parsed.String(), "/"), http: &http.Client{
		Timeout:       5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

// DeriveAddress получает дочерний публичный TRON-адрес без доступа к секретам.
func (c *Client) DeriveAddress(ctx context.Context, index uint32) (string, error) {
	body, err := json.Marshal(struct {
		Index uint32 `json:"index"`
	}{Index: index})
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/internal/v1/tron/addresses", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("создать запрос к Crypto Vault: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return "", fmt.Errorf("запросить TRON-адрес у Crypto Vault: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("endpoint Crypto Vault вернул HTTP %d при выводе TRON-адреса", response.StatusCode)
	}
	var result struct {
		Address string `json:"address"`
		Index   uint32 `json:"index"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1025))
	if err := decoder.Decode(&result); err != nil || result.Address == "" || result.Index != index {
		return "", fmt.Errorf("endpoint Crypto Vault вернул некорректный TRON-адрес")
	}
	return result.Address, nil
}

// Check проверяет readiness Crypto Vault, включая доступность seed и PostgreSQL.
func (c *Client) Check(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/readyz", nil)
	if err != nil {
		return err
	}
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("проверить Crypto Vault: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("endpoint Crypto Vault не готов: HTTP %d", response.StatusCode)
	}
	return nil
}

// isLoopback разрешает незашифрованный HTTP только в тестовом локальном окружении.
func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}
