package tron

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math/big"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/renegadik/crypto-payment-gateway/crypto-vault/internal/domain"
	cryptovaultv1 "github.com/renegadik/crypto-payment-gateway/pkg/proto/cryptovault/v1"
	tronpb "github.com/renegadik/crypto-payment-gateway/pkg/proto/tron/protocol/v1"
)

const (
	testSeedHex   = "7ae6f661157bda6492f6162701e570097fc726b6235011ea5ad09bf04986731ed4d92bc43cbdee047b60ea0dd1b1fa4274377c9bf5bd14ab1982c272d8076f29"
	testAddress   = domain.Address("THJrqfbBhoB1vX97da6S6nXWkafCxpyCNB")
	testRecipient = domain.Address("TXLAQ63Xg1NAzckPwKHvzw7CSEmLMEqcdj")
	testToken     = domain.AssetID("TXLAQ63Xg1NAzckPwKHvzw7CSEmLMEqcdj")
)

type staticSeedProvider struct {
	seed []byte
}

// WithSeed передаёт тесту копию фиксированного сида и очищает её после callback.
func (p staticSeedProvider) WithSeed(
	ctx context.Context,
	network domain.Network,
	use func([]byte) error,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if network != domain.NetworkTRON {
		return domain.ErrSeedUnavailable
	}
	seed := append([]byte(nil), p.seed...)
	defer clear(seed)
	return use(seed)
}

// newTestEngine создаёт TRON-движок с воспроизводимым тестовым сидом.
func newTestEngine(t *testing.T) *Engine {
	t.Helper()
	seed, err := hex.DecodeString(testSeedHex)
	require.NoError(t, err)
	engine, err := New(staticSeedProvider{seed: seed})
	require.NoError(t, err)
	return engine
}

// TestWalletDeriveAddressTrustWalletVector сверяет деривацию с независимым
// тест-вектором Trust Wallet и проверяет контрольную сумму адреса.
func TestWalletDeriveAddressTrustWalletVector(t *testing.T) {
	engine := newTestEngine(t)

	address, err := engine.wallet.DeriveAddress(context.Background(), domain.KeyRef{Index: 0})

	require.NoError(t, err)
	require.Equal(t, testAddress, address)
	require.True(t, engine.wallet.ValidateAddress(address))
	require.False(t, engine.wallet.ValidateAddress("THJrqfbBhoB1vX97da6S6nXWkafCxpyCNA"))
}

// TestBuilderBuild проверяет wire-поля нативной и TRC-20 транзакций.
func TestBuilderBuild(t *testing.T) {
	engine := newTestEngine(t)
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	contextBytes := testContext(t, now)

	tests := []struct {
		name            string
		asset           *domain.AssetID
		amount          *big.Int
		wantType        tronpb.Transaction_Contract_ContractType
		wantFeeLimit    int64
		wantContractURL string
	}{
		{
			name:            "native TRX",
			amount:          big.NewInt(1_500_000),
			wantType:        tronpb.Transaction_Contract_CONTRACT_TYPE_TRANSFER_CONTRACT,
			wantContractURL: transferContractTypeURL,
		},
		{
			name:            "TRC-20",
			asset:           ptr(testToken),
			amount:          big.NewInt(25_000_000),
			wantType:        tronpb.Transaction_Contract_CONTRACT_TYPE_TRIGGER_SMART_CONTRACT,
			wantFeeLimit:    100_000_000,
			wantContractURL: triggerSmartContractTypeURL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := engine.builder.Build(context.Background(), domain.TransferIntent{
				From: testAddress, To: testRecipient, Amount: tt.amount, Asset: tt.asset,
			}, contextBytes)
			require.NoError(t, err)

			var transaction tronpb.Transaction
			require.NoError(t, proto.Unmarshal(encoded, &transaction))
			require.Len(t, transaction.RawData.Contract, 1)
			require.Equal(t, tt.wantType, transaction.RawData.Contract[0].Type)
			require.Equal(t, tt.wantContractURL, transaction.RawData.Contract[0].Parameter.TypeUrl)
			require.Equal(t, tt.wantFeeLimit, transaction.RawData.FeeLimit)
			require.Equal(t, []byte{0x00, 0x2a}, transaction.RawData.RefBlockBytes)
			require.Equal(t, bytes.Repeat([]byte{0x7b}, 8), transaction.RawData.RefBlockHash)
		})
	}
}

// TestSignerSignsAndRecoversOwner проверяет подпись через восстановление public key.
func TestSignerSignsAndRecoversOwner(t *testing.T) {
	engine := newTestEngine(t)
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	engine.signer.now = func() time.Time { return now }
	unsigned, err := engine.builder.Build(context.Background(), domain.TransferIntent{
		From: testAddress, To: testRecipient, Amount: big.NewInt(1_000_000),
	}, testContext(t, now))
	require.NoError(t, err)

	signed, err := engine.signer.Sign(context.Background(), unsigned, domain.KeyRef{Index: 0})
	require.NoError(t, err)

	var transaction tronpb.Transaction
	require.NoError(t, proto.Unmarshal(signed, &transaction))
	require.Len(t, transaction.Signature, 1)
	signature := transaction.Signature[0]
	require.Len(t, signature, 65)

	rawData, err := marshalDeterministic(transaction.RawData)
	require.NoError(t, err)
	digest := sha256.Sum256(rawData)
	compact := make([]byte, 65)
	compact[0] = signature[64] + 27
	copy(compact[1:], signature[:64])
	publicKey, _, err := ecdsa.RecoverCompact(compact, digest[:])
	require.NoError(t, err)
	require.Equal(t, testAddress, encodeAddress(publicKeyAddress(publicKey)))
}

// TestSignerRejectsWrongKey проверяет отказ при несовпадении owner и дочернего ключа.
func TestSignerRejectsWrongKey(t *testing.T) {
	engine := newTestEngine(t)
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	engine.signer.now = func() time.Time { return now }
	unsigned, err := engine.builder.Build(context.Background(), domain.TransferIntent{
		From: testAddress, To: testRecipient, Amount: big.NewInt(1),
	}, testContext(t, now))
	require.NoError(t, err)

	_, err = engine.signer.Sign(context.Background(), unsigned, domain.KeyRef{Index: 1})

	require.ErrorIs(t, err, domain.ErrSigningFailed)
}

// TestSignerRejectsUnsafeTransactions проверяет защиту от подмены транзакции
// между этапами построения и подписи.
func TestSignerRejectsUnsafeTransactions(t *testing.T) {
	engine := newTestEngine(t)
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	engine.signer.now = func() time.Time { return now }
	unsigned, err := engine.builder.Build(context.Background(), domain.TransferIntent{
		From: testAddress, To: testRecipient, Amount: big.NewInt(1_000_000),
	}, testContext(t, now))
	require.NoError(t, err)

	var valid tronpb.Transaction
	require.NoError(t, proto.Unmarshal(unsigned, &valid))

	tests := []struct {
		name   string
		mutate func(*tronpb.Transaction)
	}{
		{
			name: "already signed",
			mutate: func(transaction *tronpb.Transaction) {
				transaction.Signature = [][]byte{{1}}
			},
		},
		{
			name: "expired",
			mutate: func(transaction *tronpb.Transaction) {
				transaction.RawData.Expiration = now.Add(-time.Millisecond).UnixMilli()
			},
		},
		{
			name: "multiple contracts",
			mutate: func(transaction *tronpb.Transaction) {
				transaction.RawData.Contract = append(
					transaction.RawData.Contract,
					proto.Clone(transaction.RawData.Contract[0]).(*tronpb.Transaction_Contract),
				)
			},
		},
		{
			name: "substituted type URL",
			mutate: func(transaction *tronpb.Transaction) {
				transaction.RawData.Contract[0].Parameter.TypeUrl = "type.googleapis.com/protocol.AccountCreateContract"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transaction := proto.Clone(&valid).(*tronpb.Transaction)
			tt.mutate(transaction)
			tampered, marshalErr := marshalDeterministic(transaction)
			require.NoError(t, marshalErr)

			_, signErr := engine.signer.Sign(context.Background(), tampered, domain.KeyRef{Index: 0})

			require.ErrorIs(t, signErr, domain.ErrSigningFailed)
		})
	}
}

// testContext создаёт корректный воспроизводимый сетевой контекст TRON.
func testContext(t *testing.T, now time.Time) domain.NetworkContext {
	t.Helper()
	blockID := bytes.Repeat([]byte{0x7b}, 32)
	binary.BigEndian.PutUint64(blockID[:8], 42)
	encoded, err := proto.Marshal(&cryptovaultv1.TronContextV1{
		BlockNumber:  42,
		BlockId:      blockID,
		TimestampMs:  now.UnixMilli(),
		ExpirationMs: now.Add(time.Minute).UnixMilli(),
		FeeLimitSun:  100_000_000,
	})
	require.NoError(t, err)
	return domain.NetworkContext(encoded)
}

// ptr возвращает указатель на переданное значение для табличных тестов.
func ptr[T any](value T) *T { return &value }
