package tron

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
	tronpb "github.com/renegadik/crypto-payment-gateway/pkg/proto/tron/protocol/v1"
	"google.golang.org/protobuf/proto"

	"github.com/renegadik/crypto-payment-gateway/crypto-vault/internal/domain"
	"github.com/renegadik/crypto-payment-gateway/crypto-vault/internal/infrastructure/memzero"
)

const maxUnsignedTransactionSize = 1024 * 1024

// Signer проверяет и подписывает транзакции TRON производным приватным ключом.
type Signer struct {
	keys *keyDeriver
	now  func() time.Time
}

var _ domain.Signer = (*Signer)(nil)

// Sign разбирает недоверенную транзакцию, проверяет разрешённый контракт и owner,
// подписывает SHA-256 от raw_data и возвращает транзакцию с одной подписью.
func (s *Signer) Sign(
	ctx context.Context,
	rawTx domain.UnsignedTransaction,
	key domain.KeyRef,
) (domain.SignedTransaction, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(rawTx) == 0 || len(rawTx) > maxUnsignedTransactionSize {
		return nil, fmt.Errorf("TRON transaction size: %w", domain.ErrSigningFailed)
	}

	var transaction tronpb.Transaction
	if err := proto.Unmarshal(rawTx, &transaction); err != nil {
		return nil, fmt.Errorf("decode TRON transaction: %w", domain.ErrSigningFailed)
	}
	ownerAddress, err := validateUnsignedTransaction(&transaction, s.now())
	if err != nil {
		return nil, err
	}

	var signed domain.SignedTransaction
	err = s.keys.withPrivateKey(ctx, key, func(privateKey *btcec.PrivateKey) error {
		if !bytes.Equal(ownerAddress, publicKeyAddress(privateKey.PubKey())) {
			return fmt.Errorf("TRON signer does not own transaction: %w", domain.ErrSigningFailed)
		}

		rawData, marshalErr := marshalDeterministic(transaction.RawData)
		if marshalErr != nil {
			return fmt.Errorf("encode TRON signing payload: %w", domain.ErrSigningFailed)
		}
		digest := sha256.Sum256(rawData)
		compact := ecdsa.SignCompact(privateKey, digest[:], false)
		defer memzero.Bytes(compact)
		if len(compact) != 65 || compact[0] < 27 || compact[0]-27 > 1 {
			return fmt.Errorf("TRON recovery ID: %w", domain.ErrSigningFailed)
		}

		signature := make([]byte, 65)
		copy(signature[:64], compact[1:])
		signature[64] = compact[0] - 27
		transaction.Signature = [][]byte{signature}

		encoded, marshalErr := marshalDeterministic(&transaction)
		if marshalErr != nil {
			return fmt.Errorf("encode signed TRON transaction: %w", domain.ErrSigningFailed)
		}
		signed = domain.SignedTransaction(encoded)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("sign TRON transaction: %w", err)
	}

	return signed, nil
}

// validateUnsignedTransaction проверяет оболочку, TAPOS, время жизни и единственный
// разрешённый контракт, затем возвращает адрес владельца для проверки ключа.
func validateUnsignedTransaction(transaction *tronpb.Transaction, now time.Time) ([]byte, error) {
	if len(transaction.ProtoReflect().GetUnknown()) != 0 || transaction.RawData == nil {
		return nil, fmt.Errorf("invalid TRON transaction envelope: %w", domain.ErrSigningFailed)
	}
	if len(transaction.Signature) != 0 {
		return nil, fmt.Errorf("TRON transaction is already signed: %w", domain.ErrSigningFailed)
	}
	raw := transaction.RawData
	if len(raw.ProtoReflect().GetUnknown()) != 0 || len(raw.Contract) != 1 {
		return nil, fmt.Errorf("invalid TRON raw data: %w", domain.ErrSigningFailed)
	}
	if len(raw.RefBlockBytes) != 2 || len(raw.RefBlockHash) != 8 {
		return nil, fmt.Errorf("invalid TRON TAPOS reference: %w", domain.ErrSigningFailed)
	}
	if raw.Timestamp <= 0 || raw.Expiration <= raw.Timestamp || raw.Expiration <= now.UnixMilli() {
		return nil, fmt.Errorf("expired TRON transaction: %w", domain.ErrSigningFailed)
	}
	if raw.Expiration-raw.Timestamp > maxExpirationWindow.Milliseconds() {
		return nil, fmt.Errorf("invalid TRON expiration window: %w", domain.ErrSigningFailed)
	}

	contract := raw.Contract[0]
	if contract == nil || contract.Parameter == nil || contract.PermissionId != 0 {
		return nil, fmt.Errorf("unsupported TRON contract permission: %w", domain.ErrSigningFailed)
	}
	if len(contract.ProtoReflect().GetUnknown()) != 0 || len(contract.Parameter.ProtoReflect().GetUnknown()) != 0 {
		return nil, fmt.Errorf("unknown TRON contract fields: %w", domain.ErrSigningFailed)
	}

	switch contract.Type {
	case tronpb.Transaction_Contract_CONTRACT_TYPE_TRANSFER_CONTRACT:
		return validateTRXContract(contract.Parameter.TypeUrl, contract.Parameter.Value, raw.FeeLimit)
	case tronpb.Transaction_Contract_CONTRACT_TYPE_TRIGGER_SMART_CONTRACT:
		return validateTRC20Contract(contract.Parameter.TypeUrl, contract.Parameter.Value, raw.FeeLimit)
	default:
		return nil, fmt.Errorf("unsupported TRON contract type: %w", domain.ErrSigningFailed)
	}
}

// validateTRXContract строго проверяет нативный перевод TRX и возвращает owner.
func validateTRXContract(typeURL string, encoded []byte, feeLimit int64) ([]byte, error) {
	if typeURL != transferContractTypeURL || feeLimit != 0 {
		return nil, fmt.Errorf("invalid TRX contract metadata: %w", domain.ErrSigningFailed)
	}
	var transfer tronpb.TransferContract
	if err := proto.Unmarshal(encoded, &transfer); err != nil || len(transfer.ProtoReflect().GetUnknown()) != 0 {
		return nil, fmt.Errorf("decode TRX contract: %w", domain.ErrSigningFailed)
	}
	if !validRawAddress(transfer.OwnerAddress) || !validRawAddress(transfer.ToAddress) || transfer.Amount <= 0 {
		return nil, fmt.Errorf("invalid TRX transfer: %w", domain.ErrSigningFailed)
	}
	return transfer.OwnerAddress, nil
}

// validateTRC20Contract проверяет метаданные и ABI-вызов transfer(address,uint256),
// после чего возвращает адрес владельца контракта.
func validateTRC20Contract(typeURL string, encoded []byte, feeLimit int64) ([]byte, error) {
	if typeURL != triggerSmartContractTypeURL || feeLimit <= 0 {
		return nil, fmt.Errorf("invalid TRC-20 contract metadata: %w", domain.ErrSigningFailed)
	}
	var trigger tronpb.TriggerSmartContract
	if err := proto.Unmarshal(encoded, &trigger); err != nil || len(trigger.ProtoReflect().GetUnknown()) != 0 {
		return nil, fmt.Errorf("decode TRC-20 contract: %w", domain.ErrSigningFailed)
	}
	if !validRawAddress(trigger.OwnerAddress) || !validRawAddress(trigger.ContractAddress) {
		return nil, fmt.Errorf("invalid TRC-20 addresses: %w", domain.ErrSigningFailed)
	}
	if trigger.CallValue != 0 || trigger.CallTokenValue != 0 || trigger.TokenId != 0 {
		return nil, fmt.Errorf("unexpected TRC-20 call value: %w", domain.ErrSigningFailed)
	}
	if len(trigger.Data) != trc20CallDataSize || !bytes.Equal(trigger.Data[:4], trc20TransferSelector) {
		return nil, fmt.Errorf("unsupported TRC-20 call data: %w", domain.ErrSigningFailed)
	}
	if !bytes.Equal(trigger.Data[4:16], make([]byte, 12)) {
		return nil, fmt.Errorf("invalid TRC-20 recipient encoding: %w", domain.ErrSigningFailed)
	}
	if bytes.Equal(trigger.Data[36:], make([]byte, 32)) {
		return nil, fmt.Errorf("zero TRC-20 amount: %w", domain.ErrSigningFailed)
	}

	return trigger.OwnerAddress, nil
}

// validRawAddress проверяет длину и сетевой префикс бинарного TRON-адреса.
func validRawAddress(value []byte) bool {
	return len(value) == tronAddressSize && value[0] == addressPrefix
}
