GOCMD ?= go
BIN_DIR ?= bin
MODULE := github.com/novriyantoAli/mikrotik-login-page/backend

.PHONY: deps build build-api build-worker run-api run-worker test test-unit test-integration test-coverage lint lint-golangci swagger-gen typecheck fmt tidy clean

deps:
	$(GOCMD) mod download
	$(GOCMD) mod tidy

build: build-api build-worker

build-api:
	@mkdir -p $(BIN_DIR)
	$(GOCMD) build -trimpath -o $(BIN_DIR)/api ./cmd/api

build-worker:
	@mkdir -p $(BIN_DIR)
	$(GOCMD) build -trimpath -o $(BIN_DIR)/worker ./cmd/worker

run-api: ## CONFIG_FILE=config.yaml by default; env overrides still apply
	$(GOCMD) run ./cmd/api

run-worker:
	$(GOCMD) run ./cmd/worker

# Unit tests never need a database.
test-unit:
	$(GOCMD) test -race -timeout 5m ./internal/...

# Integration tests use a real Postgres (embedded when TEST_POSTGRES_DSN is
# unset) and a real Redis (TEST_REDIS_ADDR or a local redis-server).
test-integration:
	$(GOCMD) test -race -timeout 20m ./test/integration/...

test: test-unit test-integration

test-coverage:
	$(GOCMD) test -race -covermode=atomic -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -func=coverage.out

lint:
	@unformatted="$$(gofmt -l . | grep -v '^$(BIN_DIR)/' || true)"; \
	if [ -n "$$unformatted" ]; then echo "gofmt required for:"; echo "$$unformatted"; exit 1; fi
	$(GOCMD) vet ./...

# golangci-lint is optional tooling and must be built with a Go version that can
# read this module's go directive. Run it explicitly when you have one.
swagger-gen:
	@command -v swag >/dev/null 2>&1 || { echo "install swag: go install github.com/swaggo/swag/cmd/swag@latest"; exit 1; }
	swag init -g cmd/api/main.go -o docs --parseDependency --parseInternal

lint-golangci:
	@if command -v golangci-lint >/dev/null 2>&1; then golangci-lint run; else echo "golangci-lint not installed"; exit 1; fi

typecheck:
	$(GOCMD) build ./...
	$(GOCMD) vet ./...

fmt:
	$(GOCMD) fmt ./...
	@if command -v gofumpt >/dev/null 2>&1; then gofumpt -w .; fi

tidy:
	$(GOCMD) mod tidy

clean:
	rm -rf $(BIN_DIR) coverage.out
