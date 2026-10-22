SHELL := /usr/bin/env bash
NPM ?= npm

.PHONY: help install test test-login test-backend test-frontend typecheck lint fmt build build-backend build-frontend up down logs ps dev clean

help: ## Show the available targets
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

install: ## Install every dependency (login page, backend, purchase app)
	$(NPM) install
	$(MAKE) -C backend deps
	cd frontend && $(NPM) ci

# The whole suite: the Login Page regression tests, the backend integration
# tests against a real Postgres and the purchase app's Vitest suite.
test: test-login test-backend test-frontend

test-login: ## Login Page unit tests (Vitest + jsdom)
	$(NPM) test

test-backend: ## Backend unit + integration tests (real Postgres and Redis)
	$(MAKE) -C backend test

test-frontend: ## Purchase app unit/component/contract tests
	cd frontend && $(NPM) test

typecheck: ## Typecheck every part of the monorepo
	$(NPM) run typecheck
	$(MAKE) -C backend typecheck
	cd frontend && $(NPM) run typecheck

lint: ## Lint every part of the monorepo
	$(NPM) run typecheck
	$(MAKE) -C backend lint
	cd frontend && $(NPM) run lint

fmt: ## Format Go and the purchase app
	$(MAKE) -C backend fmt
	cd frontend && npx prettier --write .

build: build-backend build-frontend

build-backend:
	$(MAKE) -C backend build

build-frontend:
	cd frontend && $(NPM) run build

up: ## Start the deployed stack (one origin: app + /api)
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
	rm -rf frontend/build frontend/.svelte-kit
