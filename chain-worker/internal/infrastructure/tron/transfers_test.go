package tron

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"net/http"
	"strings"
	"testing"

	"github.com/btcsuite/btcd/btcutil/base58"
	"github.com/renegadik/crypto-payment-gateway/chain-worker/internal/domain"
	"github.com/stretchr/testify/require"
)

const transferTestID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// TestConfirmedTransfersParsesTRXAndTRC20 проверяет нормализацию двух видов переводов.
func TestConfirmedTransfersParsesTRXAndTRC20(t *testing.T) {
	fromRaw, version, err := base58.CheckDecode("THJrqfbBhoB1vX97da6S6nXWkafCxpyCNB")
	require.NoError(t, err)
	require.Equal(t, byte(0x41), version)
	toRaw, version, err := base58.CheckDecode("TXLAQ63Xg1NAzckPwKHvzw7CSEmLMEqcdj")
	require.NoError(t, err)
	require.Equal(t, byte(0x41), version)
	contractRaw := append([]byte{0x41}, make([]byte, 20)...)
	fromHex := "41" + hex.EncodeToString(fromRaw)
	toHex := "41" + hex.EncodeToString(toRaw)
	contractHex := hex.EncodeToString(contractRaw)
	fromTopic := strings.Repeat("0", 24) + hex.EncodeToString(fromRaw)
	toTopic := strings.Repeat("0", 24) + hex.EncodeToString(toRaw)
	amount := new(big.Int).SetUint64(123456789)
	amountHex := hex.EncodeToString(amount.FillBytes(make([]byte, 32)))
	receipt := `{"id":"` + transferTestID + `","result":"SUCCESS","receipt":{"result":"SUCCESS"},"log":[{"address":"` + contractHex + `","topics":["` + trc20TransferTopic + `","` + fromTopic + `","` + toTopic + `"],"data":"` + amountHex + `"}]}`
	client, err := NewClient("http://127.0.0.1", "")
	require.NoError(t, err)
	client.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, solidifiedTransactionInfoPath, r.URL.Path)
		return jsonResponse(http.StatusOK, receipt), nil
	})
	block := json.RawMessage(`{"block_header":{"raw_data":{"number":901}},"transactions":[` +
		`{"txID":"` + transferTestID + `","ret":[{"contractRet":"SUCCESS"}],"raw_data":{"contract":[{"type":"TransferContract","parameter":{"value":{"owner_address":"` + fromHex + `","to_address":"` + toHex + `","amount":1000000}}}]}},` +
		`{"txID":"` + transferTestID + `","ret":[{"contractRet":"SUCCESS"}],"raw_data":{"contract":[{"type":"TriggerSmartContract","parameter":{"value":{}}}]}}]}`)
	transfers, err := client.ConfirmedTransfers(context.Background(), domain.TRONBlock{Number: 901, Payload: block})
	require.NoError(t, err)
	require.Len(t, transfers, 2)
	require.Equal(t, uint64(901), transfers[0].BlockNumber)
	require.Equal(t, "THJrqfbBhoB1vX97da6S6nXWkafCxpyCNB", transfers[0].From)
	require.Equal(t, "TXLAQ63Xg1NAzckPwKHvzw7CSEmLMEqcdj", transfers[0].To)
	require.Equal(t, "1000000", transfers[0].Amount)
	require.Equal(t, base58.CheckEncode(contractRaw[1:], contractRaw[0]), transfers[1].Asset)
	require.Equal(t, amount.String(), transfers[1].Amount)
}

// TestParseTRC20TransferLogsRejectsFailedReceipts проверяет отбрасывание revert-операций.
func TestParseTRC20TransferLogsRejectsFailedReceipts(t *testing.T) {
	transfers, err := parseTRC20TransferLogs(json.RawMessage(`{"id":"`+transferTestID+`","result":"FAILED","receipt":{"result":"REVERT"}}`), transferTestID, 1)
	require.NoError(t, err)
	require.Empty(t, transfers)
}
