package jenkins

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/avivsinai/jenkins-cli/internal/config"
	"github.com/avivsinai/jenkins-cli/internal/secret"
)

func TestFrontDoorHeaderAndBasicAuthOnEveryRequest(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("KEYRING_BACKEND", "file")
	t.Setenv("KEYRING_FILE_DIR", tmp+"/secrets")
	t.Setenv("JK_ALLOW_INSECURE_STORE", "1")
	t.Setenv("JK_KEYRING_PASSPHRASE", "test-pass")
	t.Setenv("KEYRING_FILE_PASSWORD", "test-pass")

	store, err := secret.Open(secret.WithAllowFileFallback(true))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(secret.TokenKey("iap"), "jenkins-token"); err != nil {
		t.Fatal(err)
	}

	var runs int
	orig := runTokenCommand
	runTokenCommand = func(_ context.Context, argv []string) ([]byte, error) {
		runs++
		return []byte("iap-token\n"), nil
	}
	t.Cleanup(func() { runTokenCommand = orig })

	var mu sync.Mutex
	seen := map[string]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if r.Header.Get("Proxy-Authorization") != "Bearer iap-token" || !ok || user != "alice" || pass != "jenkins-token" {
			t.Errorf("%s %s: missing front door header or Basic auth", r.Method, r.URL.Path)
		}
		mu.Lock()
		seen[r.Method+" "+r.URL.Path] = true
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == crumbEndpoint {
			_, _ = w.Write([]byte(`{"crumb":"c","crumbRequestField":"Jenkins-Crumb"}`))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	cfg := &config.Config{}
	cfg.SetContext("iap", &config.Context{
		URL:                srv.URL,
		Username:           "alice",
		AllowInsecureStore: true,
		FrontDoor: &config.FrontDoor{
			Mode:         config.FrontDoorModeProxyBearerCommand,
			Header:       "Proxy-Authorization",
			TokenCommand: []string{"gcloud", "auth", "print-identity-token"},
		},
	})

	client, err := NewClient(context.Background(), cfg, "iap", WithSkipCapabilityProbe(), WithDisableWarn(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.WhoAmI(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(client.NewRequest(), http.MethodPost, "/job/x/build", nil); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"GET /whoAmI/api/json", "GET " + crumbEndpoint, "POST /job/x/build"} {
		if !seen[want] {
			t.Errorf("request %q not seen", want)
		}
	}
	if runs != 1 {
		t.Errorf("token command ran %d times, want 1", runs)
	}
}
