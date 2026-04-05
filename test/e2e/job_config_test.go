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

	// The transport for `job scan` is the same Jenkins build endpoint used for scans on
	// Multibranch Pipeline jobs. Validate the wrapper against the known-good dogfood job.
	scanJSON, stderr, err := h.runCLI(ctx, "job", "scan", "dogfood/jk-smoke", "--json")
	if err != nil {
		t.Fatalf("job scan failed: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(scanJSON, `"endpoint":"build"`) {
		t.Fatalf("expected scan json to report build endpoint, got: %s", scanJSON)
	}
}
