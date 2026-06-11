package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestGoogleLoginRealmAPIToken proves the documented claim for SSO-realm
// controllers (issue #77): Jenkins core validates API tokens before the
// security realm, so Basic auth with a real API token keeps working when the
// realm is the google-login plugin — and password Basic auth stops working.
func TestGoogleLoginRealmAPIToken(t *testing.T) {
	h := requireHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	apiToken, err := h.mintAPIToken(ctx, "jk-e2e-sso")
	if err != nil {
		t.Fatalf("mint API token: %v", err)
	}

	// Login + verification under the local realm with a real API token.
	loginArgs := []string{
		"auth", "login", h.baseURL,
		"--username", h.adminUser,
		"--token", apiToken,
		"--name", "sso-e2e",
		"--set-active=false",
	}
	if stdout, stderr, err := h.runCLI(ctx, loginArgs...); err != nil {
		t.Fatalf("login with API token failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	// Restore the local realm no matter how the test exits. The restore script
	// authenticates with the API token, which stays valid under the google
	// realm; a follow-up password-authenticated request proves the restore.
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cleanupCancel()

		restore := `import hudson.security.HudsonPrivateSecurityRealm
jenkins.model.Jenkins.get().setSecurityRealm(new HudsonPrivateSecurityRealm(false))
println("restored")`
		if err := h.runScript(cleanupCtx, h.adminUser, apiToken, restore); err != nil {
			t.Errorf("restore local security realm: %v", err)
			return
		}
		if err := h.checkPasswordAuth(cleanupCtx); err != nil {
			t.Errorf("password auth not working after realm restore: %v", err)
		}
	})

	// Switch to the google-login realm transiently (no save): browser SSO
	// realm with throwaway OAuth client credentials.
	switchScript := `import org.jenkinsci.plugins.googlelogin.GoogleOAuth2SecurityRealm
jenkins.model.Jenkins.get().setSecurityRealm(new GoogleOAuth2SecurityRealm("fake-client-id", "fake-client-secret", "example.com"))
println("switched")`
	if err := h.runScript(ctx, h.adminUser, h.adminPassword, switchScript); err != nil {
		t.Fatalf("switch to google-login realm: %v", err)
	}

	// GET path: API token must authenticate reads under the google realm.
	stdout, stderr, err := h.runCLI(ctx, "--context", "sso-e2e", "job", "ls", "--folder", "dogfood")
	if err != nil {
		t.Fatalf("job ls under google-login realm failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "jk-smoke") {
		t.Fatalf("expected job list to contain jk-smoke, got: %s", stdout)
	}

	// POST path: round-trip config.xml (fetch, then post the same payload)
	// proves crumb fetch + authenticated POST under the google realm.
	configXML, stderr, err := h.runCLI(ctx, "--context", "sso-e2e", "job", "config", "dogfood/jk-smoke")
	if err != nil {
		t.Fatalf("job config under google-login realm failed: %v\nstderr: %s", err, stderr)
	}
	configPath := filepath.Join(t.TempDir(), "jk-smoke.config.xml")
	if err := os.WriteFile(configPath, []byte(configXML), 0o600); err != nil {
		t.Fatalf("write config.xml: %v", err)
	}
	if stdout, stderr, err := h.runCLI(ctx, "--context", "sso-e2e", "job", "configure", "dogfood/jk-smoke", "--file", configPath); err != nil {
		t.Fatalf("job configure under google-login realm failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	// Negative: password Basic auth is rejected by the google realm, so login
	// verification must fail with SSO guidance and roll the context back.
	negativeArgs := []string{
		"auth", "login", h.baseURL,
		"--username", h.adminUser,
		"--token", h.adminPassword,
		"--name", "sso-neg",
		"--set-active=false",
	}
	stdout, stderr, err = h.runCLI(ctx, negativeArgs...)
	if err == nil {
		t.Fatalf("expected password login to fail under google-login realm\nstdout: %s\nstderr: %s", stdout, stderr)
	}
	if !strings.Contains(stderr, "API token") {
		t.Fatalf("expected SSO guidance mentioning API token in stderr, got: %s", stderr)
	}
	contexts, stderr, err := h.runCLI(ctx, "context", "ls")
	if err != nil {
		t.Fatalf("context ls failed: %v\nstderr: %s", err, stderr)
	}
	if strings.Contains(contexts, "sso-neg") {
		t.Fatalf("context sso-neg should have been rolled back, got: %s", contexts)
	}
}

// mintAPIToken creates a real Jenkins API token for the admin user through
// the token-generation endpoint (crumb + session cookie + password auth).
func (h *harness) mintAPIToken(ctx context.Context, tokenName string) (string, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 15 * time.Second, Jar: jar}

	crumbField, crumbValue, err := h.fetchCrumb(ctx, client)
	if err != nil {
		return "", err
	}

	endpoint := fmt.Sprintf("%s/me/descriptorByName/jenkins.security.ApiTokenProperty/generateNewToken", h.baseURL)
	form := url.Values{"newTokenName": {tokenName}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(h.adminUser, h.adminPassword)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set(crumbField, crumbValue)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("generateNewToken: status %d: %s", resp.StatusCode, string(body))
	}

	var parsed struct {
		Data struct {
			TokenValue string `json:"tokenValue"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("decode generateNewToken response: %w (%s)", err, string(body))
	}
	if parsed.Data.TokenValue == "" {
		return "", fmt.Errorf("generateNewToken returned empty token: %s", string(body))
	}
	return parsed.Data.TokenValue, nil
}

// runScript executes a Groovy script through the Jenkins script console.
func (h *harness) runScript(ctx context.Context, user, secret, script string) error {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 30 * time.Second, Jar: jar}

	crumbField, crumbValue, err := h.fetchCrumbAs(ctx, client, user, secret)
	if err != nil {
		return err
	}

	form := url.Values{"script": {script}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.baseURL+"/scriptText", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.SetBasicAuth(user, secret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set(crumbField, crumbValue)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("scriptText: status %d: %s", resp.StatusCode, string(body))
	}
	if strings.Contains(string(body), "Exception") {
		return fmt.Errorf("scriptText returned exception: %s", string(body))
	}
	return nil
}

func (h *harness) fetchCrumb(ctx context.Context, client *http.Client) (string, string, error) {
	return h.fetchCrumbAs(ctx, client, h.adminUser, h.adminPassword)
}

func (h *harness) fetchCrumbAs(ctx context.Context, client *http.Client, user, secret string) (string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.baseURL+"/crumbIssuer/api/json", nil)
	if err != nil {
		return "", "", err
	}
	req.SetBasicAuth(user, secret)

	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("crumbIssuer: status %d: %s", resp.StatusCode, string(body))
	}

	var parsed struct {
		Crumb             string `json:"crumb"`
		CrumbRequestField string `json:"crumbRequestField"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", "", fmt.Errorf("decode crumb response: %w", err)
	}
	if parsed.Crumb == "" || parsed.CrumbRequestField == "" {
		return "", "", fmt.Errorf("crumb response missing fields: %s", string(body))
	}
	return parsed.CrumbRequestField, parsed.Crumb, nil
}

// checkPasswordAuth confirms password Basic auth works (local realm active).
func (h *harness) checkPasswordAuth(ctx context.Context) error {
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.baseURL+"/api/json", nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(h.adminUser, h.adminPassword)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("password auth check: status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}
