package seedvault

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tyler-smith/go-bip39"

	"github.com/renegadik/crypto-payment-gateway/crypto-vault/internal/domain"
)

type memorySeedRepository struct {
	mu      sync.Mutex
	seeds   map[domain.Network][]byte
	created bool
}

// LoadEncryptedSeed возвращает копию ciphertext тестового хранилища.
func (r *memorySeedRepository) LoadEncryptedSeed(_ context.Context, network domain.Network) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ciphertext, exists := r.seeds[network]
	if !exists {
		return nil, domain.ErrSeedUnavailable
	}
	return append([]byte(nil), ciphertext...), nil
}

// CreateEncryptedSeed сохраняет ciphertext один раз, имитируя уникальный ключ БД.
func (r *memorySeedRepository) CreateEncryptedSeed(_ context.Context, network domain.Network, ciphertext []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.seeds[network]; exists {
		return domain.ErrSeedAlreadyInitialized
	}
	r.seeds[network] = append([]byte(nil), ciphertext...)
	r.created = true
	return nil
}

// TestInitializeAndUseSeed проверяет инициализацию, callback и очистку открытого сида.
func TestInitializeAndUseSeed(t *testing.T) {
	repository := &memorySeedRepository{seeds: make(map[domain.Network][]byte)}
	key := make([]byte, seedKeySize)
	for i := range key {
		key[i] = byte(i + 1)
	}
	provider, err := NewProvider(repository, key)
	require.NoError(t, err)
	defer provider.Close()

	phrase, err := provider.InitializeTRON(context.Background())
	require.NoError(t, err)
	require.Equal(t, 12, len(splitPhrase(phrase)))
	require.True(t, bip39.IsMnemonicValid(phrase))
	require.True(t, repository.created)
	seedForComparison := bip39.NewSeed(phrase, "")
	defer clear(seedForComparison)
	ciphertext := repository.seeds[domain.NetworkTRON]
	require.False(t, bytes.Contains(ciphertext, seedForComparison))

	var callbackSeed []byte
	err = provider.WithSeed(context.Background(), domain.NetworkTRON, func(seed []byte) error {
		callbackSeed = seed
		require.Equal(t, 64, len(seed))
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, callbackSeed)
	require.Equal(t, make([]byte, len(callbackSeed)), callbackSeed)

	err = provider.Check(context.Background())
	require.NoError(t, err)
	_, err = provider.InitializeTRON(context.Background())
	require.ErrorIs(t, err, domain.ErrSeedAlreadyInitialized)
}

// TestWithSeedRejectsTamperedCiphertext проверяет аутентификацию AES-GCM.
func TestWithSeedRejectsTamperedCiphertext(t *testing.T) {
	repository := &memorySeedRepository{seeds: make(map[domain.Network][]byte)}
	provider, err := NewProvider(repository, make([]byte, seedKeySize))
	require.NoError(t, err)
	defer provider.Close()
	_, err = provider.InitializeTRON(context.Background())
	require.NoError(t, err)
	repository.seeds[domain.NetworkTRON][len(repository.seeds[domain.NetworkTRON])-1] ^= 0xff

	err = provider.WithSeed(context.Background(), domain.NetworkTRON, func([]byte) error {
		return errors.New("callback должен быть недостижим")
	})

	require.ErrorIs(t, err, domain.ErrSeedUnavailable)
}

// splitPhrase делит мнемоническую фразу на слова для проверки длины.
func splitPhrase(phrase string) []string {
	words := make([]string, 0, 12)
	start := 0
	for i, value := range phrase {
		if value == ' ' {
			words = append(words, phrase[start:i])
			start = i + 1
		}
	}
	return append(words, phrase[start:])
}
