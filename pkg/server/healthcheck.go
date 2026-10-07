package server

import (
	"fmt"
	"net/http"
	"os"
	"time"
)

// Probe выполняет healthcheck в distroless-контейнере без curl и shell.
// handled равен false при обычном запуске серверного процесса.
func Probe() (handled bool, err error) {
	if len(os.Args) != 2 || os.Args[1] != "-healthcheck" {
		return false, nil
	}
	client := &http.Client{Timeout: 2 * time.Second}
	// Внутри контейнера endpoint мониторинга всегда доступен на порту 8080.
	response, err := client.Get("http://127.0.0.1:8080/healthz")
	if err != nil {
		return true, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return true, fmt.Errorf("health status: %d", response.StatusCode)
	}
	return true, nil
}
