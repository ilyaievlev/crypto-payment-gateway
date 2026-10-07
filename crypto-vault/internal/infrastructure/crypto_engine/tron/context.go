package tron

import (
	"encoding/binary"
	"fmt"
	"time"

	cryptovaultv1 "github.com/renegadik/crypto-payment-gateway/pkg/proto/cryptovault/v1"
	"google.golang.org/protobuf/proto"

	"github.com/renegadik/crypto-payment-gateway/crypto-vault/internal/domain"
)

const (
	tronBlockIDSize     = 32
	maxExpirationWindow = 24 * time.Hour
)

// transactionContext хранит проверенные поля, которые записываются в raw_data.
type transactionContext struct {
	refBlockBytes []byte
	refBlockHash  []byte
	timestampMS   int64
	expirationMS  int64
	feeLimitSun   int64
}

// decodeContext декодирует и строго проверяет сетевой контекст TRON, после чего
// вычисляет поля TAPOS ref_block_bytes и ref_block_hash.
func decodeContext(raw domain.NetworkContext) (transactionContext, error) {
	var value cryptovaultv1.TronContextV1
	if err := proto.Unmarshal(raw, &value); err != nil {
		return transactionContext{}, fmt.Errorf("decode TRON context: %w", domain.ErrInvalidNetworkContext)
	}
	if len(value.ProtoReflect().GetUnknown()) != 0 {
		return transactionContext{}, fmt.Errorf("TRON context has unknown fields: %w", domain.ErrInvalidNetworkContext)
	}
	if len(value.BlockId) != tronBlockIDSize {
		return transactionContext{}, fmt.Errorf("TRON block ID length: %w", domain.ErrInvalidNetworkContext)
	}
	if binary.BigEndian.Uint64(value.BlockId[:8]) != value.BlockNumber {
		return transactionContext{}, fmt.Errorf("TRON block ID does not match block number: %w", domain.ErrInvalidNetworkContext)
	}
	if value.TimestampMs <= 0 || value.ExpirationMs <= value.TimestampMs {
		return transactionContext{}, fmt.Errorf("TRON transaction time range: %w", domain.ErrInvalidNetworkContext)
	}
	if value.ExpirationMs-value.TimestampMs > maxExpirationWindow.Milliseconds() {
		return transactionContext{}, fmt.Errorf("TRON expiration window: %w", domain.ErrInvalidNetworkContext)
	}
	if value.FeeLimitSun < 0 {
		return transactionContext{}, fmt.Errorf("TRON fee limit: %w", domain.ErrInvalidNetworkContext)
	}

	blockNumber := make([]byte, 8)
	binary.BigEndian.PutUint64(blockNumber, value.BlockNumber)

	return transactionContext{
		refBlockBytes: append([]byte(nil), blockNumber[6:8]...),
		refBlockHash:  append([]byte(nil), value.BlockId[8:16]...),
		timestampMS:   value.TimestampMs,
		expirationMS:  value.ExpirationMs,
		feeLimitSun:   value.FeeLimitSun,
	}, nil
}
