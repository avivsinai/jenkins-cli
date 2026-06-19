package jenkins

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/go-resty/resty/v2"
)

// restyDisableWarn reads the DisableWarn field from a resty.Client using reflection,
// since resty does not expose a getter for this field.
func restyDisableWarn(t *testing.T, rc *resty.Client) bool {
	t.Helper()
	v := reflect.ValueOf(rc).Elem().FieldByName("DisableWarn")
	if !v.IsValid() {
		t.Fatal("resty.Client has no DisableWarn field; resty API may have changed")
	}
	return v.Bool()
}

func TestWithDisableWarnTrue(t *testing.T) {
	rc := resty.New()
	rs := resty.New()
	c := &Client{resty: rc, restyStream: rs}

	opt := WithDisableWarn(true)
	opt(c)

	if !restyDisableWarn(t, rc) {
		t.Error("expected resty client DisableWarn=true after WithDisableWarn(true), got false")
	}
	if !restyDisableWarn(t, rs) {
		t.Error("expected restyStream DisableWarn=true after WithDisableWarn(true), got false")
	}
}

func TestWithDisableWarnFalse(t *testing.T) {
	rc := resty.New()
	rc.SetDisableWarn(true)
	rs := resty.New()
	rs.SetDisableWarn(true)
	c := &Client{resty: rc, restyStream: rs}

	// WithDisableWarn(false) should be a no-op — it must NOT clear an existing true value.
	opt := WithDisableWarn(false)
	opt(c)

	// The original true value should be unchanged because the option only acts when disable==true.
	if !restyDisableWarn(t, rc) {
		t.Error("WithDisableWarn(false) should not clear an existing DisableWarn=true on resty client")
	}
}

func TestWithDisableWarnNilStream(t *testing.T) {
	rc := resty.New()
	c := &Client{resty: rc, restyStream: nil}

	// Must not panic when restyStream is nil.
	opt := WithDisableWarn(true)
	opt(c)

	if !restyDisableWarn(t, rc) {
		t.Error("expected resty client DisableWarn=true, got false")
	}
}

func TestClientOptionDefaultNoDisableWarn(t *testing.T) {
	rc := resty.New()
	rs := resty.New()
	c := &Client{resty: rc, restyStream: rs}

	// Applying no options must leave DisableWarn at its default (false).
	var opts []ClientOption
	for _, opt := range opts {
		opt(c)
	}

	if restyDisableWarn(t, rc) {
		t.Error("expected resty client DisableWarn=false by default, got true")
	}
	if restyDisableWarn(t, rs) {
		t.Error("expected restyStream DisableWarn=false by default, got true")
	}
}

func TestSetDisableWarn(t *testing.T) {
	rc := resty.New()
	rs := resty.New()
	c := &Client{resty: rc, restyStream: rs}

	c.SetDisableWarn(true)

	if !restyDisableWarn(t, rc) {
		t.Error("SetDisableWarn(true): expected resty DisableWarn=true, got false")
	}
	if !restyDisableWarn(t, rs) {
		t.Error("SetDisableWarn(true): expected restyStream DisableWarn=true, got false")
	}
}

func TestSetDisableWarnNilStream(t *testing.T) {
	rc := resty.New()
	c := &Client{resty: rc, restyStream: nil}

	// Must not panic when restyStream is nil.
	c.SetDisableWarn(true)

	if !restyDisableWarn(t, rc) {
		t.Error("SetDisableWarn(true): expected resty DisableWarn=true, got false")
	}
}

func TestDoRejectsNonSuccessWhenDecodingResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"number":123}`))
	}))
	defer server.Close()

	c := &Client{resty: resty.New().SetBaseURL(server.URL)}

	var out struct {
		Number int `json:"number"`
	}
	resp, err := c.Do(c.NewRequest(), http.MethodGet, "/run/api/json", &out)

	if err == nil {
		t.Fatal("expected non-success response to return an error")
	}
	if resp == nil || resp.StatusCode() != http.StatusUnauthorized {
		t.Fatalf("expected 401 response to be returned with the error, got %#v", resp)
	}
	if out.Number != 0 {
		t.Fatalf("expected failed response not to populate result, got %d", out.Number)
	}
	if !strings.Contains(err.Error(), "401 Unauthorized") {
		t.Fatalf("expected status in error, got %q", err.Error())
	}
}

func TestDoRawPreservesNonSuccessResponseForStatusCallers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("missing"))
	}))
	defer server.Close()

	c := &Client{resty: resty.New().SetBaseURL(server.URL)}

	resp, err := c.DoRaw(c.NewRequest(), http.MethodGet, "/optional/api/json", nil)

	if err != nil {
		t.Fatalf("expected raw request to preserve 404 response without error, got %v", err)
	}
	if resp.StatusCode() != http.StatusNotFound {
		t.Fatalf("expected 404 response, got %s", resp.Status())
	}
	if resp.String() != "missing" {
		t.Fatalf("expected response body to remain available, got %q", resp.String())
	}
}
