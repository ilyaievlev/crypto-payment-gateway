// Package tron реализует локальную деривацию адресов, построение и подпись
// транзакций сети TRON.
package tron

import (
	"fmt"
	"time"

	"github.com/renegadik/crypto-payment-gateway/crypto-vault/internal/domain"
)

// Engine объединяет кошелёк, построитель транзакций и подписант сети TRON.
type Engine struct {
	wallet  *Wallet
	builder *Builder
	signer  *Signer
}

var _ domain.Engine = (*Engine)(nil)

// New создаёт TRON-движок, получающий корневой сид через seeds.
func New(seeds domain.SeedProvider) (*Engine, error) {
	if seeds == nil {
		return nil, fmt.Errorf("create TRON engine: %w", domain.ErrSeedUnavailable)
	}

	keys := &keyDeriver{seeds: seeds}
	wallet := &Wallet{keys: keys}

	return &Engine{
		wallet:  wallet,
		builder: &Builder{},
		signer:  &Signer{keys: keys, now: time.Now},
	}, nil
}

// Network возвращает идентификатор сети, обслуживаемой движком.
func (*Engine) Network() domain.Network { return domain.NetworkTRON }

// Wallet возвращает компонент деривации и проверки адресов.
func (e *Engine) Wallet() domain.Wallet { return e.wallet }

// Builder возвращает компонент построения неподписанных транзакций.
func (e *Engine) Builder() domain.Builder { return e.builder }

// Signer возвращает компонент проверки и подписи транзакций.
func (e *Engine) Signer() domain.Signer { return e.signer }
