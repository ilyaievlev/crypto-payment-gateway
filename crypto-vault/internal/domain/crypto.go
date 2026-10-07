package domain

import (
	"context"
	"errors"
	"math/big"
)

var (
	ErrUnsupportedNetwork     = errors.New("unsupported network")
	ErrInvalidAddress         = errors.New("invalid blockchain address")
	ErrInvalidKeyRef          = errors.New("invalid key reference")
	ErrInvalidTransferIntent  = errors.New("invalid transfer intent")
	ErrInvalidNetworkContext  = errors.New("invalid network context")
	ErrSeedUnavailable        = errors.New("root seed unavailable")
	ErrDerivationFailed       = errors.New("failed to derive key or address")
	ErrBuildTransactionFailed = errors.New("failed to build transaction")
	ErrSigningFailed          = errors.New("failed to sign transaction")
)

// Network определяет блокчейн-сеть, поддерживаемую Crypto Vault.
type Network string

const (
	NetworkETH  Network = "ETH"
	NetworkTRON Network = "TRON"
	NetworkSOL  Network = "SOL"
	NetworkTON  Network = "TON"
)

// Valid сообщает, известна ли сеть доменному слою.
// Наличие значения в этом списке не гарантирует, что движок сети уже реализован.
func (n Network) Valid() bool {
	switch n {
	case NetworkETH, NetworkTRON, NetworkSOL, NetworkTON:
		return true
	default:
		return false
	}
}

// Address хранит блокчейн-адрес в формате конкретной сети.
type Address string

// AssetID идентифицирует токен внутри блокчейн-сети.
//
// Примеры:
//
//	ETH  -> адрес контракта ERC-20
//	TRON -> адрес контракта TRC-20
//	SOL  -> адрес минта SPL-токена
//	TON  -> адрес мастер-контракта Jetton
//
// nil в поле TransferIntent.Asset означает нативную монету сети.
type AssetID string

// NetworkContext содержит сериализованные данные конкретной сети, необходимые
// для построения транзакции. Формат версионируется отдельно для каждой сети.
// Контекст никогда не должен содержать приватные ключи или сид-фразы.
type NetworkContext []byte

// UnsignedTransaction — сериализованная неподписанная транзакция от Builder.
type UnsignedTransaction []byte

// SignedTransaction — сериализованная транзакция, готовая к отправке в сеть.
type SignedTransaction []byte

// TransferIntent содержит не зависящие от сети параметры перевода.
// Сетевые данные, например nonce, gas, blockhash, ref block и seqno,
// передаются отдельно через NetworkContext.
//
// Amount задаётся в минимальных единицах актива. Значение должно быть
// положительным и не должно изменяться во время операции. nil в Asset означает
// перевод нативной монеты сети.
type TransferIntent struct {
	From   Address
	To     Address
	Amount *big.Int
	Asset  *AssetID
}

// MaxDerivationIndex — максимальный индекс дочернего ключа, принимаемый от
// прикладного кода. Конкретный движок определяет, используется ли индекс как
// hardened- или обычный компонент пути деривации.
const MaxDerivationIndex uint32 = 1<<31 - 1

// KeyRef указывает на детерминированный дочерний ключ единственного корневого
// кошелька сети. Приватный ключ не покидает границы Crypto Vault.
type KeyRef struct {
	Index uint32
}

// Valid проверяет, допустим ли индекс для использования в пути деривации.
func (r KeyRef) Valid() bool {
	return r.Index <= MaxDerivationIndex
}

// SeedProvider предоставляет временный доступ к корневому сиду одной сети.
// Callback не должен сохранять или изменять полученный seed. Буфер принадлежит
// провайдеру и должен быть очищен сразу после завершения callback.
type SeedProvider interface {
	// WithSeed расшифровывает сид выбранной сети на время выполнения use,
	// а после возврата из use очищает временный буфер.
	WithSeed(
		ctx context.Context,
		network Network,
		use func(seed []byte) error,
	) error
}

// Engine объединяет криптографические компоненты ровно одной сети.
type Engine interface {
	// Network возвращает сеть, которую обслуживает движок.
	Network() Network
	// Wallet возвращает компонент работы с адресами.
	Wallet() Wallet
	// Builder возвращает компонент построения транзакций.
	Builder() Builder
	// Signer возвращает компонент подписи транзакций.
	Signer() Signer
}

// Wallet выводит адреса из корневого сида сети и проверяет внешние адреса.
// Корневые сиды и приватные ключи остаются внутри Crypto Vault.
type Wallet interface {
	// DeriveAddress детерминированно выводит адрес по ссылке на дочерний ключ.
	DeriveAddress(ctx context.Context, key KeyRef) (Address, error)

	// ValidateAddress проверяет формат адреса и контрольную сумму, если она
	// предусмотрена форматом сети.
	ValidateAddress(address Address) bool
}

// Builder строит неподписанные транзакции без доступа к приватным ключам.
// Реализация обязана считать intent и networkContext недоверенными данными,
// проверять их содержимое и отклонять неизвестные версии контекста.
//
// Типичные данные контекста:
//
//	ETH  -> nonce, chain ID и параметры комиссии EIP-1559
//	TRON -> опорный блок, срок действия и лимит комиссии
//	SOL  -> недавний blockhash
//	TON  -> seqno, версия кошелька и параметры wallet-контракта
type Builder interface {
	// Build проверяет параметры перевода и строит неподписанную транзакцию.
	Build(
		ctx context.Context,
		intent TransferIntent,
		networkContext NetworkContext,
	) (UnsignedTransaction, error)
}

// Signer подписывает транзакцию дочерним приватным ключом из KeyRef.
// Реализация обязана декодировать и проверить rawTx перед подписью: произвольные
// байты нельзя подписывать вслепую. Производные приватные ключи очищаются сразу
// после использования. Из Crypto Vault выходит только подписанная транзакция.
type Signer interface {
	// Sign проверяет транзакцию и подписывает её дочерним ключом из key.
	Sign(
		ctx context.Context,
		rawTx UnsignedTransaction,
		key KeyRef,
	) (SignedTransaction, error)
}
