GO ?= go
BIN_DIR ?= bin
CMD := ./cmd/jk
SOURCES := $(shell find cmd internal pkg -name '*.go')
MACOS_CODESIGN_ID ?= io.github.avivsinai.jk

VERSION ?= $(shell \
	if git describe --tags --exact-match >/dev/null 2>&1; then \
		git describe --tags --exact-match; \
	else \
		short=$$(git rev-parse --short HEAD 2>/dev/null || echo "unknown"); \
		if git diff-index --quiet HEAD 2>/dev/null; then \
			echo "dev-$$short"; \
		else \
			echo "dev-$$short-dirty"; \
		fi; \
	fi \
)
COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X github.com/avivsinai/jenkins-cli/internal/build.versionFromLdflags=$(VERSION) \
	-X github.com/avivsinai/jenkins-cli/internal/build.commitFromLdflags=$(COMMIT) \
	-X github.com/avivsinai/jenkins-cli/internal/build.dateFromLdflags=$(BUILD_DATE)

.PHONY: build fmt lint test tidy e2e e2e-up e2e-down snapshot clean check-skills check-generated-skill check-repo-hygiene generate-skill release security gitleaks-detect gitleaks-protect pre-commit-install pre-commit-check pre-commit-update
build: $(BIN_DIR)/jk

# Skill integrity: skills/ is canonical, .claude/skills/ and .agents/skills/ are symlinks
check-skills:
	@echo "Checking skill symlinks..."
	@test -L .claude/skills/jk || (echo "❌ .claude/skills/jk is not a symlink" && exit 1)
	@test -L .agents/skills/jk || (echo "❌ .agents/skills/jk is not a symlink" && exit 1)
	@test "$$(readlink .claude/skills/jk)" = "../../skills/jk" || (echo "❌ .claude/skills/jk target is not ../../skills/jk" && exit 1)
	@test "$$(readlink .agents/skills/jk)" = "../../skills/jk" || (echo "❌ .agents/skills/jk target is not ../../skills/jk" && exit 1)
	@diff -rq skills/jk .claude/skills/jk || (echo "❌ .claude/skills/jk content mismatch" && exit 1)
	@echo "✓ Skill symlinks valid"

$(BIN_DIR)/jk: $(SOURCES) go.mod go.sum
	@mkdir -p $(BIN_DIR)
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/jk $(CMD)
	./scripts/codesign-macos.sh "$(BIN_DIR)/jk" "$(MACOS_CODESIGN_ID)"

tidy:
	$(GO) mod tidy

test:
	$(GO) test ./...

lint:
	golangci-lint run ./...

e2e:
	$(GO) test ./test/e2e -count=1

e2e-up:
	hack/e2e/up.sh up

e2e-down:
	hack/e2e/up.sh down

fmt:
	$(GO) fmt ./...

clean:
	rm -rf $(BIN_DIR) dist/

generate-skill:
	$(GO) run ./cmd/docgen -o skills/jk/rules

check-generated-skill:
	@GO="$(GO)" ./scripts/check-generated-skill.sh

check-repo-hygiene:
	@./scripts/check-repo-hygiene.sh

snapshot:
	@command -v goreleaser >/dev/null 2>&1 || { echo "goreleaser not installed. Run: brew install goreleaser"; exit 1; }
	goreleaser release --snapshot --clean --skip=publish

# Security and OSS tooling
security: gitleaks-detect pre-commit-check

gitleaks-detect:
	@command -v gitleaks >/dev/null 2>&1 || { echo "gitleaks not installed. Run: brew install gitleaks"; exit 1; }
	gitleaks detect --verbose --config .gitleaks.toml

gitleaks-protect:
	@command -v gitleaks >/dev/null 2>&1 || { echo "gitleaks not installed. Run: brew install gitleaks"; exit 1; }
	gitleaks protect --verbose --staged --config .gitleaks.toml

pre-commit-install:
	@command -v pre-commit >/dev/null 2>&1 || { echo "pre-commit not installed. Run: pip install pre-commit"; exit 1; }
	pre-commit install
	@echo "✓ Pre-commit hooks installed"

pre-commit-check:
	@command -v pre-commit >/dev/null 2>&1 || { echo "pre-commit not installed. Run: pip install pre-commit"; exit 1; }
	pre-commit run --all-files

pre-commit-update:
	@command -v pre-commit >/dev/null 2>&1 || { echo "pre-commit not installed. Run: pip install pre-commit"; exit 1; }
	pre-commit autoupdate

release:
	@test -n "$(RELEASE_VERSION)" || (echo "usage: make release RELEASE_VERSION=X.Y.Z [RELEASE_DATE=YYYY-MM-DD] [RELEASE_SKIP_VERIFY=1] [RELEASE_ALLOW_EMPTY=1] [RELEASE_NO_AUTO_MERGE=1]" && exit 1)
	./scripts/release.sh "$(RELEASE_VERSION)" $(if $(RELEASE_DATE),--date $(RELEASE_DATE),) $(if $(RELEASE_SKIP_VERIFY),--skip-verify,) $(if $(RELEASE_ALLOW_EMPTY),--allow-empty,) $(if $(RELEASE_NO_AUTO_MERGE),--no-auto-merge,)
