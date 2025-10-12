package e2e

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/build"
	containertypes "github.com/docker/docker/api/types/container"
	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

type harness struct {
	container     tc.Container
	baseURL       string
	cliPath       string
	configDir     string
	secretsDir    string
	repoRoot      string
	bareRepoPath  string
	adminUser     string
	adminPassword string
}

var (
	globalHarness *harness
	skipReason    string
)

func ptr(s string) *string {
	return &s
}

func TestMain(m *testing.M) {
	ctx := context.Background()
	if os.Getenv("JK_E2E_DISABLE") == "1" {
		skipReason = "disabled via JK_E2E_DISABLE"
		os.Exit(m.Run())
		return
	}

	if _, err := exec.LookPath("docker"); err != nil {
		skipReason = "docker executable not found; e2e tests require Docker"
		os.Exit(m.Run())
		return
	}

	h, err := newHarness(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create e2e harness: %v\n", err)
		os.Exit(1)
	}
	globalHarness = h

	code := m.Run()

	if globalHarness != nil {
		if err := globalHarness.Close(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "failed to tear down e2e harness: %v\n", err)
			if code == 0 {
				code = 1
			}
		}
	}

	os.Exit(code)
}

func requireHarness(t *testing.T) *harness {
	if skipReason != "" {
		t.Skip(skipReason)
	}
	if globalHarness == nil {
		t.Fatal("e2e harness not initialized")
	}
	return globalHarness
}

func newHarness(ctx context.Context) (*harness, error) {
	repoRoot, err := repoRoot()
	if err != nil {
		return nil, err
	}

	tmpRoot, err := os.MkdirTemp("", "jk-e2e-*")
	if err != nil {
		return nil, fmt.Errorf("create temp root: %w", err)
	}

	configDir := filepath.Join(tmpRoot, "config")
	secretsDir := filepath.Join(tmpRoot, "secrets")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return nil, fmt.Errorf("create config dir: %w", err)
	}
	if err := os.MkdirAll(secretsDir, 0o700); err != nil {
		return nil, fmt.Errorf("create secrets dir: %w", err)
	}

	cliPath := filepath.Join(tmpRoot, "jk")
	if err := buildCLI(cliPath, repoRoot); err != nil {
		return nil, err
	}

	bareRepoPath, err := exportBareRepository(repoRoot)
	if err != nil {
		return nil, err
	}

	container, baseURL, err := launchJenkins(ctx, repoRoot, bareRepoPath)
	if err != nil {
		return nil, err
	}

	h := &harness{
		container:     container,
		baseURL:       baseURL,
		cliPath:       cliPath,
		configDir:     configDir,
		secretsDir:    secretsDir,
		repoRoot:      repoRoot,
		bareRepoPath:  bareRepoPath,
		adminUser:     "admin",
		adminPassword: "admin123",
	}

	if err := h.waitForJob(ctx, "dogfood/jk-smoke", 3*time.Minute); err != nil {
		_ = container.Terminate(ctx)
		return nil, err
	}

	return h, nil
}

func (h *harness) Close(ctx context.Context) error {
	var errs []error

	if h.container != nil {
		if err := h.container.Terminate(ctx); err != nil {
			errs = append(errs, fmt.Errorf("terminate container: %w", err))
		}
	}

	if h.bareRepoPath != "" {
		if err := os.RemoveAll(filepath.Dir(h.bareRepoPath)); err != nil {
			errs = append(errs, fmt.Errorf("remove bare repo: %w", err))
		}
	}

	if h.cliPath != "" {
		if err := os.RemoveAll(filepath.Dir(h.cliPath)); err != nil {
			errs = append(errs, fmt.Errorf("remove cli snapshot: %w", err))
		}
	}

	return errors.Join(errs...)
}

func repoRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("determine repo root: %w (%s)", err, string(out))
	}
	return strings.TrimSpace(string(out)), nil
}

func buildCLI(dest, repoRoot string) error {
	cmd := exec.Command("go", "build", "-trimpath", "-o", dest, "./cmd/jk")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"GOFLAGS=",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build jk binary: %w\n%s", err, string(out))
	}
	return nil
}

func exportBareRepository(repoRoot string) (string, error) {
	baseDir := filepath.Join(repoRoot, ".tmp-jk-e2e")
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return "", fmt.Errorf("create bare base dir: %w", err)
	}

	tempDir, err := os.MkdirTemp(baseDir, "jk-bare-*")
	if err != nil {
		return "", fmt.Errorf("create bare repo temp dir: %w", err)
	}

	bareRepo := filepath.Join(tempDir, "jenkins-cli.git")
	if err := runCmd(repoRoot, "git", "init", "--bare", bareRepo); err != nil {
		return "", err
	}

	worktreeDir := filepath.Join(tempDir, "worktree")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		return "", fmt.Errorf("create worktree dir: %w", err)
	}

	if err := runCmd("", "git", "init", "--initial-branch=main", worktreeDir); err != nil {
		return "", fmt.Errorf("init worktree repo: %w", err)
	}

	if err := copyWorkingTree(repoRoot, worktreeDir); err != nil {
		return "", fmt.Errorf("copy working tree: %w", err)
	}

	if err := runCmd(worktreeDir, "git", "add", "."); err != nil {
		return "", fmt.Errorf("stage snapshot: %w", err)
	}

	if err := runCmd(worktreeDir, "git", "config", "user.email", "jk-e2e@example.com"); err != nil {
		return "", err
	}
	if err := runCmd(worktreeDir, "git", "config", "user.name", "jk e2e"); err != nil {
		return "", err
	}

	if err := runCmd(worktreeDir, "git", "commit", "-m", "jk e2e snapshot"); err != nil {
		return "", fmt.Errorf("commit snapshot: %w", err)
	}

	if err := runCmd(worktreeDir, "git", "remote", "add", "origin", bareRepo); err != nil {
		return "", err
	}

	if err := runCmd(worktreeDir, "git", "push", "--force", "origin", "main"); err != nil {
		return "", fmt.Errorf("push snapshot to bare repo: %w", err)
	}

	if err := runCmd("", "git", "--git-dir", bareRepo, "update-server-info"); err != nil {
		return "", fmt.Errorf("update bare repo info: %w", err)
	}

	return bareRepo, nil
}

func runCmd(dir string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, string(out))
	}
	return nil
}

func launchJenkins(ctx context.Context, repoRoot, bareRepoPath string) (tc.Container, string, error) {
	cascDir := filepath.Join(repoRoot, "hack", "e2e", "casc")

	env := map[string]string{
		"CASC_JENKINS_CONFIG": "/var/jenkins_home/casc/jenkins.yaml",
		"JAVA_OPTS":           "-Djenkins.install.runSetupWizard=false -Dhudson.plugins.git.GitSCM.ALLOW_LOCAL_CHECKOUT=true",
	}

	req := tc.ContainerRequest{
		FromDockerfile: tc.FromDockerfile{
			Context:    filepath.Join(repoRoot, "hack", "e2e"),
			Dockerfile: "controller.Dockerfile",
			BuildOptionsModifier: func(opts *build.ImageBuildOptions) {
				opts.NoCache = true
				opts.Platform = "linux/arm64"
			},
			BuildArgs: map[string]*string{
				"TARGETARCH":     ptr("arm64"),
				"TARGETPLATFORM": ptr("linux/arm64"),
			},
			BuildLogWriter: os.Stdout,
		},
		ImagePlatform: "linux/arm64",
		Env:           env,
		ExposedPorts:  []string{"8080/tcp"},
		WaitingFor:    wait.ForLog("Jenkins is fully up and running").WithStartupTimeout(5 * time.Minute),
		HostConfigModifier: func(hc *containertypes.HostConfig) {
			hc.Binds = append(hc.Binds,
				fmt.Sprintf("%s:/var/jenkins_home/casc:ro", cascDir),
				fmt.Sprintf("%s:/fixtures/repos/jenkins-cli.git:ro", bareRepoPath),
			)
		},
	}

	container, err := tc.GenericContainer(ctx, tc.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, "", fmt.Errorf("start jenkins container: %w", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, "", err
	}

	mappedPort, err := container.MappedPort(ctx, "8080/tcp")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, "", fmt.Errorf("resolve mapped port: %w", err)
	}

	baseURL := fmt.Sprintf("http://%s:%s", host, mappedPort.Port())
	return container, baseURL, nil
}

func (h *harness) waitForJob(ctx context.Context, jobPath string, timeout time.Duration) error {
	client := &http.Client{Timeout: 10 * time.Second}
	deadline := time.Now().Add(timeout)
	url := fmt.Sprintf("%s/job/%s/api/json", h.baseURL, strings.ReplaceAll(jobPath, "/", "/job/"))

	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		req.SetBasicAuth(h.adminUser, h.adminPassword)
		resp, err := client.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			return nil
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		time.Sleep(5 * time.Second)
	}

	return fmt.Errorf("job %s not ready within %s", jobPath, timeout)
}

func (h *harness) runCLI(ctx context.Context, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, h.cliPath, args...)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("XDG_CONFIG_HOME=%s", h.configDir),
		fmt.Sprintf("KEYRING_BACKEND=%s", "file"),
		fmt.Sprintf("KEYRING_FILE_DIR=%s", h.secretsDir),
		"JK_NO_COLOR=1",
	)
	cmd.Dir = h.repoRoot

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// fetchConsoleLog returns the Jenkins console output for a build.
func (h *harness) fetchConsoleLog(ctx context.Context, jobPath string, buildNumber int64) (string, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	part := strings.ReplaceAll(jobPath, "/", "/job/")
	url := fmt.Sprintf("%s/job/%s/%d/consoleText", h.baseURL, part, buildNumber)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}

	req.SetBasicAuth(h.adminUser, h.adminPassword)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("fetch console log: status %d: %s", resp.StatusCode, string(body))
	}

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, resp.Body); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func copyWorkingTree(src, dest string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if strings.Contains(rel, ".DS_Store") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(rel, ".git") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(rel, filepath.Join("hack", "e2e", ".tmp")) ||
			strings.HasPrefix(rel, filepath.Join("hack", "e2e", ".tmp-tests")) ||
			strings.HasPrefix(rel, ".tmp-jk-e2e") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		return copyFile(path, target, d.Type())
	})
}

func copyFile(src, dest string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		_ = in.Close()
	}()

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}

	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		_ = out.Close()
	}()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	if mode&0o111 != 0 {
		if err := os.Chmod(dest, 0o755); err != nil {
			return err
		}
	}
	return nil
}
