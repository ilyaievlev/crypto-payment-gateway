package usecase

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/renegadik/crypto-payment-gateway/payment-core/internal/domain"
)

var ErrInvalidTRONInvoice = domain.ErrInvalidTRONInvoice

// TRONInvoices координирует HD-адресацию и учёт финализированных поступлений.
type TRONInvoices struct {
	repository domain.TRONInvoiceRepository
	addresses  domain.TRONAddressProvider
}

// NewTRONInvoices создаёт сценарии открытия и чтения инвойсов TRON.
func NewTRONInvoices(repository domain.TRONInvoiceRepository, addresses domain.TRONAddressProvider) (*TRONInvoices, error) {
	if repository == nil || addresses == nil {
		return nil, fmt.Errorf("репозиторий и провайдер адресов TRON обязательны")
	}
	return &TRONInvoices{repository: repository, addresses: addresses}, nil
}

// Create выделяет уникальный адрес, привязанный к индексу HD-кошелька.
func (u *TRONInvoices) Create(ctx context.Context, idempotencyKey, amount string) (domain.TRONInvoice, error) {
	parsed, ok := new(big.Int).SetString(amount, 10)
	if !ok || parsed.Sign() <= 0 || parsed.BitLen() > 256 || len(amount) > 78 || strings.HasPrefix(amount, "+") {
		return domain.TRONInvoice{}, ErrInvalidTRONInvoice
	}
	if len(idempotencyKey) > 200 || strings.TrimSpace(idempotencyKey) != idempotencyKey {
		return domain.TRONInvoice{}, ErrInvalidTRONInvoice
	}
	if idempotencyKey != "" {
		invoice, err := u.repository.FindByIdempotencyKey(ctx, idempotencyKey)
		if err == nil {
			if invoice.ExpectedAmount != amount {
				return domain.TRONInvoice{}, ErrInvalidTRONInvoice
			}
			return invoice, nil
		}
		if !errors.Is(err, ErrInvoiceNotFound) {
			return domain.TRONInvoice{}, fmt.Errorf("найти идемпотентный TRON-инвойс: %w", err)
		}
	}
	index, err := u.repository.NextAddressIndex(ctx)
	if err != nil {
		return domain.TRONInvoice{}, fmt.Errorf("зарезервировать индекс TRON-адреса: %w", err)
	}
	if index > uint64(^uint32(0)>>1) {
		return domain.TRONInvoice{}, ErrInvalidTRONInvoice
	}
	address, err := u.addresses.DeriveAddress(ctx, uint32(index))
	if err != nil {
		return domain.TRONInvoice{}, fmt.Errorf("вывести адрес TRON: %w", err)
	}
	return u.repository.Create(ctx, idempotencyKey, index, address, amount)
}

// Get возвращает инвойс вместе с суммой уже финализированных переводов.
func (u *TRONInvoices) Get(ctx context.Context, id string) (domain.TRONInvoice, error) {
	if len(id) != 36 {
		return domain.TRONInvoice{}, ErrInvalidTRONInvoice
	}
	invoice, err := u.repository.GetWithTransfers(ctx, id)
	if err != nil {
		return domain.TRONInvoice{}, err
	}
	received := new(big.Int)
	for _, transfer := range invoice.Transfers {
		value, ok := new(big.Int).SetString(transfer.Amount, 10)
		if !ok || value.Sign() <= 0 {
			return domain.TRONInvoice{}, fmt.Errorf("в БД обнаружена некорректная сумма перевода")
		}
		received.Add(received, value)
	}
	invoice.ReceivedAmount = received.String()
	expected, _ := new(big.Int).SetString(invoice.ExpectedAmount, 10)
	invoice.Status = "pending"
	if received.Cmp(expected) >= 0 {
		invoice.Status = "paid"
	}
	return invoice, nil
}

var ErrInvoiceNotFound = domain.ErrTRONInvoiceNotFound
