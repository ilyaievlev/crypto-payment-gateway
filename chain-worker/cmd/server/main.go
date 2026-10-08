package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	delivery "github.com/renegadik/crypto-payment-gateway/chain-worker/internal/delivery/http"
	tronclient "github.com/renegadik/crypto-payment-gateway/chain-worker/internal/infrastructure/tron"
	"github.com/renegadik/crypto-payment-gateway/chain-worker/internal/repository/postgres"
	"github.com/renegadik/crypto-payment-gateway/chain-worker/internal/usecase"
	"github.com/renegadik/crypto-payment-gateway/pkg/config"
	"github.com/renegadik/crypto-payment-gateway/pkg/logging"
	"github.com/renegadik/crypto-payment-gateway/pkg/server"
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
	cfg, err := config.Load(":8082", "")
	if err != nil {
		return err
	}
	logger := logging.New(os.Stdout, "chain-worker", cfg.LogLevel)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// Здесь репозитории и инфраструктурные адаптеры подключаются к сценариям.
	endpoint := os.Getenv("TRON_NILE_ENDPOINT")
	if endpoint == "" {
		endpoint = tronclient.DefaultNileEndpoint()
	}
	tronNode, err := tronclient.NewClient(endpoint, os.Getenv("TRON_NILE_API_KEY"))
	if err != nil {
		return err
	}
	store, err := postgres.NewTRONStore(ctx, os.Getenv("CHAIN_DATABASE_URL"))
	if err != nil {
		return err
	}
	defer store.Close()
	scanner, err := usecase.NewTRONScanner(tronNode, store)
	if err != nil {
		return err
	}
	startCursor, err := initialTRONCursor(ctx, tronNode, os.Getenv("TRON_START_BLOCK"))
	if err != nil {
		return err
	}
	if err := scanner.Initialize(ctx, startCursor); err != nil {
		return err
	}
	go runTRONIndexer(ctx, logger, scanner)
	checker := usecase.NewHealth(tronNode, store)
	return server.Run(ctx, logger, cfg.HTTPAddr, cfg.GRPCAddr, delivery.NewHandler(checker, tronNode))
}

// initialTRONCursor выбирает текущую высоту либо блок перед заданным стартом.
func initialTRONCursor(ctx context.Context, client *tronclient.Client, configured string) (uint64, error) {
	if configured != "" {
		firstBlock, err := strconv.ParseUint(configured, 10, 64)
		if err != nil || firstBlock == 0 {
			return 0, fmt.Errorf("TRON_START_BLOCK должен быть положительной высотой")
		}
		return firstBlock - 1, nil
	}
	return client.SolidifiedHeight(ctx)
}

// runTRONIndexer обрабатывает новые финализированные блоки до остановки процесса.
func runTRONIndexer(ctx context.Context, logger *slog.Logger, scanner *usecase.TRONScanner) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		processed, err := scanner.SyncOnce(ctx)
		if err != nil && ctx.Err() == nil {
			logger.Error("ошибка индексации TRON", "error", err)
		} else if processed > 0 {
			logger.Info("обработаны финализированные блоки TRON", "count", processed)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
