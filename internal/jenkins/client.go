package jenkins

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"

	"github.com/your-org/jenkins-cli/internal/build"
	"github.com/your-org/jenkins-cli/internal/config"
	"github.com/your-org/jenkins-cli/internal/log"
	"github.com/your-org/jenkins-cli/internal/secret"
)

const (
	crumbEndpoint      = "/crumbIssuer/api/json"
	eventsProbePath    = "/sse-gateway/stats"
	prometheusProbe    = "/prometheus"
	defaultUserAgent   = "jk"
	headerJKClient     = "X-JK-Client"
	headerJKFeatures   = "X-JK-Features"
	defaultFeatures    = "core"
	capabilityCacheTTL = time.Minute
)

// Client provides authenticated communication with Jenkins.
type Client struct {
	resty            *resty.Client
	contextName      string
	ctxConfig        *config.Context
	capabilities     Capabilities
	capMu            sync.RWMutex
	lastCapProbe     time.Time
	crumb            *crumbValue
	crumbMu          sync.Mutex
	crumbUnsupported bool
}

// Capabilities captures Jenkins feature detection results.
type Capabilities struct {
	RunsFacade       bool
	CredentialFacade bool
	Events           bool
	Prometheus       bool
	SSEGateway       bool
}

type crumbValue struct {
	Field string
	Value string
}

type crumbResponse struct {
	Crumb             string `json:"crumb"`
	CrumbRequestField string `json:"crumbRequestField"`
}

type statusResponse struct {
	Version           string   `json:"version"`
	Features          []string `json:"features"`
	MinClient         string   `json:"minClient"`
	RecommendedClient string   `json:"recommendedClient"`
}

// NewClient constructs a Jenkins client for the supplied context.
func NewClient(ctx context.Context, cfg *config.Config, contextName string) (*Client, error) {
	if cfg == nil {
		return nil, errors.New("configuration is required")
	}

	if contextName == "" {
		_, name, err := cfg.ActiveContext()
		if err != nil {
			return nil, err
		}
		contextName = name
	}

	if contextName == "" {
		return nil, errors.New("no active context; use 'jk context use' or provide --context")
	}

	ctxDef, err := cfg.Context(contextName)
	if err != nil {
		return nil, err
	}

	store, err := secret.Open()
	if err != nil {
		return nil, err
	}
	token, err := store.Get(secret.TokenKey(contextName))
	if err != nil {
		return nil, fmt.Errorf("load token for context %s: %w", contextName, err)
	}

	parsedURL, err := url.Parse(ctxDef.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid Jenkins URL for context %s: %w", contextName, err)
	}

	restyClient := resty.New()
	restyClient.SetBaseURL(strings.TrimSuffix(parsedURL.String(), "/"))
	restyClient.SetHeader(headerJKClient, build.Version)
	restyClient.SetHeader(headerJKFeatures, defaultFeatures)
	restyClient.SetHeader("User-Agent", fmt.Sprintf("%s/%s", defaultUserAgent, build.Version))
	restyClient.SetRetryCount(2)
	restyClient.SetRetryWaitTime(500 * time.Millisecond)
	restyClient.SetRetryMaxWaitTime(3 * time.Second)
	restyClient.SetBasicAuth(ctxDef.Username, token)
	restyClient.SetTimeout(30 * time.Second)
	restyClient.SetHeader("Accept", "application/json")

	if ctxDef.Proxy != "" {
		restyClient.SetProxy(ctxDef.Proxy)
	}

	if ctxDef.Insecure {
		restyClient.SetTLSClientConfig(&tls.Config{InsecureSkipVerify: true}) //nolint:gosec // intentional per user configuration
	}

	if ctxDef.CAFile != "" {
		if err := applyCustomCA(restyClient, ctxDef.CAFile); err != nil {
			return nil, err
		}
	}

	client := &Client{
		resty:       restyClient,
		contextName: contextName,
		ctxConfig:   ctxDef,
	}

	if err := client.refreshCapabilities(ctx); err != nil {
		log.L().Warn().Err(err).Msg("capability detection failed")
	}

	return client, nil
}

func applyCustomCA(client *resty.Client, path string) error {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read ca file: %w", err)
	}

	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}

	if ok := pool.AppendCertsFromPEM(bytes); !ok {
		return errors.New("failed to append CA certificate")
	}

	tlsConfig := &tls.Config{RootCAs: pool}
	client.SetTLSClientConfig(tlsConfig)
	return nil
}

// NewRequest creates a prepared resty request.
func (c *Client) NewRequest() *resty.Request {
	return c.resty.R().SetHeader("Accept", "application/json")
}

// Context returns the underlying Jenkins context configuration.
func (c *Client) Context() *config.Context {
	return c.ctxConfig
}

// ContextName exposes the context identifier backing the client.
func (c *Client) ContextName() string {
	return c.contextName
}

// Do executes the request with crumb handling.
func (c *Client) Do(req *resty.Request, method, path string, result interface{}) (*resty.Response, error) {
	if result != nil {
		req.SetResult(result)
	}

	resp, err := c.execute(req, method, path, true)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) execute(req *resty.Request, method, path string, allowRetry bool) (*resty.Response, error) {
	if needsCrumb(method) {
		crumb, err := c.ensureCrumb(req.Context())
		if err != nil {
			return nil, err
		}
		if crumb != nil {
			req.SetHeader(crumb.Field, crumb.Value)
		}
	}

	resp, err := req.Execute(method, path)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() == http.StatusForbidden && allowRetry && needsCrumb(method) {
		c.clearCrumb()
		return c.execute(req, method, path, false)
	}

	return resp, nil
}

func needsCrumb(method string) bool {
	switch strings.ToUpper(method) {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func (c *Client) ensureCrumb(ctx context.Context) (*crumbValue, error) {
	c.crumbMu.Lock()
	defer c.crumbMu.Unlock()

	if c.crumb != nil {
		return c.crumb, nil
	}
	if c.crumbUnsupported {
		return nil, nil
	}

	if ctx == nil {
		ctx = context.Background()
	}

	var result crumbResponse
	resp, err := c.resty.R().SetContext(ctx).SetResult(&result).Get(crumbEndpoint)
	if err != nil {
		return nil, fmt.Errorf("fetch crumb: %w", err)
	}

	switch resp.StatusCode() {
	case http.StatusOK:
		if result.Crumb == "" || result.CrumbRequestField == "" {
			return nil, errors.New("crumb issuer returned empty data")
		}
		c.crumb = &crumbValue{Field: result.CrumbRequestField, Value: result.Crumb}
		return c.crumb, nil
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		c.crumbUnsupported = true
		return nil, nil
	default:
		return nil, fmt.Errorf("crumb issuer error: %s", resp.Status())
	}
}

func (c *Client) clearCrumb() {
	c.crumbMu.Lock()
	defer c.crumbMu.Unlock()
	c.crumb = nil
}

// Capabilities returns the cached capabilities, refreshing if stale.
func (c *Client) Capabilities(ctx context.Context) Capabilities {
	c.capMu.RLock()
	delta := time.Since(c.lastCapProbe)
	caps := c.capabilities
	c.capMu.RUnlock()

	if delta < capabilityCacheTTL {
		return caps
	}

	if err := c.refreshCapabilities(ctx); err != nil {
		log.L().Debug().Err(err).Msg("capability refresh failed")
	}

	c.capMu.RLock()
	defer c.capMu.RUnlock()
	return c.capabilities
}

func (c *Client) refreshCapabilities(ctx context.Context) error {
	c.capMu.Lock()
	defer c.capMu.Unlock()

	if ctx == nil {
		ctx = context.Background()
	}

	var status statusResponse
	resp, err := c.resty.R().SetContext(ctx).SetResult(&status).Get("/jk/api/status")
	if err != nil {
		return fmt.Errorf("probe jk/api/status: %w", err)
	}

	caps := Capabilities{}
	if resp.StatusCode() == http.StatusOK {
		for _, feature := range enumerateFeatures(status.Features) {
			switch feature {
			case "runs":
				caps.RunsFacade = true
			case "credentials":
				caps.CredentialFacade = true
			case "events":
				caps.Events = true
			}
		}
	}

	if ok := c.probeEndpoint(ctx, eventsProbePath); ok {
		caps.SSEGateway = true
	}
	if ok := c.probeEndpoint(ctx, prometheusProbe); ok {
		caps.Prometheus = true
	}

	c.capabilities = caps
	c.lastCapProbe = time.Now()
	return nil
}

func enumerateFeatures(features []string) []string {
	out := make([]string, 0, len(features))
	for _, f := range features {
		trim := strings.TrimSpace(strings.ToLower(f))
		if trim != "" {
			out = append(out, trim)
		}
	}
	return out
}

func (c *Client) probeEndpoint(ctx context.Context, path string) bool {
	resp, err := c.resty.R().SetContext(ctx).SetDoNotParseResponse(true).Head(path)
	if err != nil {
		return false
	}

	status := resp.StatusCode()
	return status >= 200 && status < 400
}
