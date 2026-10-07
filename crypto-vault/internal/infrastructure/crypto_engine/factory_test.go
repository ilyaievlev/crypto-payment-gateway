package cryptoengine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/renegadik/crypto-payment-gateway/crypto-vault/internal/domain"
)

type testSeedProvider struct{}

// WithSeed реализует тестовый провайдер без выдачи реального сида.
func (testSeedProvider) WithSeed(
	context.Context,
	domain.Network,
	func([]byte) error,
) error {
	return nil
}

// TestFactoryEngine проверяет регистрацию TRON и отказ для остальных сетей.
func TestFactoryEngine(t *testing.T) {
	factory, err := NewFactory(testSeedProvider{})
	require.NoError(t, err)

	tests := []struct {
		name    string
		network domain.Network
		wantErr error
	}{
		{name: "tron", network: domain.NetworkTRON},
		{name: "ethereum not implemented", network: domain.NetworkETH, wantErr: domain.ErrUnsupportedNetwork},
		{name: "unknown", network: "UNKNOWN", wantErr: domain.ErrUnsupportedNetwork},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, engineErr := factory.Engine(tt.network)
			if tt.wantErr != nil {
				require.ErrorIs(t, engineErr, tt.wantErr)
				require.Nil(t, engine)
				return
			}

			require.NoError(t, engineErr)
			require.Equal(t, tt.network, engine.Network())
		})
	}
}

// TestNewFactoryRejectsNilProvider проверяет обязательность провайдера сидов.
func TestNewFactoryRejectsNilProvider(t *testing.T) {
	factory, err := NewFactory(nil)

	require.ErrorIs(t, err, domain.ErrSeedUnavailable)
	require.Nil(t, factory)
}
