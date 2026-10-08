package seedvault

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestWrappedKeyRoundTrip проверяет создание ключа, повторное открытие и пароль.
func TestWrappedKeyRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys", "wrapped-key.json")
	password := []byte("test-only-password-with-enough-entropy")

	first, err := LoadOrCreateKey(path, password)
	require.NoError(t, err)
	require.Equal(t, masterKeySize, len(first))

	second, err := LoadOrCreateKey(path, password)
	require.NoError(t, err)
	require.True(t, bytes.Equal(first, second))

	wrongPassword, err := LoadKey(path, []byte("different-test-password-with-entropy"))
	require.Error(t, err)
	require.Nil(t, wrongPassword)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// TestLoadKeyRejectsInsecurePermissions проверяет отказ от доступного группе файла.
func TestLoadKeyRejectsInsecurePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wrapped-key.json")
	require.NoError(t, os.WriteFile(path, []byte(`{}`), 0o644))

	_, err := LoadKey(path, []byte("test-only-password-with-enough-entropy"))

	require.Error(t, err)
}
