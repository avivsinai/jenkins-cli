package secret

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/99designs/keyring"
)

const serviceName = "jk"

// Store wraps the OS keyring integration.
type Store struct {
	kr keyring.Keyring
}

// Open initializes and returns a secret store backed by the OS keyring.
func Open() (*Store, error) {
	cfg := keyring.Config{
		ServiceName: serviceName,
	}

	if pwd := os.Getenv("KEYRING_FILE_PASSWORD"); pwd != "" {
		cfg.FilePasswordFunc = keyring.FixedStringPrompt(pwd)
	} else if pwd := os.Getenv("KEYRING_PASSWORD"); pwd != "" {
		cfg.FilePasswordFunc = keyring.FixedStringPrompt(pwd)
	} else {
		cfg.FilePasswordFunc = keyring.TerminalPrompt
	}

	// Configure file caching in case the backend requires it.
	if dir, err := os.UserConfigDir(); err == nil {
		cfg.FileDir = filepath.Join(dir, serviceName, "secrets")
	}

	kr, err := keyring.Open(cfg)
	if err != nil {
		return nil, fmt.Errorf("open keyring: %w", err)
	}

	return &Store{kr: kr}, nil
}

// Set writes a secret value.
func (s *Store) Set(key, value string) error {
	if s == nil || s.kr == nil {
		return errors.New("secret store not initialized")
	}

	return s.kr.Set(keyring.Item{
		Key:   key,
		Data:  []byte(value),
		Label: fmt.Sprintf("jk context %s token", key),
	})
}

// Get retrieves a secret; returns os.ErrNotExist when missing.
func (s *Store) Get(key string) (string, error) {
	if s == nil || s.kr == nil {
		return "", errors.New("secret store not initialized")
	}

	item, err := s.kr.Get(key)
	if err != nil {
		if errors.Is(err, keyring.ErrKeyNotFound) {
			return "", os.ErrNotExist
		}
		return "", err
	}

	return string(item.Data), nil
}

// Delete removes a secret.
func (s *Store) Delete(key string) error {
	if s == nil || s.kr == nil {
		return errors.New("secret store not initialized")
	}

	err := s.kr.Remove(key)
	if errors.Is(err, keyring.ErrKeyNotFound) {
		return nil
	}
	return err
}

// TokenKey returns the keyring identifier for a context token.
func TokenKey(contextName string) string {
	return fmt.Sprintf("context/%s/token", contextName)
}
