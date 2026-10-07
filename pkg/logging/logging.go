// Package logging настраивает общий структурированный логгер.
package logging

import (
	"io"
	"log/slog"
)

// New создаёт JSON-логгер с именем сервиса и заданным уровнем логирования.
func New(out io.Writer, service string, level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(out, &slog.HandlerOptions{Level: level})).With("service", service)
}
