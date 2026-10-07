// Package server управляет жизненным циклом процесса и транспортом мониторинга.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/renegadik/crypto-payment-gateway/pkg/grpcinterceptors"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

// Run запускает HTTP и при наличии адреса стандартный gRPC health API.
// Прикладные gRPC API должны регистрироваться до вызова Serve.
func Run(ctx context.Context, logger *slog.Logger, httpAddr, grpcAddr string, handler http.Handler) error {
	httpListener, err := net.Listen("tcp", httpAddr)
	if err != nil {
		return fmt.Errorf("listen HTTP: %w", err)
	}
	defer func() { _ = httpListener.Close() }()
	var grpcListener net.Listener
	var grpcServer *grpc.Server
	var healthServer *health.Server
	if grpcAddr != "" {
		grpcListener, err = net.Listen("tcp", grpcAddr)
		if err != nil {
			return fmt.Errorf("listen gRPC: %w", err)
		}
		defer func() { _ = grpcListener.Close() }()
		grpcServer = grpc.NewServer(grpc.ChainUnaryInterceptor(grpcinterceptors.Logging(logger)))
		healthServer = health.NewServer()
		healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
		healthpb.RegisterHealthServer(grpcServer, healthServer)
	}
	httpServer := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	errs := make(chan error, 2)
	go func() { errs <- httpServer.Serve(httpListener) }()
	if grpcServer != nil {
		go func() { errs <- grpcServer.Serve(grpcListener) }()
	}
	logger.Info("server started", "http_addr", httpAddr, "grpc_addr", grpcAddr)
	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-errs:
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if healthServer != nil {
		healthServer.Shutdown()
	}
	if grpcServer != nil {
		stopped := make(chan struct{})
		go func() { grpcServer.GracefulStop(); close(stopped) }()
		select {
		case <-stopped:
		case <-shutdownCtx.Done():
			grpcServer.Stop()
		}
	}
	shutdownErr := httpServer.Shutdown(shutdownCtx)
	if shutdownErr != nil {
		_ = httpServer.Close()
	}
	if errors.Is(serveErr, http.ErrServerClosed) || errors.Is(serveErr, grpc.ErrServerStopped) {
		serveErr = nil
	}
	logger.Info("server stopped")
	return errors.Join(serveErr, shutdownErr)
}
