package tron

import (
	"context"
	"fmt"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcutil/hdkeychain"
	"github.com/btcsuite/btcd/chaincfg"

	"github.com/renegadik/crypto-payment-gateway/crypto-vault/internal/domain"
)

const (
	bip44Purpose = uint32(44)
	tronCoinType = uint32(195)
)

// keyDeriver получает сетевой сид и выводит из него дочерние приватные ключи.
type keyDeriver struct {
	seeds domain.SeedProvider
}

// withPrivateKey выводит приватный ключ по пути m/44'/195'/0'/0/index,
// передаёт его callback-функции и очищает все промежуточные ключи после работы.
func (d *keyDeriver) withPrivateKey(
	ctx context.Context,
	keyRef domain.KeyRef,
	use func(*btcec.PrivateKey) error,
) error {
	if !keyRef.Valid() {
		return domain.ErrInvalidKeyRef
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	err := d.seeds.WithSeed(ctx, domain.NetworkTRON, func(seed []byte) error {
		if len(seed) < hdkeychain.MinSeedBytes || len(seed) > hdkeychain.MaxSeedBytes {
			return fmt.Errorf("TRON seed length: %w", domain.ErrDerivationFailed)
		}

		master, err := hdkeychain.NewMaster(seed, &chaincfg.MainNetParams)
		if err != nil {
			return fmt.Errorf("create TRON master key: %w", domain.ErrDerivationFailed)
		}
		defer master.Zero()

		path := [...]uint32{
			hdkeychain.HardenedKeyStart + bip44Purpose,
			hdkeychain.HardenedKeyStart + tronCoinType,
			hdkeychain.HardenedKeyStart,
			0,
			keyRef.Index,
		}

		current := master
		for _, index := range path {
			if err := ctx.Err(); err != nil {
				if current != master {
					current.Zero()
				}
				return err
			}

			child, deriveErr := current.Derive(index)
			if current != master {
				current.Zero()
			}
			if deriveErr != nil {
				return fmt.Errorf("derive TRON child key: %w", domain.ErrDerivationFailed)
			}
			current = child
		}
		defer current.Zero()

		privateKey, err := current.ECPrivKey()
		if err != nil {
			return fmt.Errorf("extract TRON private key: %w", domain.ErrDerivationFailed)
		}
		defer privateKey.Zero()

		return use(privateKey)
	})
	if err != nil {
		return fmt.Errorf("access TRON seed: %w", err)
	}

	return nil
}
