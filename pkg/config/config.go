// Package config читает конфигурацию процесса, не содержащую секретов.
package config

import (
	"fmt"
	"log/slog"
	"net"
	"os"
)

type Config struct {
	HTTPAddr string
	GRPCAddr string
	LogLevel slog.Level
}

// Load читает адреса и уровень логирования из окружения, подставляет значения по
// умолчанию и проверяет формат сетевых адресов.
func Load(defaultHTTP, defaultGRPC string) (Config, error) {
	cfg := Config{HTTPAddr: env("HTTP_ADDR", defaultHTTP), GRPCAddr: env("GRPC_ADDR", defaultGRPC)}
	if err := cfg.LogLevel.UnmarshalText([]byte(env("LOG_LEVEL", "info"))); err != nil {
		return Config{}, fmt.Errorf("invalid LOG_LEVEL: %w", err)
	}
	for _, addr := range []string{cfg.HTTPAddr, cfg.GRPCAddr} {
		if addr == "" {
			continue
		}
		if _, _, err := net.SplitHostPort(addr); err != nil {
			return Config{}, fmt.Errorf("invalid listen address: %w", err)
		}
	}
	return cfg, nil
}

// env возвращает значение переменной окружения либо fallback, если она не задана.
func env(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
