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

.PHONY: build build-hub build-agent test test-docker lint image-hub

build: build-hub build-agent

build-hub:
	go build $(LDFLAGS_HUB) -o bin/constellate-hub ./cmd/hub

build-agent:
	go build $(LDFLAGS_AGENT) -o bin/constellate-agent ./cmd/agent

test:
	go test ./...

test-docker:
	./test/docker/run.sh

lint:
	golangci-lint run

image-hub:
	docker build -f deploy/hub.Dockerfile -t constellate-hub:$(HUB_VERSION) .
