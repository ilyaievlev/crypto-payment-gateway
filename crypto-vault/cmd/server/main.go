package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	delivery "github.com/renegadik/crypto-payment-gateway/crypto-vault/internal/delivery/http"
	"github.com/renegadik/crypto-payment-gateway/crypto-vault/internal/domain"
	cryptoengine "github.com/renegadik/crypto-payment-gateway/crypto-vault/internal/infrastructure/crypto_engine"
	"github.com/renegadik/crypto-payment-gateway/crypto-vault/internal/infrastructure/seedvault"
	"github.com/renegadik/crypto-payment-gateway/crypto-vault/internal/repository/postgres"
	"github.com/renegadik/crypto-payment-gateway/crypto-vault/internal/usecase"
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
	if len(os.Args) == 2 && os.Args[1] == "init-seed" {
		if err := initializeSeed(); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "ошибка инициализации TRON-кошелька:", err)
			os.Exit(1)
		}
		return
	}
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
	cfg, err := config.Load(":8084", ":9084")
	if err != nil {
		return err
	}
	logger := logging.New(os.Stdout, "crypto-vault", cfg.LogLevel)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	store, pool, err := postgres.NewSeedStore(ctx, os.Getenv("VAULT_DATABASE_URL"))
	if err != nil {
		return err
	}
	defer pool.Close()
	provider, err := loadProvider(store)
	if err != nil {
		return err
	}
	defer provider.Close()
	factory, err := cryptoengine.NewFactory(provider)
	if err != nil {
		return err
	}
	engine, err := factory.Engine(domain.NetworkTRON)
	if err != nil {
		return err
	}
	checker := usecase.NewHealth(store)
	return server.Run(ctx, logger, cfg.HTTPAddr, cfg.GRPCAddr, delivery.NewHandler(checker, engine))
}

// initializeSeed создаёт ключевой файл, шифрует новый TRON seed в PostgreSQL
// и выводит мнемоническую фразу владельцу только после успешного сохранения.
func initializeSeed() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, pool, err := postgres.NewSeedStore(ctx, os.Getenv("VAULT_DATABASE_URL"))
	if err != nil {
		return err
	}
	defer pool.Close()
	if _, err := store.LoadEncryptedSeed(ctx, domain.NetworkTRON); err == nil {
		return fmt.Errorf("TRON-кошелёк уже инициализирован; повторная инициализация запрещена")
	} else if !errors.Is(err, domain.ErrSeedUnavailable) {
		return err
	}
	provider, err := loadProviderForInitialization(store)
	if err != nil {
		return err
	}
	defer provider.Close()
	mnemonic, err := provider.InitializeTRON(ctx)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(os.Stdout, "Фраза восстановления TRON (сохраните её отдельно; повторно она не показывается):")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(os.Stdout, mnemonic)
	return err
}

// loadProvider загружает пароль из файла и открывает существующий локальный ключ.
func loadProvider(store *postgres.SeedStore) (*seedvault.Provider, error) {
	return seedvault.OpenProvider(store, keyFilePath(), passwordFilePath(), false)
}

// loadProviderForInitialization открывает или создаёт локальный ключ данных.
func loadProviderForInitialization(store *postgres.SeedStore) (*seedvault.Provider, error) {
	return seedvault.OpenProvider(store, keyFilePath(), passwordFilePath(), true)
}

// keyFilePath возвращает путь к зашифрованному локальному ключу данных.
func keyFilePath() string {
	if path := os.Getenv("VAULT_KEY_FILE"); path != "" {
		return path
	}
	return "/var/lib/crypto-vault/keys/wrapped-key.json"
}

// passwordFilePath возвращает путь к паролю, переданному отдельным секрет-файлом.
func passwordFilePath() string {
	if path := os.Getenv("VAULT_PASSWORD_FILE"); path != "" {
		return path
	}
	return "/run/secrets/vault_password"
}
