// Package seedvault управляет локальным ключом шифрования и доступом к зашифрованным сидам.
package seedvault

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/crypto/argon2"

	"github.com/renegadik/crypto-payment-gateway/crypto-vault/internal/infrastructure/memzero"
)

const (
	wrappedKeyVersion = 1
	masterKeySize     = 32
	kdfSaltSize       = 16
	keyNonceSize      = 12
	argonMemoryKiB    = 64 * 1024
	argonIterations   = 3
	argonParallelism  = 4
	keyFileAAD        = "crypto-vault/local-key/v1"
	maxKeyFileSize    = 4096
)

// wrappedKeyFile содержит зашифрованный ключ данных и параметры его открытия.
type wrappedKeyFile struct {
	Version    int    `json:"version"`
	KDF        string `json:"kdf"`
	MemoryKiB  uint32 `json:"memory_kib"`
	Iterations uint32 `json:"iterations"`
	Lanes      uint8  `json:"lanes"`
	Salt       string `json:"salt"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

// ReadPasswordFile читает пароль из отдельного файла и удаляет завершающие переводы строк.
func ReadPasswordFile(path string) ([]byte, error) {
	password, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("прочитать файл пароля Crypto Vault: %w", err)
	}
	for len(password) > 0 && (password[len(password)-1] == '\r' || password[len(password)-1] == '\n') {
		password = password[:len(password)-1]
	}
	if len(password) < 16 {
		memzero.Bytes(password)
		return nil, fmt.Errorf("пароль Crypto Vault должен содержать не менее 16 байт")
	}
	return password, nil
}

// LoadKeyFromPasswordFile читает пароль, открывает ключ и сразу очищает пароль.
func LoadKeyFromPasswordFile(keyPath, passwordPath string) ([]byte, error) {
	password, err := ReadPasswordFile(passwordPath)
	if err != nil {
		return nil, err
	}
	defer memzero.Bytes(password)
	return LoadKey(keyPath, password)
}

// LoadOrCreateKeyFromPasswordFile открывает или создаёт ключ и очищает пароль.
func LoadOrCreateKeyFromPasswordFile(keyPath, passwordPath string) ([]byte, error) {
	password, err := ReadPasswordFile(passwordPath)
	if err != nil {
		return nil, err
	}
	defer memzero.Bytes(password)
	return LoadOrCreateKey(keyPath, password)
}

// LoadKey читает и расшифровывает локальный ключ данных.
func LoadKey(path string, password []byte) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("получить сведения о файле ключа Crypto Vault: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("файл ключа Crypto Vault должен быть обычным файлом с правами не шире 0600")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("открыть файл ключа Crypto Vault: %w", err)
	}
	defer func() { _ = file.Close() }()
	encoded, err := io.ReadAll(io.LimitReader(file, maxKeyFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("прочитать файл ключа Crypto Vault: %w", err)
	}
	if len(encoded) > maxKeyFileSize {
		return nil, fmt.Errorf("файл ключа Crypto Vault превышает допустимый размер")
	}

	var record wrappedKeyFile
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return nil, fmt.Errorf("разобрать файл ключа Crypto Vault: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("файл ключа Crypto Vault содержит лишние данные")
	}
	if record.Version != wrappedKeyVersion || record.KDF != "argon2id" ||
		record.MemoryKiB != argonMemoryKiB || record.Iterations != argonIterations || record.Lanes != argonParallelism {
		return nil, fmt.Errorf("неподдерживаемый формат файла ключа Crypto Vault")
	}
	salt, err := base64.StdEncoding.DecodeString(record.Salt)
	if err != nil || len(salt) != kdfSaltSize {
		return nil, fmt.Errorf("некорректная соль файла ключа Crypto Vault")
	}
	nonce, err := base64.StdEncoding.DecodeString(record.Nonce)
	if err != nil || len(nonce) != keyNonceSize {
		return nil, fmt.Errorf("некорректный nonce файла ключа Crypto Vault")
	}
	ciphertext, err := base64.StdEncoding.DecodeString(record.Ciphertext)
	if err != nil || len(ciphertext) != masterKeySize+16 {
		return nil, fmt.Errorf("некорректный зашифрованный ключ Crypto Vault")
	}

	wrappingKey := deriveWrappingKey(password, salt)
	defer memzero.Bytes(wrappingKey)
	block, err := aes.NewCipher(wrappingKey)
	if err != nil {
		return nil, fmt.Errorf("создать AES-шифр для ключа Crypto Vault: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("создать GCM для ключа Crypto Vault: %w", err)
	}
	key, err := aead.Open(nil, nonce, ciphertext, []byte(keyFileAAD))
	if err != nil {
		return nil, fmt.Errorf("не удалось открыть файл ключа Crypto Vault")
	}
	if len(key) != masterKeySize {
		memzero.Bytes(key)
		return nil, fmt.Errorf("некорректная длина ключа Crypto Vault")
	}
	return key, nil
}

// LoadOrCreateKey открывает существующий ключ или атомарно создаёт новый.
func LoadOrCreateKey(path string, password []byte) ([]byte, error) {
	key, err := LoadKey(path, password)
	if err == nil {
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key = make([]byte, masterKeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("создать ключ данных Crypto Vault: %w", err)
	}
	if err := saveWrappedKey(path, password, key); err != nil {
		if errors.Is(err, os.ErrExist) {
			memzero.Bytes(key)
			return LoadKey(path, password)
		}
		memzero.Bytes(key)
		return nil, err
	}
	return key, nil
}

// saveWrappedKey шифрует ключ данных паролем и создаёт файл без перезаписи.
func saveWrappedKey(path string, password, key []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("создать защищённый каталог ключей: %w", err)
	}
	dirInfo, err := os.Lstat(directory)
	if err != nil || dirInfo.Mode()&os.ModeSymlink != 0 || !dirInfo.IsDir() {
		return fmt.Errorf("каталог ключей Crypto Vault должен быть обычным каталогом")
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return fmt.Errorf("ограничить права каталога ключей: %w", err)
	}

	salt := make([]byte, kdfSaltSize)
	if _, err := rand.Read(salt); err != nil {
		return fmt.Errorf("создать соль файла ключа: %w", err)
	}
	nonce := make([]byte, keyNonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("создать nonce файла ключа: %w", err)
	}
	wrappingKey := deriveWrappingKey(password, salt)
	defer memzero.Bytes(wrappingKey)
	block, err := aes.NewCipher(wrappingKey)
	if err != nil {
		return fmt.Errorf("создать AES-шифр для ключа Crypto Vault: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("создать GCM для ключа Crypto Vault: %w", err)
	}
	record := wrappedKeyFile{
		Version:    wrappedKeyVersion,
		KDF:        "argon2id",
		MemoryKiB:  argonMemoryKiB,
		Iterations: argonIterations,
		Lanes:      argonParallelism,
		Salt:       base64.StdEncoding.EncodeToString(salt),
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(aead.Seal(nil, nonce, key, []byte(keyFileAAD))),
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("закодировать файл ключа Crypto Vault: %w", err)
	}

	temporary, err := os.CreateTemp(directory, ".crypto-vault-key-*")
	if err != nil {
		return fmt.Errorf("создать временный файл ключа Crypto Vault: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("ограничить права временного файла ключа: %w", err)
	}
	if _, err := temporary.Write(encoded); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("записать файл ключа Crypto Vault: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("синхронизировать файл ключа Crypto Vault: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("закрыть временный файл ключа Crypto Vault: %w", err)
	}
	if err := os.Link(temporaryPath, path); err != nil {
		return fmt.Errorf("создать файл ключа Crypto Vault без перезаписи: %w", err)
	}
	return nil
}

// deriveWrappingKey выводит AES-ключ из пароля с помощью Argon2id.
func deriveWrappingKey(password, salt []byte) []byte {
	return argon2.IDKey(password, salt, argonIterations, argonMemoryKiB, argonParallelism, masterKeySize)
}
