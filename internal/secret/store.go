package secret

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/99designs/keyring"
)

const serviceName = "jk"

const (
	envAllowInsecure = "JK_ALLOW_INSECURE_STORE"
	envPassphrase    = "JK_KEYRING_PASSPHRASE"
	envBackend       = "KEYRING_BACKEND"
	envFileDir       = "KEYRING_FILE_DIR"
)

// Store wraps the OS keyring integration.
type Store struct {
	kr keyring.Keyring
}

type openOptions struct {
	allowFile       bool
	passphrase      string
	allowedBackends []keyring.BackendType
	fileDir         string
}

// Option adjusts how the secret store is opened.
type Option func(*openOptions)

// WithAllowFileFallback permits the encrypted file backend when no native
// keyring is available. This is considered insecure compared to OS keychains.
func WithAllowFileFallback(enable bool) Option {
	return func(o *openOptions) {
		o.allowFile = enable
	}
}

// WithPassphrase supplies a passphrase for the encrypted file backend so that
// it can be unlocked without interactive prompts.
func WithPassphrase(pass string) Option {
	return func(o *openOptions) {
		if pass != "" {
			o.passphrase = pass
		}
	}
}

// withAllowedBackends overrides backend selection (intended for tests).
func withAllowedBackends(backends []keyring.BackendType) Option {
	return func(o *openOptions) {
		o.allowedBackends = backends
	}
}

// WithFileDir overrides the directory used by the file backend.
func WithFileDir(dir string) Option {
	return func(o *openOptions) {
		if dir != "" {
			o.fileDir = dir
		}
	}
}

// Open initializes and returns a secret store backed by the preferred OS keyring.
// It can optionally fall back to the encrypted file backend when explicitly
// permitted via options or environment variables.
func Open(opts ...Option) (*Store, error) {
	cfg, err := buildConfig(opts...)
	if err != nil {
		return nil, err
	}

	kr, err := keyring.Open(cfg)
	if err != nil {
		if errors.Is(err, keyring.ErrNoAvailImpl) && !usesFileBackend(cfg.AllowedBackends) {
			return nil, fmt.Errorf("open keyring: %w (set %s=1 or rerun with --allow-insecure-store to permit encrypted file fallback)", err, envAllowInsecure)
		}
		return nil, fmt.Errorf("open keyring: %w", err)
	}

	return &Store{kr: kr}, nil
}

// buildConfig assembles the keyring.Config the same way Open does, without
// actually opening the backend. Exposed for tests that need to assert
// platform-specific defaults such as the darwin Keychain trust flags.
func buildConfig(opts ...Option) (keyring.Config, error) {
	cfg := keyring.Config{
		ServiceName: serviceName,
	}

	if runtime.GOOS == "darwin" {
		// KeychainTrustApplication makes 99designs/keyring call SecAccessCreate
		// with a nil TrustedApplications list when creating new items, which
		// tells Keychain Services to trust the calling binary. Without this we
		// pass an empty slice, which forces a prompt on every access.
		//
		// KeychainAccessibleWhenUnlocked is set alongside for forward
		// compatibility. It maps to kSecAttrAccessible, which Apple documents
		// as applying only to data-protection keychain items or synchronizable
		// items — so in the file-based Keychain path 99designs/keyring uses
		// today it is a no-op. Harmless to set; keeps us ready if the backend
		// migrates to the data-protection keychain.
		//
		// The trust flag alone does not stop re-prompts after `brew upgrade
		// jk` — that requires a stable Designated Requirement on the signed
		// binary (see scripts/sign-darwin-binary.sh). It just ensures a clean
		// first-run experience for new items. Existing items keep whatever ACL
		// they had until the item is deleted and recreated (see auth login).
		cfg.KeychainTrustApplication = true
		cfg.KeychainAccessibleWhenUnlocked = true
	}

	settings := openOptions{}

	if envEnabled(os.Getenv(envAllowInsecure)) {
		settings.allowFile = true
	}
	if pass := strings.TrimSpace(os.Getenv(envPassphrase)); pass != "" {
		settings.passphrase = pass
	}
	if dir := strings.TrimSpace(os.Getenv(envFileDir)); dir != "" {
		settings.fileDir = dir
	}

	for _, opt := range opts {
		opt(&settings)
	}

	cfg.AllowedBackends = resolveAllowedBackends(settings)

	if usesFileBackend(cfg.AllowedBackends) {
		if err := configureFileBackend(&cfg, settings); err != nil {
			return keyring.Config{}, err
		}
	}

	return cfg, nil
}

// Set writes a secret value.
func (s *Store) Set(key, value string) error {
	if s == nil || s.kr == nil {
		return errors.New("secret store not initialized")
	}

	return s.withKeychainLock(func() error {
		return s.kr.Set(keyring.Item{
			Key:   key,
			Data:  []byte(value),
			Label: fmt.Sprintf("jk context %s token", key),
		})
	})
}

// Get retrieves a secret; returns os.ErrNotExist when missing.
func (s *Store) Get(key string) (string, error) {
	if s == nil || s.kr == nil {
		return "", errors.New("secret store not initialized")
	}

	var item keyring.Item
	err := s.withKeychainLock(func() error {
		var getErr error
		item, getErr = s.kr.Get(key)
		return getErr
	})
	if err != nil {
		if errors.Is(err, keyring.ErrKeyNotFound) {
			return "", os.ErrNotExist
		}
		return "", err
	}

	return string(item.Data), nil
}

// Delete removes a secret. Missing items are treated as success.
func (s *Store) Delete(key string) error {
	if s == nil || s.kr == nil {
		return errors.New("secret store not initialized")
	}

	err := s.withKeychainLock(func() error {
		return s.kr.Remove(key)
	})
	if err == nil || errors.Is(err, keyring.ErrKeyNotFound) || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// TokenKey returns the keyring identifier for a context token.
func TokenKey(contextName string) string {
	return fmt.Sprintf("context/%s/token", contextName)
}

// IsNoKeyringError reports whether the provided error indicates that no native
// keyring backend is available on the host. Callers can use this to decide when
// to fall back to the encrypted file backend for backwards compatibility.
func IsNoKeyringError(err error) bool {
	return errors.Is(err, keyring.ErrNoAvailImpl)
}

func resolveAllowedBackends(opts openOptions) []keyring.BackendType {
	if len(opts.allowedBackends) > 0 {
		return opts.allowedBackends
	}

	if backendEnv := strings.TrimSpace(os.Getenv(envBackend)); backendEnv != "" {
		return parseBackendList(backendEnv, opts.allowFile)
	}

	backends := defaultBackends()
	if opts.allowFile {
		backends = append(backends, keyring.FileBackend)
	}
	return backends
}

func defaultBackends() []keyring.BackendType {
	switch runtime.GOOS {
	case "darwin":
		return []keyring.BackendType{keyring.KeychainBackend}
	case "windows":
		return []keyring.BackendType{keyring.WinCredBackend}
	default:
		return []keyring.BackendType{
			keyring.SecretServiceBackend,
			keyring.KWalletBackend,
			keyring.KeyCtlBackend,
			keyring.PassBackend,
		}
	}
}

func parseBackendList(raw string, allowFile bool) []keyring.BackendType {
	parts := strings.Split(raw, ",")
	var backends []keyring.BackendType
	for _, part := range parts {
		switch strings.TrimSpace(strings.ToLower(part)) {
		case "keychain":
			backends = append(backends, keyring.KeychainBackend)
		case "wincred":
			backends = append(backends, keyring.WinCredBackend)
		case "secret-service", "secretservice":
			backends = append(backends, keyring.SecretServiceBackend)
		case "kwallet":
			backends = append(backends, keyring.KWalletBackend)
		case "keyctl":
			backends = append(backends, keyring.KeyCtlBackend)
		case "pass":
			backends = append(backends, keyring.PassBackend)
		case "file":
			backends = append(backends, keyring.FileBackend)
		}
	}
	if !allowFile {
		// Remove file backend unless explicitly allowed via option/env.
		filtered := backends[:0]
		for _, backend := range backends {
			if backend == keyring.FileBackend {
				continue
			}
			filtered = append(filtered, backend)
		}
		backends = filtered
	}
	return backends
}

func configureFileBackend(cfg *keyring.Config, opts openOptions) error {
	passphrase := opts.passphrase
	if passphrase == "" {
		if pwd := os.Getenv("KEYRING_FILE_PASSWORD"); pwd != "" {
			passphrase = pwd
		} else if pwd := os.Getenv("KEYRING_PASSWORD"); pwd != "" {
			passphrase = pwd
		}
	}

	if passphrase != "" {
		cfg.FilePasswordFunc = keyring.FixedStringPrompt(passphrase)
	} else {
		cfg.FilePasswordFunc = keyring.TerminalPrompt
	}

	dir := opts.fileDir
	if dir == "" {
		if userDir, err := os.UserConfigDir(); err == nil {
			dir = filepath.Join(userDir, serviceName, "secrets")
		}
	}

	if dir != "" {
		cfg.FileDir = dir
	}
	return nil
}

func usesFileBackend(backends []keyring.BackendType) bool {
	for _, backend := range backends {
		if backend == keyring.FileBackend {
			return true
		}
	}
	return false
}

func envEnabled(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func (s *Store) withKeychainLock(fn func() error) error {
	return withKeychainLock(fn)
}
