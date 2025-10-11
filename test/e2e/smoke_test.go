package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

type runSummary struct {
	Number   int64  `json:"number"`
	Result   string `json:"result"`
	Building bool   `json:"building"`
	URL      string `json:"url"`
}

type artifactItem struct {
	FileName     string `json:"fileName"`
	RelativePath string `json:"relativePath"`
	Size         int64  `json:"size"`
}

func TestDogfoodSmoke(t *testing.T) {
	h := requireHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	const jobPath = "dogfood/jk-smoke"

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

	if stdout, stderr, err := h.runCLI(ctx, "job", "ls", "--folder", "dogfood"); err != nil {
		t.Fatalf("job ls failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	} else if !strings.Contains(stdout, "jk-smoke") {
		t.Fatalf("expected job list to contain jk-smoke, got: %s", stdout)
	}

	if stdout, stderr, err := h.runCLI(ctx, "run", "start", jobPath, "--follow", "--interval", "1s"); err != nil {
		runJSON, _, listErr := h.runCLI(ctx, "run", "ls", jobPath, "--limit", "1", "--json")
		if listErr == nil {
			var runs []runSummary
			if jsonErr := json.Unmarshal([]byte(runJSON), &runs); jsonErr == nil && len(runs) > 0 {
				if console, logErr := h.fetchConsoleLog(ctx, jobPath, runs[0].Number); logErr == nil {
					t.Fatalf("run start failed: %v\nstdout: %s\nstderr: %s\nconsole:\n%s", err, stdout, stderr, console)
				}
			}
		}
		t.Fatalf("run start failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	runJSON, stderr, err := h.runCLI(ctx, "run", "ls", jobPath, "--limit", "1", "--json")
	if err != nil {
		t.Fatalf("run ls failed: %v\nstderr: %s", err, stderr)
	}

	var runs []runSummary
	if err := json.Unmarshal([]byte(runJSON), &runs); err != nil {
		var wrapper struct {
			Items []runSummary `json:"items"`
		}
		if err := json.Unmarshal([]byte(runJSON), &wrapper); err != nil {
			t.Fatalf("decode run list: %v\npayload: %s", err, runJSON)
		}
		runs = wrapper.Items
	}
	if len(runs) == 0 {
		t.Fatal("run list returned no entries")
	}
	latest := runs[0]
	if latest.Result != "SUCCESS" || latest.Building {
		t.Fatalf("expected successful build, got %+v", latest)
	}

	console, err := h.fetchConsoleLog(ctx, "dogfood/jk-smoke", latest.Number)
	if err != nil {
		t.Fatalf("fetch console log: %v", err)
	}
	if !strings.Contains(console, "go test ./...") {
		t.Fatalf("console log missing go test invocation:\n%s", console)
	}

	artifactJSON, stderr, err := h.runCLI(ctx, "artifact", "ls", jobPath, fmt.Sprintf("%d", latest.Number), "--json")
	if err != nil {
		t.Fatalf("artifact ls failed: %v\nstderr: %s", err, stderr)
	}
	var artifacts []artifactItem
	if err := json.Unmarshal([]byte(artifactJSON), &artifacts); err != nil {
		t.Fatalf("decode artifact list: %v\npayload: %s", err, artifactJSON)
	}
	found := false
	for _, art := range artifacts {
		if art.RelativePath == "bin/jk" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected archived binary bin/jk, artifacts: %+v", artifacts)
	}
}
