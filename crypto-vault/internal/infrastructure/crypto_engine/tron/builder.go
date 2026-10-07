package tron

import (
	"context"
	"fmt"
	"math/big"

	tronpb "github.com/renegadik/crypto-payment-gateway/pkg/proto/tron/protocol/v1"
	"golang.org/x/crypto/sha3"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/renegadik/crypto-payment-gateway/crypto-vault/internal/domain"
)

const (
	transferContractTypeURL     = "type.googleapis.com/protocol.TransferContract"
	triggerSmartContractTypeURL = "type.googleapis.com/protocol.TriggerSmartContract"
	tronAddressSize             = 21
	trc20CallDataSize           = 4 + 32 + 32
)

var trc20TransferSelector = functionSelector("transfer(address,uint256)")

// Builder строит неподписанные транзакции TRON без обращения к приватным ключам.
type Builder struct{}

var _ domain.Builder = (*Builder)(nil)

// Build проверяет намерение перевода и сетевой контекст, строит один разрешённый
// контракт и возвращает детерминированно сериализованную транзакцию TRON.
func (*Builder) Build(
	ctx context.Context,
	intent domain.TransferIntent,
	networkContext domain.NetworkContext,
) (domain.UnsignedTransaction, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if intent.Amount == nil || intent.Amount.Sign() <= 0 {
		return nil, fmt.Errorf("TRON transfer amount: %w", domain.ErrInvalidTransferIntent)
	}

	from, err := decodeAddress(intent.From)
	if err != nil {
		return nil, fmt.Errorf("TRON sender address: %w", domain.ErrInvalidTransferIntent)
	}
	to, err := decodeAddress(intent.To)
	if err != nil {
		return nil, fmt.Errorf("TRON recipient address: %w", domain.ErrInvalidTransferIntent)
	}
	transactionContext, err := decodeContext(networkContext)
	if err != nil {
		return nil, err
	}

	contract, feeLimit, err := buildContract(intent, from, to, transactionContext.feeLimitSun)
	if err != nil {
		return nil, err
	}

	transaction := &tronpb.Transaction{
		RawData: &tronpb.Transaction_Raw{
			RefBlockBytes: transactionContext.refBlockBytes,
			RefBlockHash:  transactionContext.refBlockHash,
			Expiration:    transactionContext.expirationMS,
			Contract:      []*tronpb.Transaction_Contract{contract},
			Timestamp:     transactionContext.timestampMS,
			FeeLimit:      feeLimit,
		},
	}

	encoded, err := marshalDeterministic(transaction)
	if err != nil {
		return nil, fmt.Errorf("encode unsigned TRON transaction: %w", domain.ErrBuildTransactionFailed)
	}

	return domain.UnsignedTransaction(encoded), nil
}

// buildContract строит нативный TransferContract либо TRC-20 вызов
// TriggerSmartContract и возвращает допустимый для него fee_limit.
func buildContract(
	intent domain.TransferIntent,
	from []byte,
	to []byte,
	feeLimitSun int64,
) (*tronpb.Transaction_Contract, int64, error) {
	if intent.Asset == nil {
		if !intent.Amount.IsInt64() {
			return nil, 0, fmt.Errorf("TRX amount exceeds int64: %w", domain.ErrInvalidTransferIntent)
		}

		payload, err := marshalDeterministic(&tronpb.TransferContract{
			OwnerAddress: from,
			ToAddress:    to,
			Amount:       intent.Amount.Int64(),
		})
		if err != nil {
			return nil, 0, fmt.Errorf("encode TRX transfer: %w", domain.ErrBuildTransactionFailed)
		}

		return &tronpb.Transaction_Contract{
			Type: tronpb.Transaction_Contract_CONTRACT_TYPE_TRANSFER_CONTRACT,
			Parameter: &anypb.Any{
				TypeUrl: transferContractTypeURL,
				Value:   payload,
			},
		}, 0, nil
	}

	if intent.Amount.BitLen() > 256 {
		return nil, 0, fmt.Errorf("TRC-20 amount exceeds uint256: %w", domain.ErrInvalidTransferIntent)
	}
	if feeLimitSun <= 0 {
		return nil, 0, fmt.Errorf("TRC-20 fee limit: %w", domain.ErrInvalidNetworkContext)
	}

	contractAddress, err := decodeAddress(domain.Address(*intent.Asset))
	if err != nil {
		return nil, 0, fmt.Errorf("TRC-20 contract address: %w", domain.ErrInvalidTransferIntent)
	}
	payload, err := marshalDeterministic(&tronpb.TriggerSmartContract{
		OwnerAddress:    from,
		ContractAddress: contractAddress,
		Data:            encodeTRC20Transfer(to, intent.Amount),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("encode TRC-20 transfer: %w", domain.ErrBuildTransactionFailed)
	}

	return &tronpb.Transaction_Contract{
		Type: tronpb.Transaction_Contract_CONTRACT_TYPE_TRIGGER_SMART_CONTRACT,
		Parameter: &anypb.Any{
			TypeUrl: triggerSmartContractTypeURL,
			Value:   payload,
		},
	}, feeLimitSun, nil
}

// encodeTRC20Transfer кодирует вызов transfer(address,uint256) по правилам ABI.
func encodeTRC20Transfer(to []byte, amount *big.Int) []byte {
	data := make([]byte, trc20CallDataSize)
	copy(data[:4], trc20TransferSelector)
	copy(data[4+12:4+32], to[1:])
	amount.FillBytes(data[4+32:])
	return data
}

// functionSelector вычисляет первые четыре байта Keccak-256 от ABI-сигнатуры.
func functionSelector(signature string) []byte {
	hasher := sha3.NewLegacyKeccak256()
	_, _ = hasher.Write([]byte(signature))
	return hasher.Sum(nil)[:4]
}

// marshalDeterministic сериализует protobuf одинаково для одинаковых данных.
// Это необходимо, чтобы хеш подписываемого raw_data был воспроизводимым.
func marshalDeterministic(message proto.Message) ([]byte, error) {
	return (proto.MarshalOptions{Deterministic: true}).Marshal(message)
}
