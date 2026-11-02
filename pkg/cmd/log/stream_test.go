package logcmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/avivsinai/jenkins-cli/internal/config"
	"github.com/avivsinai/jenkins-cli/internal/jenkins"
	"github.com/avivsinai/jenkins-cli/pkg/cmdutil"
)

func TestLogFollowStreamsLargeCompletedResponse(t *testing.T) {
	const size = 256 * 1024 * 1024
	firstWrite := make(chan struct{})
	serverErr := make(chan error, 1)
	client, cfg := logTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/job/main/7/api/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"building":false,"result":"SUCCESS"}`)
		case "/job/main/7/logText/progressiveText":
			if r.URL.Query().Get("start") != "0" {
				serverErr <- fmt.Errorf("unexpected offset %q", r.URL.Query().Get("start"))
				return
			}
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("Content-Length", strconv.Itoa(size))
			w.Header().Set("X-Text-Size", strconv.Itoa(size))
			w.Header().Set("X-More-Data", "false")
			// Flush only a prefix. The rest cannot arrive until the command
			// writes output, so buffering the response deadlocks until timeout.
			prefix := bytes.Repeat([]byte("x"), 64*1024)
			if _, err := w.Write(prefix); err != nil {
				serverErr <- err
				return
			}
			w.(http.Flusher).Flush()
			timer := time.NewTimer(5 * time.Second)
			defer timer.Stop()
			select {
			case <-firstWrite:
			case <-timer.C:
				serverErr <- fmt.Errorf("no output before completed response finished")
				return
			case <-r.Context().Done():
				serverErr <- r.Context().Err()
				return
			}
			// Generate the large remainder with constant memory.
			n, err := io.CopyN(w, zeroReader{}, size-int64(len(prefix)))
			if err == nil && n != size-int64(len(prefix)) {
				err = io.ErrShortWrite
			}
			serverErr <- err
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	f := &cmdutil.Factory{
		Config: func() (*config.Config, error) { return cfg, nil },
		JenkinsClient: func(context.Context, string, ...jenkins.ClientOption) (*jenkins.Client, error) {
			return client, nil
		},
	}
	out := &streamCountingWriter{firstWrite: firstWrite}
	cmd := NewCmdLog(f)
	cmd.SetOut(out)
	cmd.SetErr(io.Discard)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"main", "7", "--follow", "--plain"})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd.SetContext(ctx)
	err := cmd.Execute()
	select {
	case serveErr := <-serverErr:
		require.NoError(t, serveErr, "completed response must reach output before EOF")
	case <-ctx.Done():
		t.Fatalf("stream handler did not finish: %v (command error: %v)", ctx.Err(), err)
	}
	require.NoError(t, err)
	require.EqualValues(t, size, out.total)
	require.LessOrEqual(t, out.maxWrite, 32*1024, "output writes must stay bounded")
}

type streamCountingWriter struct {
	firstWrite chan struct{}
	total      int64
	maxWrite   int
}

func (w *streamCountingWriter) Write(p []byte) (int, error) {
	if w.total == 0 {
		close(w.firstWrite)
	}
	w.total += int64(len(p))
	w.maxWrite = max(w.maxWrite, len(p))
	return len(p), nil
}

func TestLogFollowPreservesProgressiveOffsets(t *testing.T) {
	var starts []string
	client, cfg := logTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/job/main/7/api/json" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"building":false,"result":"SUCCESS"}`)
			return
		}
		starts = append(starts, r.URL.Query().Get("start"))
		switch len(starts) {
		case 1:
			// Jenkins' byte offset need not equal the decoded output length.
			w.Header().Set("X-Text-Size", "123")
			w.Header().Set("X-More-Data", "True")
			_, _ = io.WriteString(w, "first\n")
		case 2:
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		case 3:
			w.Header().Set("X-More-Data", "false")
			_, _ = io.WriteString(w, "last\n")
		default:
			t.Errorf("unexpected extra progressive request")
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	f := &cmdutil.Factory{
		Config: func() (*config.Config, error) { return cfg, nil },
		JenkinsClient: func(context.Context, string, ...jenkins.ClientOption) (*jenkins.Client, error) {
			return client, nil
		},
	}
	var out bytes.Buffer
	cmd := NewCmdLog(f)
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"main", "7", "--follow", "--plain", "--interval", "1ms"})
	require.NoError(t, cmd.Execute())
	require.Equal(t, []string{"0", "123", "0"}, starts)
	require.Equal(t, "first\nlast\n", out.String())
}
