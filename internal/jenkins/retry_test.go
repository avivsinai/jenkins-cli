package jenkins

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/avivsinai/jenkins-cli/internal/config"
)

func retryTestClient(t *testing.T, url string) *Client {
	t.Helper()
	t.Setenv("JK_TOKEN", "synthetic-token")
	t.Setenv("JK_URL", "")
	t.Setenv("JK_USER", "")
	cfg := &config.Config{Contexts: map[string]*config.Context{
		"test": {URL: url, Username: "test"},
	}}
	client, err := NewClient(context.Background(), cfg, "test", WithSkipCapabilityProbe(), WithDisableWarn(true))
	if err != nil {
		t.Fatal(err)
	}
	client.resty.SetRetryWaitTime(time.Millisecond).SetRetryMaxWaitTime(time.Millisecond)
	client.restyStream.SetRetryWaitTime(time.Millisecond).SetRetryMaxWaitTime(time.Millisecond)
	return client
}

func dropRetryTestReply(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	if _, err := io.Copy(io.Discard, r.Body); err != nil {
		t.Error(err)
		return
	}
	conn, _, err := w.(http.Hijacker).Hijack()
	if err != nil {
		t.Error(err)
		return
	}
	if err := conn.Close(); err != nil {
		t.Error(err)
	}
}

func TestMutationTransportFailureIsNotRetried(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
			t.Run(fmt.Sprintf("%s/stream=%t", method, stream), func(t *testing.T) {
				var accepted atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == crumbEndpoint {
						w.WriteHeader(http.StatusNotFound)
						return
					}
					accepted.Add(1)
					dropRetryTestReply(t, w, r)
				}))
				defer server.Close()
				client := retryTestClient(t, server.URL)
				req := client.NewRequest()
				if stream {
					req = client.NewStreamingRequest()
				}
				_, err := client.Do(req.SetBody("action"), method, "/job/example/build", nil)
				if err == nil {
					t.Error("lost mutation reply must leave an error")
				}
				if got := accepted.Load(); got != 1 {
					t.Errorf("accepted mutation requests = %d, want 1", got)
				}
			})
		}
	}
}

func TestReadTransportFailureIsRetried(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			t.Run(fmt.Sprintf("%s/stream=%t", method, stream), func(t *testing.T) {
				var requests atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if requests.Add(1) == 1 {
						dropRetryTestReply(t, w, r)
						return
					}
					w.WriteHeader(http.StatusOK)
				}))
				defer server.Close()
				client := retryTestClient(t, server.URL)
				req := client.NewRequest()
				if stream {
					req = client.NewStreamingRequest()
				}
				if _, err := client.Do(req, method, "/read", nil); err != nil {
					t.Fatal(err)
				}
				if got := requests.Load(); got != 2 {
					t.Errorf("read requests = %d, want 2", got)
				}
			})
		}
	}
}

func TestCrumbRefreshDoesNotRetryTransportFailure(t *testing.T) {
	for _, refusal := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		for _, outcome := range []string{"accepted", "lost reply", "refused"} {
			t.Run(fmt.Sprintf("%d/%s", refusal, outcome), func(t *testing.T) {
				var crumbs, attempts, accepted atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == crumbEndpoint {
						w.Header().Set("Content-Type", "application/json")
						fmt.Fprintf(w, `{"crumbRequestField":"Jenkins-Crumb","crumb":"c%d"}`, crumbs.Add(1))
						return
					}
					attempt := attempts.Add(1)
					if got, want := r.Header.Get("Jenkins-Crumb"), fmt.Sprintf("c%d", attempt); got != want {
						t.Errorf("crumb = %q, want %q", got, want)
					}
					if attempt == 1 || outcome == "refused" {
						w.WriteHeader(refusal)
						return
					}
					accepted.Add(1)
					if outcome == "lost reply" {
						dropRetryTestReply(t, w, r)
						return
					}
					w.WriteHeader(http.StatusCreated)
				}))
				defer server.Close()
				client := retryTestClient(t, server.URL)
				_, err := client.Do(client.NewRequest().SetBody("action"), http.MethodPost, "/job/example/build", nil)
				if (err != nil) != (outcome != "accepted") {
					t.Errorf("outcome %q, error = %v", outcome, err)
				}
				if got := attempts.Load(); got != 2 {
					t.Errorf("mutation attempts = %d, want 2 (one refused, one refreshed)", got)
				}
				if got := crumbs.Load(); got != 2 {
					t.Errorf("crumb fetches = %d, want 2", got)
				}
				wantAccepted := int32(1)
				if outcome == "refused" {
					wantAccepted = 0
				}
				if got := accepted.Load(); got != wantAccepted {
					t.Errorf("accepted mutations = %d, want %d", got, wantAccepted)
				}
			})
		}
	}
}
