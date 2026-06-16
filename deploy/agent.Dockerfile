# deploy/agent.Dockerfile — production agent image published to GHCR.
# Debian 13 (trixie) based, so browser-opened shells land in a familiar Debian
# environment with the usual interactive tooling on PATH.
# (The dev/demo image is deploy/agent.dev.Dockerfile; automated topology tests
# use test/docker/agent.test.Dockerfile.)

# Build on the native BUILDPLATFORM and cross-compile to TARGETARCH — with
# CGO_ENABLED=0 this is a free, fast cross-build (no QEMU-emulated Go compile).
# The runtime stage below still runs apt under emulation for arm64, which is
# why the workflow sets up QEMU.
FROM --platform=$BUILDPLATFORM golang:1.25 AS build
ENV GOPROXY=https://goproxy.cn,direct GOTOOLCHAIN=auto CGO_ENABLED=0 GOOS=linux
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=0.1.0
ARG COMMIT=docker
ARG TARGETARCH
RUN GOARCH=${TARGETARCH} go build -trimpath -ldflags "-s -w -X github.com/rizquuula/Constellate/internal/platform/version.Version=${VERSION} -X github.com/rizquuula/Constellate/internal/platform/version.Commit=${COMMIT}" -o /out/constellate-agent ./cmd/agent

FROM debian:trixie-slim
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      ca-certificates curl bash git less nano \
 && rm -rf /var/lib/apt/lists/*

# Spawned shells inherit this env (the agent's PTY factory copies os.Environ()).
ENV SHELL=/bin/bash

COPY --from=build /out/constellate-agent /usr/local/bin/constellate-agent
# Entrypoint wrapper: enrolls the agent on first start, then runs connect.
# Used by docker-compose (overrides CMD). Running the image directly without the
# entrypoint override still runs `connect` as before.
COPY deploy/agent-entrypoint.sh /usr/local/bin/agent-entrypoint.sh
ENTRYPOINT ["/usr/local/bin/constellate-agent"]
CMD ["connect"]
