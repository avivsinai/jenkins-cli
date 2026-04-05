package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestJobConfigConfigureAndScan(t *testing.T) {
	h := requireHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	loginArgs := []string{
		"auth", "login", h.baseURL,
		"--username", h.adminUser,
		"--token", h.adminPassword,
		"--name", "e2e",
		"--set-active",
	}
	if _, stderr, err := h.runCLI(ctx, loginArgs...); err != nil && !strings.Contains(stderr, "already exists") {
		t.Fatalf("login failed: %v\nstderr: %s", err, stderr)
	}

	jobName := fmt.Sprintf("jk-job-config-%d", time.Now().UnixNano())
	jobPath := "dogfood/" + jobName
	const repoOwner = "atlassian"
	const repository = "aui"

	if stdout, stderr, err := h.runCLI(
		ctx,
		"job", "create", jobName,
		"--folder", "dogfood",
		"--repo-owner", repoOwner,
		"--repository", repository,
		"--script-path", "README.md",
	); err != nil {
		t.Fatalf("job create failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	configXML, stderr, err := h.runCLI(ctx, "job", "config", jobPath)
	if err != nil {
		t.Fatalf("job config failed: %v\nstderr: %s", err, stderr)
	}
	if !strings.HasPrefix(strings.TrimSpace(configXML), "<?xml") {
		t.Fatalf("expected raw xml output, got: %s", configXML)
	}
	if !strings.Contains(configXML, "<scriptPath>README.md</scriptPath>") {
		t.Fatalf("expected initial scriptPath in config.xml, got:\n%s", configXML)
	}

	if stdout, stderr, err := h.runCLI(ctx, "job", "configure", jobPath, "--script-path", "package.json"); err != nil {
		t.Fatalf("job configure --script-path failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	updatedConfigXML, stderr, err := h.runCLI(ctx, "job", "config", jobPath)
	if err != nil {
		t.Fatalf("job config after configure failed: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(updatedConfigXML, "<scriptPath>package.json</scriptPath>") {
		t.Fatalf("expected updated scriptPath in config.xml, got:\n%s", updatedConfigXML)
	}

	if stdout, stderr, err := h.runCLIWithInput(ctx, updatedConfigXML, "job", "configure", jobPath, "--stdin"); err != nil {
		t.Fatalf("job configure --stdin failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	roundTripConfigXML, stderr, err := h.runCLI(ctx, "job", "config", jobPath)
	if err != nil {
		t.Fatalf("job config after round trip failed: %v\nstderr: %s", err, stderr)
	}
	if roundTripConfigXML != updatedConfigXML {
		t.Fatalf("expected config round-trip to be stable\nbefore:\n%s\nafter:\n%s", updatedConfigXML, roundTripConfigXML)
	}

	// Scan the multibranch job we just created. The Bitbucket source isn't reachable
	// from CI, so scan will fail with "no buildable sources". That's expected — we
	// only need to verify the type guard allows the request through.
	scanJSON, stderr, err := h.runCLI(ctx, "job", "scan", jobPath, "--json")
	if err != nil {
		// Accept "no buildable sources" as a valid outcome — it means the type guard
		// passed and the scan endpoint was reached.
		if strings.Contains(stderr, "no buildable sources configured") {
			return
		}
		t.Fatalf("job scan failed: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(scanJSON, `"endpoint":"build"`) {
		t.Fatalf("expected scan json to report build endpoint, got: %s", scanJSON)
	}

	// Verify scan rejects non-multibranch jobs.
	_, scanStderr, scanErr := h.runCLI(ctx, "job", "scan", "dogfood/jk-smoke")
	if scanErr == nil {
		t.Fatal("expected scan to reject non-multibranch job dogfood/jk-smoke")
	}
	if !strings.Contains(scanStderr, "not a Multibranch Pipeline") {
		t.Fatalf("expected type guard error, got: %s", scanStderr)
	}
}
