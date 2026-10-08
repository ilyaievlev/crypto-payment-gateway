package tron

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/btcsuite/btcd/btcutil/base58"
	"golang.org/x/crypto/sha3"

	"github.com/renegadik/crypto-payment-gateway/chain-worker/internal/domain"
)

var trc20TransferTopic = func() string {
	h := sha3.NewLegacyKeccak256()
	_, _ = h.Write([]byte("Transfer(address,address,uint256)"))
	return hex.EncodeToString(h.Sum(nil))
}()

// ConfirmedTransfer описывает успешный перевод из уже финализированного блока.
type ConfirmedTransfer = domain.TRONTransfer

// ConfirmedTransfers разбирает входящие TRX и TRC-20 переводы блока.
// Для вызовов контрактов читает только финализированные receipts.
func (c *Client) ConfirmedTransfers(ctx context.Context, block domain.TRONBlock) ([]ConfirmedTransfer, error) {
	var parsed struct {
		Header struct {
			Raw struct {
				Number uint64 `json:"number"`
			} `json:"raw_data"`
		} `json:"block_header"`
		Transactions []struct {
			ID      string `json:"txID"`
			Results []struct {
				ContractResult string `json:"contractRet"`
			} `json:"ret"`
			RawData struct {
				Contracts []struct {
					Type      string `json:"type"`
					Parameter struct {
						Value json.RawMessage `json:"value"`
					} `json:"parameter"`
				} `json:"contract"`
			} `json:"raw_data"`
		} `json:"transactions"`
	}
	if err := json.Unmarshal(block.Payload, &parsed); err != nil {
		return nil, fmt.Errorf("разобрать транзакции блока TRON: %w", err)
	}
	if parsed.Header.Raw.Number == 0 || parsed.Header.Raw.Number != block.Number {
		return nil, fmt.Errorf("блок TRON не содержит высоту")
	}
	transfers := make([]ConfirmedTransfer, 0)
	for _, transaction := range parsed.Transactions {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !validTransactionID(transaction.ID) || len(transaction.RawData.Contracts) != 1 || !successfulContract(transaction.Results) {
			continue
		}
		contract := transaction.RawData.Contracts[0]
		switch contract.Type {
		case "TransferContract":
			var value struct {
				Owner  string      `json:"owner_address"`
				To     string      `json:"to_address"`
				Amount json.Number `json:"amount"`
			}
			decoder := json.NewDecoder(strings.NewReader(string(contract.Parameter.Value)))
			decoder.UseNumber()
			if err := decoder.Decode(&value); err != nil {
				continue
			}
			amount, ok := new(big.Int).SetString(value.Amount.String(), 10)
			if !ok || amount.Sign() <= 0 {
				continue
			}
			from, fromErr := normalizeNodeAddress(value.Owner)
			to, toErr := normalizeNodeAddress(value.To)
			if fromErr != nil || toErr != nil {
				continue
			}
			transfers = append(transfers, ConfirmedTransfer{
				TransactionID: strings.ToLower(transaction.ID), BlockNumber: parsed.Header.Raw.Number,
				EventIndex: 0, From: from, To: to, Amount: amount.String(),
			})
		case "TriggerSmartContract":
			info, found, err := c.SolidifiedTransactionInfo(ctx, transaction.ID)
			if err != nil {
				return nil, err
			}
			if !found {
				continue
			}
			events, err := parseTRC20TransferLogs(info, transaction.ID, parsed.Header.Raw.Number)
			if err != nil {
				return nil, err
			}
			transfers = append(transfers, events...)
		}
	}
	return transfers, nil
}

// successfulContract допускает только завершённые без ошибки on-chain операции.
func successfulContract(results []struct {
	ContractResult string `json:"contractRet"`
}) bool {
	return len(results) == 1 && strings.EqualFold(results[0].ContractResult, "SUCCESS")
}

// parseTRC20TransferLogs принимает только успешные ERC-20-совместимые Transfer logs.
func parseTRC20TransferLogs(receipt json.RawMessage, transactionID string, blockNumber uint64) ([]ConfirmedTransfer, error) {
	var result struct {
		ID      string `json:"id"`
		Status  string `json:"result"`
		Receipt struct {
			Status string `json:"result"`
		} `json:"receipt"`
		Logs []struct {
			Address string   `json:"address"`
			Topics  []string `json:"topics"`
			Data    string   `json:"data"`
		} `json:"log"`
	}
	if err := json.Unmarshal(receipt, &result); err != nil {
		return nil, fmt.Errorf("разобрать TRON receipt: %w", err)
	}
	if !strings.EqualFold(result.ID, transactionID) {
		return nil, fmt.Errorf("TRON receipt не соответствует транзакции")
	}
	if !strings.EqualFold(result.Status, "SUCCESS") || !strings.EqualFold(result.Receipt.Status, "SUCCESS") {
		return nil, nil
	}
	transfers := make([]ConfirmedTransfer, 0)
	for eventIndex, event := range result.Logs {
		if len(event.Topics) != 3 || !strings.EqualFold(strings.TrimPrefix(event.Topics[0], "0x"), trc20TransferTopic) {
			continue
		}
		contract, err := normalizeNodeAddress(event.Address)
		if err != nil {
			continue
		}
		from, err := normalizeTopicAddress(event.Topics[1])
		if err != nil {
			continue
		}
		to, err := normalizeTopicAddress(event.Topics[2])
		if err != nil {
			continue
		}
		data := strings.TrimPrefix(event.Data, "0x")
		amountBytes, err := hex.DecodeString(data)
		if err != nil || len(amountBytes) != 32 {
			continue
		}
		amount := new(big.Int).SetBytes(amountBytes)
		if amount.Sign() <= 0 {
			continue
		}
		transfers = append(transfers, ConfirmedTransfer{
			TransactionID: strings.ToLower(transactionID), BlockNumber: blockNumber,
			EventIndex: int32(eventIndex), From: from, To: to, Asset: contract, Amount: amount.String(),
		})
	}
	return transfers, nil
}

// normalizeTopicAddress преобразует indexed-адрес Solidity в Base58Check TRON.
func normalizeTopicAddress(value string) (string, error) {
	value = strings.TrimPrefix(strings.TrimPrefix(value, "0x"), "0X")
	if len(value) != 64 {
		return "", fmt.Errorf("неверная длина индексированного TRON-адреса")
	}
	payload, err := hex.DecodeString(value[24:])
	if err != nil {
		return "", err
	}
	return normalizeNodeAddress("41" + hex.EncodeToString(payload))
}

// normalizeNodeAddress проверяет сетевой префикс и возвращает адрес Base58Check.
func normalizeNodeAddress(value string) (string, error) {
	value = strings.TrimPrefix(strings.TrimPrefix(value, "0x"), "0X")
	if len(value) == 40 {
		value = "41" + value
	}
	raw, err := hex.DecodeString(value)
	if err == nil && len(raw) == 21 && raw[0] == 0x41 {
		return base58.CheckEncode(raw[1:], raw[0]), nil
	}
	payload, version, err := base58.CheckDecode(value)
	if err == nil && version == 0x41 && len(payload) == 20 {
		return value, nil
	}
	return "", fmt.Errorf("некорректный адрес TRON из ответа узла")
}
