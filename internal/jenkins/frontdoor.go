package jenkins

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/avivsinai/jenkins-cli/internal/config"
)

const frontDoorCommandTimeout = time.Minute

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
// the Jenkins host. The token comes from the configured command, which runs
// once per process; its result (token or error) is cached. Requests to other
// hosts (for example artifact redirects to object storage) do not get the
// header.
type frontDoorTransport struct {
	base        http.RoundTripper
	host        string
	header      string
	argv        []string
	contextName string

	mu    sync.Mutex
	done  bool
	token string
	err   error
}

func newFrontDoorTransport(base http.RoundTripper, host, contextName string, fd *config.FrontDoor) *frontDoorTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	return &frontDoorTransport{
		base:        base,
		host:        host,
		header:      http.CanonicalHeaderKey(strings.TrimSpace(fd.Header)),
		argv:        append([]string(nil), fd.TokenCommand...),
		contextName: contextName,
	}
}

func (t *frontDoorTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL == nil || !strings.EqualFold(req.URL.Host, t.host) {
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

	if t.done {
		return t.token, t.err
	}

	out, err := runTokenCommand(ctx, t.argv)
	if err != nil && ctx.Err() != nil {
		// The request was canceled; do not cache that as a command failure.
		return "", ctx.Err()
	}
	token := strings.TrimSpace(string(out))
	if err == nil && token == "" {
		err = errors.New("command printed no token")
	}
	if err != nil {
		t.err = fmt.Errorf("front door token command for context %q (%s) failed: %w",
			t.contextName, t.argv[0], err)
	} else {
		t.token = token
	}
	t.done = true
	return t.token, t.err
}
