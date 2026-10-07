// Package grpcinterceptors реализует общие middleware для gRPC-транспорта.
package grpcinterceptors

import (
	"context"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// Logging записывает только метод, статус и длительность запроса. Тела запросов
// и ответов, metadata и тексты ошибок исключены, чтобы не раскрывать секреты.
func Logging(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		response, err := handler(ctx, req)
		logger.InfoContext(ctx, "gRPC request", "method", info.FullMethod, "code", status.Code(err).String(), "duration", time.Since(start))
		return response, err
	}
}
