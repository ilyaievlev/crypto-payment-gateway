package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/renegadik/crypto-payment-gateway/pkg/config"
	"github.com/renegadik/crypto-payment-gateway/pkg/logging"
	"github.com/renegadik/crypto-payment-gateway/pkg/server"
	delivery "github.com/renegadik/crypto-payment-gateway/webhook-sender/internal/delivery/http"
	"github.com/renegadik/crypto-payment-gateway/webhook-sender/internal/usecase"
)

// @title Crypto Payment Gateway API
// @version 0.1.0
// @description Каркас сервиса. API для бизнеса пока не реализован.
// @BasePath /
// main запускает сервис и завершает процесс с ошибкой при неудачном старте.
func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "server failed:", err)
		os.Exit(1)
	}
}

// run собирает зависимости, настраивает обработку сигналов и запускает сервер.
func run() error {
	if handled, err := server.Probe(); handled {
		return err
	}
	cfg, err := config.Load(":8083", "")
	if err != nil {
		return err
	}
	logger := logging.New(os.Stdout, "webhook-sender", cfg.LogLevel)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// Здесь репозитории и инфраструктурные адаптеры подключаются к сценариям.
	checker := usecase.NewHealth()
	return server.Run(ctx, logger, cfg.HTTPAddr, cfg.GRPCAddr, delivery.NewHandler(checker))
}
