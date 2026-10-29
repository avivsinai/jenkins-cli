package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// The plugin lives only in the disposable test controller. CLI users do not
// need it unless they request run steps / log --step.
func TestPipelineStepsAndReadOnlyStepLog(t *testing.T) {
	h := requireHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if _, stderr, err := h.runCLI(ctx, "auth", "login", h.baseURL, "--username", h.adminUser, "--token", h.adminPassword, "--name", "e2e", "--set-active"); err != nil {
		t.Fatalf("admin login: %v\n%s", err, stderr)
	}
	name := fmt.Sprintf("jk-step-logs-%d", time.Now().UnixNano())
	job := "dogfood/" + name
	const configXML = `<flow-definition plugin="workflow-job"><definition class="org.jenkinsci.plugins.workflow.cps.CpsFlowDefinition" plugin="workflow-cps"><script>node { stage('Checks') { echo 'unrelated outer marker'; sh(label: 'target check', script: 'echo isolated failure marker; exit 17') } }</script><sandbox>true</sandbox></definition><disabled>false</disabled></flow-definition>`
	if _, stderr, err := h.runCLIWithInput(ctx, configXML, "job", "create", name, "--folder", "dogfood", "--stdin"); err != nil {
		t.Fatalf("create pipeline: %v\n%s", err, stderr)
	}
	// A failed build is expected. Following it still waits until the log and
	// graph snapshots are stable before the assertions below.
	_, _, _ = h.runCLI(ctx, "run", "start", job, "--follow", "--interval", "1s")
	if ctx.Err() != nil {
		t.Fatal(ctx.Err())
	}

	reader := fmt.Sprintf("step-reader-%d", time.Now().UnixNano())
	setup := fmt.Sprintf(`
import jenkins.model.Jenkins
import hudson.model.Item
import hudson.security.GlobalMatrixAuthorizationStrategy

def j = Jenkins.get()
j.securityRealm.createAccount('%s', 'reader-test-password')
def strategy = new GlobalMatrixAuthorizationStrategy()
strategy.add(Jenkins.ADMINISTER, '%s')
strategy.add(Jenkins.READ, '%s')
strategy.add(Item.READ, '%s')
j.setAuthorizationStrategy(strategy)
`, reader, h.adminUser, reader, reader)
	if err := h.runScript(ctx, h.adminUser, h.adminPassword, setup); err != nil {
		t.Fatalf("configure read-only user: %v", err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		err := h.runScript(cleanupCtx, h.adminUser, h.adminPassword, `def s = new hudson.security.FullControlOnceLoggedInAuthorizationStrategy(); s.setAllowAnonymousRead(false); jenkins.model.Jenkins.get().setAuthorizationStrategy(s)`)
		if err != nil {
			t.Errorf("restore authorization strategy: %v", err)
		}
	}()
	if _, stderr, err := h.runCLI(ctx, "auth", "login", h.baseURL, "--username", reader, "--token", "reader-test-password", "--name", "steps-reader"); err != nil {
		t.Fatalf("read-only login: %v\n%s", err, stderr)
	}
	stepsJSON, stderr, err := h.runCLI(ctx, "run", "steps", job, "1", "--context", "steps-reader", "--json")
	if err != nil {
		t.Fatalf("read-only run steps: %v\n%s", err, stderr)
	}
	var result struct {
		Complete bool `json:"complete"`
		Steps    []struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			State string `json:"state"`
		} `json:"steps"`
	}
	if err := json.Unmarshal([]byte(stepsJSON), &result); err != nil {
		t.Fatalf("decode steps: %v\n%s", err, stepsJSON)
	}
	if !result.Complete {
		t.Fatalf("expected completed Pipeline graph: %s", stepsJSON)
	}
	var id string
	for _, step := range result.Steps {
		if step.Name == "target check" {
			id = step.ID
			if step.State != "failure" {
				t.Fatalf("expected failed labelled step: %+v", step)
			}
		}
	}
	if id == "" {
		t.Fatalf("labelled shell step missing: %s", stepsJSON)
	}
	logText, stderr, err := h.runCLI(ctx, "log", job, "1", "--context", "steps-reader", "--step", id, "--plain")
	if err != nil {
		t.Fatalf("read-only isolated log: %v\n%s", err, stderr)
	}
	if !strings.Contains(logText, "isolated failure marker") || strings.Contains(logText, "unrelated outer marker") {
		t.Fatalf("expected only selected step log: %s", logText)
	}
	_, stderr, err = h.runCLI(ctx, "run", "start", job, "--context", "steps-reader")
	if err == nil || !strings.Contains(stderr, "403") {
		t.Fatalf("read-only user should not have build permission: %v\n%s", err, stderr)
	}
}
