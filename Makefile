GO ?= go
BIN_DIR ?= bin
CMD := ./cmd/jk
SOURCES := $(shell find cmd internal -name '*.go')

VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X github.com/your-org/jenkins-cli/internal/build.Version=$(VERSION) \
	-X github.com/your-org/jenkins-cli/internal/build.Commit=$(COMMIT) \
	-X github.com/your-org/jenkins-cli/internal/build.Date=$(BUILD_DATE)

.PHONY: build
build: $(BIN_DIR)/jk

$(BIN_DIR)/jk: $(SOURCES) go.mod go.sum
	@mkdir -p $(BIN_DIR)
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/jk $(CMD)

.PHONY: tidy
tidy:
	$(GO) mod tidy

.PHONY: test
test:
	$(GO) test ./...

.PHONY: lint
lint:
	golangci-lint run ./...

.PHONY: e2e
e2e:
	$(GO) test ./test/e2e -count=1

.PHONY: e2e-up
e2e-up:
	hack/e2e/up.sh up

.PHONY: e2e-down
e2e-down:
	hack/e2e/up.sh down

.PHONY: fmt
fmt:
	$(GO) fmt ./...

.PHONY: clean
clean:
	rm -rf $(BIN_DIR)
