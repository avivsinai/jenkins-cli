package jenkins

import (
	"reflect"
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
