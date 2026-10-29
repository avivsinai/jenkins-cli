package jenkins

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/avivsinai/jenkins-cli/internal/config"
)

// HasEnvironmentAuth reports whether an environment authentication setting is
// active. Empty variables are treated as unset.
func HasEnvironmentAuth() bool {
	for _, key := range []string{"JK_URL", "JK_USER", "JK_TOKEN"} {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			return true
		}
	}
	return false
}

// ResolveAuthContext resolves connection metadata without opening a secret
// store or mutating cfg. A complete environment tuple supplies an ephemeral
// context only when no stored context is selected. With a selected context,
// environment connection metadata must agree with it to avoid sending a token
// intended for one controller to another.
func ResolveAuthContext(cfg *config.Config, name string) (*config.Context, string, string, error) {
	if cfg == nil {
		return nil, "", "", errors.New("configuration is required")
	}
	if name == "" {
		_, activeName, err := cfg.ActiveContext()
		if err != nil {
			return nil, activeName, "", err
		}
		name = activeName
	}
	endpoint := strings.TrimSpace(os.Getenv("JK_URL"))
	username := strings.TrimSpace(os.Getenv("JK_USER"))
	token := strings.TrimSpace(os.Getenv("JK_TOKEN"))
	source := "stored"
	if token != "" {
		source = "environment"
	}
	if endpoint != "" || username != "" {
		if endpoint == "" || username == "" || token == "" {
			return nil, name, source, errors.New("JK_URL and JK_USER require a complete JK_URL, JK_USER and JK_TOKEN tuple")
		}
		parsed, err := url.Parse(endpoint)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return nil, name, source, errors.New("JK_URL must be an http or https controller URL without credentials, query or fragment")
		}
		endpoint = strings.TrimRight(endpoint, "/")
	}
	if name == "" {
		if endpoint != "" {
			return &config.Context{URL: endpoint, Username: username}, "environment", source, nil
		}
		if token != "" {
			return nil, "", source, errors.New("JK_TOKEN requires a selected context or both JK_URL and JK_USER")
		}
		return nil, "", source, errors.New("no active context; use 'jk context use' or provide --context, or set JK_URL, JK_USER and JK_TOKEN")
	}
	definition, err := cfg.Context(name)
	if err != nil {
		return nil, name, source, fmt.Errorf("context %q: %w", name, err)
	}
	if definition == nil {
		return nil, name, source, fmt.Errorf("context %q has no configuration", name)
	}
	if endpoint != "" && (endpoint != strings.TrimRight(definition.URL, "/") || username != definition.Username) {
		return nil, name, source, fmt.Errorf("JK_URL/JK_USER conflict with selected context %q; unset them or select the matching context", name)
	}
	return definition, name, source, nil
}
