package jenkins

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/avivsinai/jenkins-cli/internal/config"
)

const (
	frontDoorCommandTimeout = time.Minute
	// frontDoorTokenRefresh re-runs the token command before short-lived
	// tokens expire (Google IAP identity tokens last one hour).
	frontDoorTokenRefresh = 30 * time.Minute
)

// runTokenCommand executes a front door token command. Tests replace it.
var runTokenCommand = execTokenCommand

func execTokenCommand(ctx context.Context, argv []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, frontDoorCommandTimeout)
	defer cancel()

	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec // user-configured argv, executed without a shell
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := lastLine(stderr.String()); msg != "" {
			return nil, fmt.Errorf("%w: %s", err, msg)
		}
		return nil, err
	}
	return out, nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	line := strings.TrimSpace(lines[len(lines)-1])
	const maxLen = 200
	if len(line) > maxLen {
		line = line[:maxLen] + "..."
	}
	return line
}

// frontDoorTransport adds "<header>: Bearer <token>" to every request sent to
// the Jenkins host. The token comes from the configured command; it is cached
// and the command runs again once the token is older than
// frontDoorTokenRefresh. A command failure fails only the current request.
// Requests to other hosts (for example artifact redirects to object storage)
// do not get the header.
type frontDoorTransport struct {
	base        http.RoundTripper
	hostPort    string
	header      string
	argv        []string
	contextName string

	mu      sync.Mutex
	token   string
	fetched time.Time
}

func newFrontDoorTransport(base http.RoundTripper, target *url.URL, contextName string, fd *config.FrontDoor) *frontDoorTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	return &frontDoorTransport{
		base:        base,
		hostPort:    canonicalHostPort(target),
		header:      http.CanonicalHeaderKey(strings.TrimSpace(fd.Header)),
		argv:        append([]string(nil), fd.TokenCommand...),
		contextName: contextName,
	}
}

// canonicalHostPort returns "host:port" with the host lowercased and a
// missing port replaced by the scheme default.
func canonicalHostPort(u *url.URL) string {
	port := u.Port()
	if port == "" {
		switch strings.ToLower(u.Scheme) {
		case "http":
			port = "80"
		case "https":
			port = "443"
		}
	}
	return net.JoinHostPort(strings.ToLower(u.Hostname()), port)
}

func (t *frontDoorTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL == nil || canonicalHostPort(req.URL) != t.hostPort {
		return t.base.RoundTrip(req)
	}

	token, err := t.tokenFor(req.Context())
	if err != nil {
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return nil, err
	}

	out := req.Clone(req.Context())
	out.Header.Set(t.header, "Bearer "+token)
	return t.base.RoundTrip(out)
}

func (t *frontDoorTransport) tokenFor(ctx context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.token != "" && time.Since(t.fetched) < frontDoorTokenRefresh {
		return t.token, nil
	}

	out, err := runTokenCommand(ctx, t.argv)
	if err != nil && ctx.Err() != nil {
		return "", ctx.Err()
	}
	token := strings.TrimSpace(string(out))
	if err == nil && token == "" {
		err = errors.New("command printed no token")
	}
	if err != nil {
		return "", fmt.Errorf("front door token command for context %q (%s) failed: %w",
			t.contextName, t.argv[0], err)
	}
	t.token = token
	t.fetched = time.Now()
	return token, nil
}
