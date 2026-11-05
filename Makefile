SHELL := /usr/bin/env bash
NPM ?= npm

.PHONY: help install test test-login test-backend test-frontend test-admin test-e2e typecheck lint fmt build build-backend build-frontend build-admin up down logs ps dev clean

help: ## Show the available targets
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

install: ## Install every dependency (login page, backend, purchase app, admin panel, linters)
	$(NPM) install
	$(MAKE) -C backend deps
	$(MAKE) -C backend tools
	cd frontend && $(NPM) ci
	cd admin && $(NPM) ci

# The whole suite: the Login Page regression tests, the backend integration
# tests against a real Postgres and the purchase app's Vitest suite, plus the
# Admin Panel's own suite (its own artefact, its own tests — ADR 0028).
test: test-login test-backend test-frontend test-admin

test-login: ## Login Page unit tests (Vitest + jsdom)
	$(NPM) test

test-backend: ## Backend unit + integration tests (real Postgres and Redis)
	$(MAKE) -C backend test

test-frontend: ## Purchase app unit/component/contract tests
	cd frontend && $(NPM) test

test-admin: ## Admin Panel unit/component/contract tests
	cd admin && $(NPM) test

# The full journey needs the real stack (Postgres, MySQL FreeRADIUS, Redis, API,
# worker, app, admin panel) plus a stub for the vendor, so it is not part of
# `make test`: `make e2e` brings the stack up, runs both browser projects (the
# buyer's journey and the panel's), and tears it down again.
test-e2e: ## Full-journey smoke against the compose stack (needs Docker)
	docker compose -f docker-compose.yml -f docker-compose.e2e.yml up -d --build
	$(NPM) run test:e2e
	docker compose -f docker-compose.yml -f docker-compose.e2e.yml down -v

typecheck: ## Typecheck every part of the monorepo
	$(NPM) run typecheck
	$(MAKE) -C backend typecheck
	cd frontend && $(NPM) run typecheck
	cd admin && $(NPM) run typecheck

lint: ## Lint every part of the monorepo
	$(NPM) run typecheck
	$(MAKE) -C backend lint
	cd frontend && $(NPM) run lint
	cd admin && $(NPM) run lint

fmt: ## Format Go and both frontends
	$(MAKE) -C backend fmt
	cd frontend && npx prettier --write .
	cd admin && npx prettier --write .

build: build-backend build-frontend build-admin

build-backend:
	$(MAKE) -C backend build

build-frontend:
	cd frontend && $(NPM) run build

build-admin:
	cd admin && $(NPM) run build

up: ## Start the deployed stack (one origin: app + /api + /admin)
	docker compose up -d --build

down: ## Stop the stack
	docker compose down

logs: ## Follow stack logs
	docker compose logs -f --tail=100

ps: ## Show stack status
	docker compose ps

dev: ## Run the stack in the foreground
	docker compose up --build

clean: ## Remove build output and test artifacts
	$(MAKE) -C backend clean
	rm -rf frontend/build frontend/.svelte-kit admin/build admin/.svelte-kit
