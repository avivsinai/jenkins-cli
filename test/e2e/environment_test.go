package e2e

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// dockerProxyEnvironment reads only the Docker CLI proxy-default fields. The
// testcontainers SDK does not apply these defaults as the Docker CLI does.
// Authentication settings are intentionally neither decoded nor forwarded.
func dockerProxyEnvironment(configDir string) (map[string]string, error) {
	result := map[string]string{}
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("find Docker config directory: %w", err)
		}
		configDir = filepath.Join(home, ".docker")
	}
	file, err := os.Open(filepath.Join(configDir, "config.json"))
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open Docker proxy defaults: %w", err)
	}
	defer func() { _ = file.Close() }()
	var cfg struct {
		Proxies map[string]struct {
			HTTPProxy  string `json:"httpProxy"`
			HTTPSProxy string `json:"httpsProxy"`
			NoProxy    string `json:"noProxy"`
		} `json:"proxies"`
	}
	if err := json.NewDecoder(file).Decode(&cfg); err != nil {
		return nil, errors.New("parse Docker proxy defaults: invalid config.json")
	}
	proxy := cfg.Proxies["default"]
	for key, value := range map[string]string{"HTTP_PROXY": proxy.HTTPProxy, "HTTPS_PROXY": proxy.HTTPSProxy, "NO_PROXY": proxy.NoProxy} {
		if value != "" {
			result[key] = value
			result[strings.ToLower(key)] = value
		}
	}
	return result, nil
}

func e2eCAFile() (string, error) {
	path := os.Getenv("JK_E2E_CA_FILE")
	if path == "" {
		return "", nil
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve JK_E2E_CA_FILE: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("read JK_E2E_CA_FILE: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("JK_E2E_CA_FILE must name a regular CA bundle file")
	}
	return path, nil
}

func TestDockerProxyEnvironment(t *testing.T) {
	dir := t.TempDir()
	proxy, err := dockerProxyEnvironment(dir)
	require.NoError(t, err)
	require.Empty(t, proxy)
	config := `{"auths":{"registry.example":{"auth":"do-not-forward"}},"proxies":{"default":{"httpProxy":"http://proxy.example:8080","httpsProxy":"http://proxy.example:8080","noProxy":"localhost,127.0.0.1"},"tcp://other-host":{"httpProxy":"http://other.example:8080"}}}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"), []byte(config), 0o600))
	proxy, err = dockerProxyEnvironment(dir)
	require.NoError(t, err)
	require.Len(t, proxy, 6)
	require.Equal(t, "http://proxy.example:8080", proxy["HTTP_PROXY"])
	require.Equal(t, proxy["HTTP_PROXY"], proxy["http_proxy"])
	require.Equal(t, "localhost,127.0.0.1", proxy["NO_PROXY"])
	for _, value := range proxy {
		require.NotContains(t, value, "do-not-forward")
		require.NotContains(t, value, "other.example")
	}
}

func TestE2ECAFile(t *testing.T) {
	t.Setenv("JK_E2E_CA_FILE", "")
	path, err := e2eCAFile()
	require.NoError(t, err)
	require.Empty(t, path)
	t.Setenv("JK_E2E_CA_FILE", t.TempDir())
	_, err = e2eCAFile()
	require.ErrorContains(t, err, "regular CA bundle")
	file := filepath.Join(t.TempDir(), "bundle.pem")
	require.NoError(t, os.WriteFile(file, []byte("test CA bundle"), 0o644))
	t.Setenv("JK_E2E_CA_FILE", file)
	path, err = e2eCAFile()
	require.NoError(t, err)
	require.Equal(t, file, path)
}
