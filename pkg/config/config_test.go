package config

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestLoad проверяет чтение и валидацию всех параметров конфигурации.
func TestLoad(t *testing.T) {
	tests := []struct {
		name, http, grpc, level string
		wantErr                 bool
	}{
		{name: "valid", http: ":8080", grpc: ":9080", level: "debug"},
		{name: "no grpc", http: "127.0.0.1:8080", level: "info"},
		{name: "bad http", http: "8080", level: "info", wantErr: true},
		{name: "bad grpc", http: ":8080", grpc: "bad", level: "info", wantErr: true},
		{name: "bad level", http: ":8080", level: "verbose", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HTTP_ADDR", tt.http)
			t.Setenv("GRPC_ADDR", tt.grpc)
			t.Setenv("LOG_LEVEL", tt.level)
			cfg, err := Load(":9999", "")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.http, cfg.HTTPAddr)
			require.Equal(t, tt.grpc, cfg.GRPCAddr)
			if tt.level == "debug" {
				require.Equal(t, slog.LevelDebug, cfg.LogLevel)
			}
		})
	}
}
