GOCMD ?= go
BIN_DIR ?= bin
MODULE := github.com/novriyantoAli/mikrotik-login-page/backend
GOIMPORTS ?= goimports
GOFUMPT ?= gofumpt
GOLANGCI ?= golangci-lint
SWAG ?= swag

.PHONY: deps tools build build-api build-worker build-migrate run-api run-worker run-migrate migrate test test-unit test-integration test-coverage lint format format-strict swagger-gen typecheck fmt tidy clean

deps:
	$(GOCMD) mod download
	$(GOCMD) mod tidy

# Install the tools the Definition of Done needs: formatting, linting and
# Swagger generation. golangci-lint v2.15+ can read this module's go directive.
tools:
	$(GOCMD) install golang.org/x/tools/cmd/goimports@latest
	$(GOCMD) install mvdan.cc/gofumpt@latest
	$(GOCMD) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	$(GOCMD) install github.com/swaggo/swag/cmd/swag@latest

build: build-api build-worker build-migrate

build-api:
	@mkdir -p $(BIN_DIR)
	$(GOCMD) build -trimpath -o $(BIN_DIR)/api ./cmd/api

build-worker:
	@mkdir -p $(BIN_DIR)
	$(GOCMD) build -trimpath -o $(BIN_DIR)/worker ./cmd/worker

build-migrate:
	@mkdir -p $(BIN_DIR)
	$(GOCMD) build -trimpath -o $(BIN_DIR)/migrate ./cmd/migrate

run-api: ## CONFIG_FILE=config.yaml by default; env overrides still apply
	$(GOCMD) run ./cmd/api

run-worker:
	$(GOCMD) run ./cmd/worker

# Applies the schema and the idempotent Package seed, then exits (ADR 0012).
# Deployment runs the same command as a one-shot container before the API.
run-migrate:
	$(GOCMD) run ./cmd/migrate

# Kept for muscle memory: `migrate` is the old name of `run-migrate`.
migrate: run-migrate

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

# The Definition of Done target: gofmt check, vet, and every linter the backend
# standard requires (.golangci.yml, which also runs gofmt/goimports as
# formatters). Run `make tools` once to install golangci-lint.
lint:
	@unformatted="$$(gofmt -l . | grep -v '^$(BIN_DIR)/' || true)"; \
	if [ -n "$$unformatted" ]; then echo "gofmt required for:"; echo "$$unformatted"; exit 1; fi
	$(GOCMD) vet ./...
	@command -v $(GOLANGCI) >/dev/null 2>&1 || { \
		echo "$(GOLANGCI) not found: run 'make tools' (needs golangci-lint v2.15+ for Go 1.26)"; \
		exit 1; \
	}
	$(GOLANGCI) run

# gofmt + goimports with the module's local import prefix.
format:
	$(GOCMD) fmt ./...
	@command -v $(GOIMPORTS) >/dev/null 2>&1 || { echo "$(GOIMPORTS) not found: run 'make tools'"; exit 1; }
	$(GOIMPORTS) -local $(MODULE) -w .

# format plus gofumpt, for the strictest pass.
format-strict: format
	@command -v $(GOFUMPT) >/dev/null 2>&1 || { echo "$(GOFUMPT) not found: run 'make tools'"; exit 1; }
	$(GOFUMPT) -w .

# Kept for muscle memory: `fmt` is the old name of `format`.
fmt: format

typecheck:
	$(GOCMD) build ./...
	$(GOCMD) vet ./...

swagger-gen:
	@command -v $(SWAG) >/dev/null 2>&1 || { echo "$(SWAG) not found: run 'make tools'"; exit 1; }
	# Output lives outside the Go module so the generated package never breaks
	# `go vet`/lint; it is deployment documentation, not code.
	$(SWAG) init -g cmd/api/main.go -o ../docs/api --parseDependency --parseInternal

tidy:
	$(GOCMD) mod tidy

clean:
	rm -rf $(BIN_DIR) coverage.out
