package seedvault

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"sync"

	"github.com/tyler-smith/go-bip39"

	"github.com/renegadik/crypto-payment-gateway/crypto-vault/internal/domain"
	"github.com/renegadik/crypto-payment-gateway/crypto-vault/internal/infrastructure/memzero"
)

const (
	seedCipherVersion = byte(1)
	seedKeySize       = 32
	seedNonceSize     = 12
	seedAADPrefix     = "crypto-vault/root-seed/v1/"
)

// Provider расшифровывает корневой сид только на время callback SeedProvider.
type Provider struct {
	mu         sync.RWMutex
	repository domain.SeedRepository
	key        []byte
}

var _ domain.SeedProvider = (*Provider)(nil)

// OpenProvider открывает локальный ключ с помощью пароля из отдельного файла.
// Если createKey равен true, отсутствующий ключ создаётся без перезаписи.
func OpenProvider(
	repository domain.SeedRepository,
	keyPath string,
	passwordPath string,
	createKey bool,
) (*Provider, error) {
	var (
		key []byte
		err error
	)
	if createKey {
		key, err = LoadOrCreateKeyFromPasswordFile(keyPath, passwordPath)
	} else {
		key, err = LoadKeyFromPasswordFile(keyPath, passwordPath)
	}
	if err != nil {
		return nil, err
	}
	defer memzero.Bytes(key)
	return NewProvider(repository, key)
}

// NewProvider создаёт провайдер и копирует ключ данных в защищённое состояние.
func NewProvider(repository domain.SeedRepository, key []byte) (*Provider, error) {
	if repository == nil || len(key) != seedKeySize {
		return nil, fmt.Errorf("репозиторий или ключ Crypto Vault некорректен")
	}
	return &Provider{repository: repository, key: append([]byte(nil), key...)}, nil
}

// InitializeTRON создаёт BIP-39 фразу из 12 слов и сохраняет соответствующий
// BIP-39 seed в зашифрованном виде. Фразу вызывает возвращаемое значение один раз.
func (p *Provider) InitializeTRON(ctx context.Context) (string, error) {
	entropy, err := bip39.NewEntropy(128)
	if err != nil {
		return "", fmt.Errorf("создать энтропию TRON-кошелька: %w", err)
	}
	defer memzero.Bytes(entropy)

	mnemonic, err := bip39.NewMnemonic(entropy)
	if err != nil {
		return "", fmt.Errorf("создать мнемоническую фразу TRON: %w", err)
	}
	seed := bip39.NewSeed(mnemonic, "")
	defer memzero.Bytes(seed)

	if err := p.storeSeed(ctx, domain.NetworkTRON, seed); err != nil {
		return "", err
	}
	return mnemonic, nil
}

// WithSeed загружает ciphertext из PostgreSQL, расшифровывает его на время use
// и очищает открытый сид после завершения callback.
func (p *Provider) WithSeed(
	ctx context.Context,
	network domain.Network,
	use func(seed []byte) error,
) error {
	if use == nil || !network.Valid() {
		return domain.ErrSeedUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	encrypted, err := p.repository.LoadEncryptedSeed(ctx, network)
	if err != nil {
		return fmt.Errorf("загрузить зашифрованный сид: %w", err)
	}
	if len(encrypted) < 1+seedNonceSize+16 || encrypted[0] != seedCipherVersion {
		return fmt.Errorf("повреждённый зашифрованный сид: %w", domain.ErrSeedUnavailable)
	}

	p.mu.RLock()
	defer p.mu.RUnlock()
	if len(p.key) != seedKeySize {
		return domain.ErrSeedUnavailable
	}
	block, err := aes.NewCipher(p.key)
	if err != nil {
		return fmt.Errorf("создать AES-шифр сида: %w", domain.ErrSeedUnavailable)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("создать GCM сида: %w", domain.ErrSeedUnavailable)
	}
	nonceStart := 1
	nonceEnd := nonceStart + seedNonceSize
	seed, err := aead.Open(nil, encrypted[nonceStart:nonceEnd], encrypted[nonceEnd:], seedAAD(network))
	if err != nil {
		return fmt.Errorf("расшифровать сид сети %s: %w", network, domain.ErrSeedUnavailable)
	}
	defer memzero.Bytes(seed)
	if err := ctx.Err(); err != nil {
		return err
	}
	return use(seed)
}

// Check проверяет доступность репозитория и наличие зашифрованного TRON-сида.
func (p *Provider) Check(ctx context.Context) error {
	_, err := p.repository.LoadEncryptedSeed(ctx, domain.NetworkTRON)
	return err
}

// Close очищает ключ данных из памяти после остановки сервиса.
func (p *Provider) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	memzero.Bytes(p.key)
	p.key = nil
}

// storeSeed шифрует сид с привязкой к сети и сохраняет его без возможности замены.
func (p *Provider) storeSeed(ctx context.Context, network domain.Network, seed []byte) error {
	if !network.Valid() || len(seed) < 16 || len(seed) > 64 {
		return domain.ErrSeedUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if len(p.key) != seedKeySize {
		return domain.ErrSeedUnavailable
	}
	block, err := aes.NewCipher(p.key)
	if err != nil {
		return fmt.Errorf("создать AES-шифр сида: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("создать GCM сида: %w", err)
	}
	nonce := make([]byte, seedNonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("создать nonce сида: %w", err)
	}
	ciphertext := make([]byte, 1, 1+seedNonceSize+len(seed)+aead.Overhead())
	ciphertext[0] = seedCipherVersion
	ciphertext = append(ciphertext, nonce...)
	ciphertext = aead.Seal(ciphertext, nonce, seed, seedAAD(network))
	defer memzero.Bytes(ciphertext)
	if err := p.repository.CreateEncryptedSeed(ctx, network, ciphertext); err != nil {
		return fmt.Errorf("сохранить зашифрованный сид: %w", err)
	}
	return nil
}

// seedAAD возвращает контекст аутентификации, привязанный к конкретной сети.
func seedAAD(network domain.Network) []byte {
	return []byte(seedAADPrefix + string(network))
}
