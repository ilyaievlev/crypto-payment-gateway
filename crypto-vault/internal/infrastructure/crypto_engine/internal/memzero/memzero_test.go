package memzero

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBytes проверяет, что Bytes обнуляет весь переданный срез.
func TestBytes(t *testing.T) {
	secret := []byte("sensitive material")

	Bytes(secret)

	require.Equal(t, make([]byte, len(secret)), secret)
}
