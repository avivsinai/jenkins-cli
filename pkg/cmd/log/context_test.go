package logcmd

import (
	"context"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAdversarialStepPreflightHonorsContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"number":17,"building":false,"result":"FAILURE"}`))
	}))
	defer srv.Close()
	cmd := NewCmdLog(stepLogFactory(t, srv.URL))
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"team/app", "17", "--step", "3", "--plain"})
	start := time.Now()
	require.Error(t, cmd.Execute())
	require.Less(t, time.Since(start), 100*time.Millisecond, "initial log preflight ignored command cancellation")
}
