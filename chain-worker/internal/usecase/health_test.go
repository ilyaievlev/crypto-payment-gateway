package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// TestHealthCheck проверяет успешную, ошибочную и отменённую проверки готовности.
func TestHealthCheck(t *testing.T) {
	unavailable := errors.New("offline")
	tests := []struct {
		name          string
		dependencyErr error
		canceled      bool
		want          error
	}{
		{name: "available"},
		{name: "unavailable", dependencyErr: unavailable, want: unavailable},
		{name: "canceled", canceled: true, want: context.Canceled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			dependency := NewMockHealthChecker(ctrl)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.canceled {
				cancel()
			} else {
				dependency.EXPECT().Check(ctx).Return(tt.dependencyErr)
			}
			err := NewHealth(dependency).Check(ctx)
			if tt.want == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tt.want)
			}
		})
	}
}

// TestHealthStopsOnFirstFailure проверяет остановку на первой ошибке зависимости.
func TestHealthStopsOnFirstFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	first := NewMockHealthChecker(ctrl)
	second := NewMockHealthChecker(ctrl)
	failure := errors.New("offline")
	first.EXPECT().Check(gomock.Any()).Return(failure)
	require.ErrorIs(t, NewHealth(first, second).Check(context.Background()), failure)
}
