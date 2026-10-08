package memzero

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBytes проверяет, что Bytes обнуляет весь переданный срез.
func TestBytes(t *testing.T) {
	value := []byte("чувствительные данные")
	Bytes(value)
	require.Equal(t, make([]byte, len(value)), value)
}
