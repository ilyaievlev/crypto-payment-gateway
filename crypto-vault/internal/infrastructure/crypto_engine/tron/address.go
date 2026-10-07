package tron

import (
	"context"
	"fmt"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcutil/base58"
	"golang.org/x/crypto/sha3"

	"github.com/renegadik/crypto-payment-gateway/crypto-vault/internal/domain"
)

const (
	addressPrefix       = byte(0x41)
	addressPayloadSize  = 20
	base58AddressLength = 34
)

// Wallet выводит и проверяет адреса сети TRON.
type Wallet struct {
	keys *keyDeriver
}

var _ domain.Wallet = (*Wallet)(nil)

// DeriveAddress выводит TRON-адрес из дочернего ключа, указанного в key.
func (w *Wallet) DeriveAddress(ctx context.Context, key domain.KeyRef) (domain.Address, error) {
	var result domain.Address
	err := w.keys.withPrivateKey(ctx, key, func(privateKey *btcec.PrivateKey) error {
		result = encodeAddress(publicKeyAddress(privateKey.PubKey()))
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("derive TRON address: %w", err)
	}

	return result, nil
}

// ValidateAddress проверяет длину, сетевой префикс и Base58Check-сумму адреса.
func (*Wallet) ValidateAddress(value domain.Address) bool {
	_, err := decodeAddress(value)
	return err == nil
}

// publicKeyAddress преобразует secp256k1 public key в 21-байтовый TRON-адрес.
func publicKeyAddress(publicKey *btcec.PublicKey) []byte {
	uncompressed := publicKey.SerializeUncompressed()
	hasher := sha3.NewLegacyKeccak256()
	_, _ = hasher.Write(uncompressed[1:])
	digest := hasher.Sum(nil)

	result := make([]byte, 1+addressPayloadSize)
	result[0] = addressPrefix
	copy(result[1:], digest[len(digest)-addressPayloadSize:])
	return result
}

// encodeAddress кодирует бинарный TRON-адрес в строку Base58Check.
func encodeAddress(raw []byte) domain.Address {
	return domain.Address(base58.CheckEncode(raw[1:], raw[0]))
}

// decodeAddress проверяет строковый TRON-адрес и возвращает его бинарный вид.
func decodeAddress(value domain.Address) ([]byte, error) {
	if len(value) != base58AddressLength {
		return nil, domain.ErrInvalidAddress
	}

	payload, version, err := base58.CheckDecode(string(value))
	if err != nil || version != addressPrefix || len(payload) != addressPayloadSize {
		return nil, domain.ErrInvalidAddress
	}

	result := make([]byte, 1+len(payload))
	result[0] = version
	copy(result[1:], payload)

	return result, nil
}
