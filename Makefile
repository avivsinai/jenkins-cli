GO ?= go
GOFMT ?= gofmt
BIN_DIR := bin
CMD := ./cmd/jk

export PATH := /opt/homebrew/opt/go@1.22/bin:$(PATH)

.PHONY: build
build: $(BIN_DIR)/jk

$(BIN_DIR)/jk: $(shell find cmd pkg -name '*.go') go.mod go.sum
	@mkdir -p $(BIN_DIR)
	$(GO) build -trimpath -ldflags "-s -w -X github.com/your-org/jenkins-cli/pkg/build.Version=$$(git describe --tags --always 2>/dev/null || echo dev) -X github.com/your-org/jenkins-cli/pkg/build.Commit=$$(git rev-parse HEAD 2>/dev/null || echo unknown) -X github.com/your-org/jenkins-cli/pkg/build.Date=$$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o $(BIN_DIR)/jk $(CMD)

.PHONY: tidy
 tidy:
	$(GO) mod tidy

.PHONY: test
 test:
	$(GO) test ./...

.PHONY: fmt
 fmt:
	$(GOFMT) -w $(shell find cmd pkg -name '*.go')

.PHONY: clean
 clean:
	rm -rf $(BIN_DIR)
