// Package cryptoengine выбирает криптографический движок для заданной сети.
package cryptoengine

import (
	"fmt"

	"github.com/renegadik/crypto-payment-gateway/crypto-vault/internal/domain"
	"github.com/renegadik/crypto-payment-gateway/crypto-vault/internal/infrastructure/crypto_engine/tron"
)

// Factory создаёт сетевые движки с общим защищённым провайдером сидов.
type Factory struct {
	seeds domain.SeedProvider
}

// NewFactory создаёт фабрику сетевых движков и проверяет наличие провайдера сидов.
func NewFactory(seeds domain.SeedProvider) (*Factory, error) {
	if seeds == nil {
		return nil, fmt.Errorf("create crypto engine factory: %w", domain.ErrSeedUnavailable)
	}

	return &Factory{seeds: seeds}, nil
}

// Engine возвращает реализацию для указанной сети или ErrUnsupportedNetwork,
// если движок этой сети ещё не зарегистрирован.
func (f *Factory) Engine(network domain.Network) (domain.Engine, error) {
	switch network {
	case domain.NetworkTRON:
		return tron.New(f.seeds)
	case domain.NetworkETH, domain.NetworkSOL, domain.NetworkTON:
		return nil, fmt.Errorf("create %s engine: %w", network, domain.ErrUnsupportedNetwork)
	default:
		return nil, fmt.Errorf("create engine for %q: %w", network, domain.ErrUnsupportedNetwork)
	}
}
