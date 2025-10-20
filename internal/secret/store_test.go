package secret

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/99designs/keyring"
)

func TestEnvEnabled(t *testing.T) {
	t.Helper()
	cases := map[string]bool{
		"1":     true,
		"true":  true,
		"TRUE":  true,
		"yes":   true,
		"on":    true,
		"0":     false,
		"false": false,
		"no":    false,
		"":      false,
	}

	for input, expected := range cases {
		if got := envEnabled(input); got != expected {
			t.Errorf("envEnabled(%q) = %v, expected %v", input, got, expected)
		}
	}
}

func TestParseBackendList(t *testing.T) {
	t.Helper()

	cases := []struct {
		name      string
		raw       string
		allowFile bool
		expected  []keyring.BackendType
	}{
		{
			name:      "explicit file allowed",
			raw:       "file",
			allowFile: true,
			expected:  []keyring.BackendType{keyring.FileBackend},
		},
		{
			name:      "explicit file disallowed",
			raw:       "file,keychain",
			allowFile: false,
			expected:  []keyring.BackendType{keyring.KeychainBackend},
		},
		{
			name:      "multiple backends preserved",
			raw:       "secret-service,pass",
			allowFile: false,
			expected: []keyring.BackendType{
				keyring.SecretServiceBackend,
				keyring.PassBackend,
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := parseBackendList(tc.raw, tc.allowFile)
			if !reflect.DeepEqual(got, tc.expected) {
				t.Fatalf("parseBackendList(%q, %v) = %#v, expected %#v", tc.raw, tc.allowFile, got, tc.expected)
			}
		})
	}
}

func TestConfigureFileBackendUsesPassphraseOption(t *testing.T) {
	t.Helper()

	tmpDir := t.TempDir()
	cfg := keyring.Config{}
	opts := openOptions{
		passphrase: "secret-pass",
		fileDir:    filepath.Join(tmpDir, "secrets"),
	}

	if err := configureFileBackend(&cfg, opts); err != nil {
		t.Fatalf("configureFileBackend returned error: %v", err)
	}

	if cfg.FileDir != opts.fileDir {
		t.Fatalf("FileDir = %q, expected %q", cfg.FileDir, opts.fileDir)
	}

	if cfg.FilePasswordFunc == nil {
		t.Fatalf("FilePasswordFunc should not be nil")
	}

	value, err := cfg.FilePasswordFunc("prompt")
	if err != nil {
		t.Fatalf("FilePasswordFunc returned error: %v", err)
	}
	if value != opts.passphrase {
		t.Fatalf("FilePasswordFunc returned %q, expected %q", value, opts.passphrase)
	}
}

func TestConfigureFileBackendFallsBackToEnv(t *testing.T) {
	t.Helper()

	tmpDir := t.TempDir()
	const envPass = "env-pass"

	t.Setenv("KEYRING_FILE_PASSWORD", envPass)
	t.Setenv("KEYRING_PASSWORD", "")

	cfg := keyring.Config{}
	opts := openOptions{
		fileDir: filepath.Join(tmpDir, "secrets"),
	}

	if err := configureFileBackend(&cfg, opts); err != nil {
		t.Fatalf("configureFileBackend returned error: %v", err)
	}

	if cfg.FilePasswordFunc == nil {
		t.Fatalf("FilePasswordFunc should not be nil")
	}

	value, err := cfg.FilePasswordFunc("prompt")
	if err != nil {
		t.Fatalf("FilePasswordFunc returned error: %v", err)
	}

	if value != envPass {
		t.Fatalf("FilePasswordFunc returned %q, expected %q", value, envPass)
	}
}

func TestResolveAllowedBackendsEnvOverride(t *testing.T) {
	t.Helper()

	t.Setenv(envBackend, "file")
	opts := openOptions{allowFile: true}

	got := resolveAllowedBackends(opts)
	expected := []keyring.BackendType{keyring.FileBackend}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("resolveAllowedBackends = %#v, expected %#v", got, expected)
	}
}

func TestIsNoKeyringError(t *testing.T) {
	t.Helper()

	err := fmt.Errorf("open keyring: %w", keyring.ErrNoAvailImpl)
	if !IsNoKeyringError(err) {
		t.Fatalf("expected IsNoKeyringError to return true")
	}

	if IsNoKeyringError(nil) {
		t.Fatalf("expected IsNoKeyringError to return false for nil error")
	}

	otherErr := errors.New("some other error")
	if IsNoKeyringError(otherErr) {
		t.Fatalf("expected IsNoKeyringError to return false for unrelated error")
	}
}

func TestWithAllowedBackends(t *testing.T) {
	t.Helper()

	backends := []keyring.BackendType{keyring.FileBackend}
	opts := openOptions{}

	withAllowedBackends(backends)(&opts)

	if !reflect.DeepEqual(opts.allowedBackends, backends) {
		t.Fatalf("withAllowedBackends did not set opts.allowedBackends, got %#v, expected %#v", opts.allowedBackends, backends)
	}
}
