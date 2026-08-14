package secret

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/99designs/keyring"
)

func TestBuildConfig_DarwinTrustFlags(t *testing.T) {
	cfg, err := buildConfig()
	if err != nil {
		t.Fatalf("buildConfig: %v", err)
	}

	if runtime.GOOS == "darwin" {
		if !cfg.KeychainTrustApplication {
			t.Errorf("KeychainTrustApplication should be true on darwin")
		}
		if !cfg.KeychainAccessibleWhenUnlocked {
			t.Errorf("KeychainAccessibleWhenUnlocked should be true on darwin")
		}
	} else {
		if cfg.KeychainTrustApplication {
			t.Errorf("KeychainTrustApplication should be false on %s", runtime.GOOS)
		}
		if cfg.KeychainAccessibleWhenUnlocked {
			t.Errorf("KeychainAccessibleWhenUnlocked should be false on %s", runtime.GOOS)
		}
	}

	if cfg.ServiceName != serviceName {
		t.Errorf("ServiceName = %q, want %q", cfg.ServiceName, serviceName)
	}
}

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

func TestConfigureFileBackendSkipsPromptWhenEmptyPassphraseWorks(t *testing.T) {
	t.Helper()

	t.Setenv("KEYRING_FILE_PASSWORD", "")
	t.Setenv("KEYRING_PASSWORD", "")

	dir := filepath.Join(t.TempDir(), "secrets")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	seed, err := keyring.Open(keyring.Config{
		ServiceName:      serviceName,
		AllowedBackends:  []keyring.BackendType{keyring.FileBackend},
		FileDir:          dir,
		FilePasswordFunc: keyring.FixedStringPrompt(""),
	})
	if err != nil {
		t.Fatalf("seed open: %v", err)
	}
	if err := seed.Set(keyring.Item{Key: "context/test/token", Data: []byte("token-value")}); err != nil {
		t.Fatalf("seed set: %v", err)
	}

	cfg := keyring.Config{}
	if err := configureFileBackend(&cfg, openOptions{fileDir: dir}); err != nil {
		t.Fatalf("configureFileBackend: %v", err)
	}

	value, err := cfg.FilePasswordFunc("should not prompt")
	if err != nil {
		t.Fatalf("FilePasswordFunc: %v", err)
	}
	if value != "" {
		t.Fatalf("FilePasswordFunc returned %q, want empty passphrase", value)
	}

	store, err := Open(WithAllowFileFallback(true), WithFileDir(dir))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	got, err := store.Get("context/test/token")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "token-value" {
		t.Fatalf("Get = %q, want %q", got, "token-value")
	}
}

func TestConfigureFileBackendSkipsPromptForEmptyDir(t *testing.T) {
	t.Helper()

	t.Setenv("KEYRING_FILE_PASSWORD", "")
	t.Setenv("KEYRING_PASSWORD", "")

	dir := filepath.Join(t.TempDir(), "secrets")
	cfg := keyring.Config{}
	if err := configureFileBackend(&cfg, openOptions{fileDir: dir}); err != nil {
		t.Fatalf("configureFileBackend: %v", err)
	}

	value, err := cfg.FilePasswordFunc("should not prompt")
	if err != nil {
		t.Fatalf("FilePasswordFunc: %v", err)
	}
	if value != "" {
		t.Fatalf("FilePasswordFunc returned %q, want empty passphrase", value)
	}
}

func TestConfigureFileBackendPromptsWhenPassphraseRequired(t *testing.T) {
	t.Helper()

	t.Setenv("KEYRING_FILE_PASSWORD", "")
	t.Setenv("KEYRING_PASSWORD", "")

	dir := filepath.Join(t.TempDir(), "secrets")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	seed, err := keyring.Open(keyring.Config{
		ServiceName:      serviceName,
		AllowedBackends:  []keyring.BackendType{keyring.FileBackend},
		FileDir:          dir,
		FilePasswordFunc: keyring.FixedStringPrompt("secret-pass"),
	})
	if err != nil {
		t.Fatalf("seed open: %v", err)
	}
	if err := seed.Set(keyring.Item{Key: "context/test/token", Data: []byte("token-value")}); err != nil {
		t.Fatalf("seed set: %v", err)
	}

	if fileSecretsUnlockWithEmptyPassphrase(&keyring.Config{FileDir: dir}) {
		t.Fatalf("expected passphrase-protected secrets to require a prompt")
	}

	cfg := keyring.Config{}
	if err := configureFileBackend(&cfg, openOptions{fileDir: dir}); err != nil {
		t.Fatalf("configureFileBackend: %v", err)
	}

	// TerminalPrompt is the function value itself; compare pointers.
	if reflect.ValueOf(cfg.FilePasswordFunc).Pointer() != reflect.ValueOf(keyring.TerminalPrompt).Pointer() {
		t.Fatalf("FilePasswordFunc should be TerminalPrompt when secrets require a passphrase")
	}
}

func TestBuildConfigUsesJkPassphraseEnv(t *testing.T) {
	t.Helper()

	tmpDir := t.TempDir()
	const envPass = "jk-env-pass"

	t.Setenv(envBackend, "")
	t.Setenv(envAllowInsecure, "1")
	t.Setenv(envPassphrase, envPass)
	t.Setenv("KEYRING_FILE_PASSWORD", "")
	t.Setenv("KEYRING_PASSWORD", "")
	t.Setenv(envFileDir, filepath.Join(tmpDir, "secrets"))

	cfg, err := buildConfig()
	if err != nil {
		t.Fatalf("buildConfig: %v", err)
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

func TestResolveAllowedBackendsAllowFileUsesFileExclusively(t *testing.T) {
	t.Helper()

	t.Setenv(envBackend, "")
	opts := openOptions{allowFile: true}

	got := resolveAllowedBackends(opts)
	expected := []keyring.BackendType{keyring.FileBackend}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("resolveAllowedBackends = %#v, expected %#v", got, expected)
	}
}

func TestBuildConfigAllowInsecureEnvUsesFileExclusively(t *testing.T) {
	t.Helper()

	t.Setenv(envBackend, "")
	t.Setenv(envAllowInsecure, "1")

	cfg, err := buildConfig()
	if err != nil {
		t.Fatalf("buildConfig: %v", err)
	}

	expected := []keyring.BackendType{keyring.FileBackend}
	if !reflect.DeepEqual(cfg.AllowedBackends, expected) {
		t.Fatalf("AllowedBackends = %#v, expected %#v", cfg.AllowedBackends, expected)
	}
}

func TestResolveAllowedBackendsExplicitBackendWinsOverAllowFile(t *testing.T) {
	t.Helper()

	t.Setenv(envBackend, "secret-service,pass")
	opts := openOptions{allowFile: true}

	got := resolveAllowedBackends(opts)
	expected := []keyring.BackendType{
		keyring.SecretServiceBackend,
		keyring.PassBackend,
	}
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
