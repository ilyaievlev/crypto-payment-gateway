package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/renegadik/crypto-payment-gateway/payment-core/internal/domain"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// TestCreateTRONInvoiceRejectsInvalidAmounts проверяет строгий разбор суммы в sun.
func TestCreateTRONInvoiceRejectsInvalidAmounts(t *testing.T) {
	for _, amount := range []string{"", "0", "-1", "+1", "1.0", "1e3", " 1", "1 "} {
		t.Run(amount, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			repository := NewMockTRONInvoiceRepository(ctrl)
			addresses := NewMockTRONAddressProvider(ctrl)
			service, err := NewTRONInvoices(repository, addresses)
			require.NoError(t, err)
			_, err = service.Create(context.Background(), "", amount)
			require.ErrorIs(t, err, domain.ErrInvalidTRONInvoice)
		})
	}
}

// TestCreateTRONInvoiceUsesIdempotency проверяет повтор и первоначальное HD-адресование.
func TestCreateTRONInvoiceUsesIdempotency(t *testing.T) {
	t.Run("повтор возвращает прежний адрес", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		repository := NewMockTRONInvoiceRepository(ctrl)
		addresses := NewMockTRONAddressProvider(ctrl)
		want := domain.TRONInvoice{ID: "invoice-id", Address: "TAddress", ExpectedAmount: "1000000"}
		repository.EXPECT().FindByIdempotencyKey(gomock.Any(), "request-1").Return(want, nil)
		service, err := NewTRONInvoices(repository, addresses)
		require.NoError(t, err)
		got, err := service.Create(context.Background(), "request-1", "1000000")
		require.NoError(t, err)
		require.Equal(t, want, got)
	})

	t.Run("создание резервирует индекс и получает адрес из vault", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		repository := NewMockTRONInvoiceRepository(ctrl)
		addresses := NewMockTRONAddressProvider(ctrl)
		repository.EXPECT().NextAddressIndex(gomock.Any()).Return(uint64(7), nil)
		addresses.EXPECT().DeriveAddress(gomock.Any(), uint32(7)).Return("TAddress", nil)
		want := domain.TRONInvoice{ID: "invoice-id", Address: "TAddress", ExpectedAmount: "1000000"}
		repository.EXPECT().Create(gomock.Any(), "", uint64(7), "TAddress", "1000000").Return(want, nil)
		service, err := NewTRONInvoices(repository, addresses)
		require.NoError(t, err)
		got, err := service.Create(context.Background(), "", "1000000")
		require.NoError(t, err)
		require.Equal(t, want, got)
	})
}

// TestGetTRONInvoiceCountsOnlyFinalizedTransfers проверяет итоговый статус инвойса.
func TestGetTRONInvoiceCountsOnlyFinalizedTransfers(t *testing.T) {
	ctrl := gomock.NewController(t)
	repository := NewMockTRONInvoiceRepository(ctrl)
	addresses := NewMockTRONAddressProvider(ctrl)
	createdAt := time.Now().UTC()
	repository.EXPECT().GetWithTransfers(gomock.Any(), "12345678-1234-1234-1234-123456789abc").Return(domain.TRONInvoice{
		ID: "12345678-1234-1234-1234-123456789abc", ExpectedAmount: "100",
		Transfers: []domain.TRONInvoiceTransfer{{Amount: "60"}, {Amount: "40"}}, CreatedAt: createdAt,
	}, nil)
	service, err := NewTRONInvoices(repository, addresses)
	require.NoError(t, err)
	invoice, err := service.Get(context.Background(), "12345678-1234-1234-1234-123456789abc")
	require.NoError(t, err)
	require.Equal(t, "100", invoice.ReceivedAmount)
	require.Equal(t, "paid", invoice.Status)
}
