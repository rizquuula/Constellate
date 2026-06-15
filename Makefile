GOTOOLCHAIN ?= auto
export GOTOOLCHAIN

HUB_VERSION   := $(shell cat cmd/hub/VERSION)
AGENT_VERSION := $(shell cat cmd/agent/VERSION)
COMMIT        := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILDTIME     := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

VERSION_PKG := github.com/rizquuula/Constellate/internal/platform/version
LDFLAGS_HUB := -ldflags "-X $(VERSION_PKG).Version=$(HUB_VERSION) \
	-X $(VERSION_PKG).Commit=$(COMMIT) \
	-X $(VERSION_PKG).BuildTime=$(BUILDTIME)"
LDFLAGS_AGENT := -ldflags "-X $(VERSION_PKG).Version=$(AGENT_VERSION) \
	-X $(VERSION_PKG).Commit=$(COMMIT) \
	-X $(VERSION_PKG).BuildTime=$(BUILDTIME)"

DEV_COMPOSE  := docker compose -f docker-compose.dev.yaml
PROD_COMPOSE := docker compose -f deploy/compose.yaml

.DEFAULT_GOAL := help

.PHONY: help build build-hub build-agent web image-hub \
	test test-web test-e2e test-docker lint \
	ddocker-up ddocker-totp ddocker-logs ddocker-down ddocker-reset \
	docker-up docker-down docker-logs

##@ General

help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nConstellate — hub $(HUB_VERSION) · agent $(AGENT_VERSION)\n\nUsage:\n  make \033[36m<target>\033[0m\n"} \
		/^[a-zA-Z0-9_-]+:.*?##/ { printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2 } \
		/^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) }' $(MAKEFILE_LIST)
	@echo ""

##@ Build

build: web build-hub build-agent ## Build everything: web assets + both binaries

build-hub: ## Build the hub binary (bin/constellate-hub)
	go build $(LDFLAGS_HUB) -o bin/constellate-hub ./cmd/hub

build-agent: ## Build the agent binary (bin/constellate-agent)
	go build $(LDFLAGS_AGENT) -o bin/constellate-agent ./cmd/agent

web: ## Build the embedded frontend into web/dist
	@if [ ! -f web/package-lock.json ]; then \
		cd web && npm install; \
	else \
		cd web && npm ci; \
	fi
	cd web && npm run build
	@touch web/dist/.gitkeep   # vite emptyOutDir wipes it; keep the embed placeholder tracked

image-hub: ## Build the hub Docker image (tagged with the hub version)
	docker build -f deploy/hub.Dockerfile -t constellate-hub:$(HUB_VERSION) .

##@ Test & lint

test: ## Run Go unit + integration + in-proc E2E tests
	go test ./...

test-web: ## Run frontend unit tests
	cd web && npm run test:run

test-e2e: ## Run single-machine Playwright E2E suite
	./test/e2e/run.sh

test-docker: ## Run dockerized E2E (hub + 2 agent containers)
	./test/docker/run.sh

lint: ## Run golangci-lint (v2 config)
	golangci-lint run

##@ Dev stack (local docker: hub + 2 agents)

ddocker-up: ## Build & start the local dev stack, bootstrap operator + enroll agents
	./deploy/dev-up.sh

ddocker-totp: ## Print a current TOTP login code for the dev operator
	./deploy/dev-totp.sh

ddocker-logs: ## Follow the dev hub logs
	$(DEV_COMPOSE) logs -f hub

ddocker-down: ## Stop the dev stack (keep volumes/data)
	$(DEV_COMPOSE) down

ddocker-reset: ## Stop the dev stack and wipe volumes (fresh operator next up)
	$(DEV_COMPOSE) down -v

##@ Prod stack (deploy/compose.yaml + Caddy TLS)

docker-up: ## Start the production stack detached (needs CONSTELLATE_DOMAIN + DNS)
	$(PROD_COMPOSE) up -d

docker-down: ## Stop the production stack
	$(PROD_COMPOSE) down

docker-logs: ## Follow the production stack logs
	$(PROD_COMPOSE) logs -f
