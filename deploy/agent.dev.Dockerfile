# deploy/agent.dev.Dockerfile — dev/demo agent image used by deploy/compose.dev.yaml.
# Debian-based, with the `opencode` CLI installed, so the shells you open in the
# browser land in a Debian environment with opencode available on PATH.
# (Automated topology tests use the smaller test/docker/agent.test.Dockerfile.)

FROM golang:1.25 AS build
ENV GOPROXY=https://goproxy.cn,direct GOTOOLCHAIN=auto CGO_ENABLED=0 GOOS=linux
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -trimpath -ldflags "-s -w" -o /out/constellate-agent ./cmd/agent

FROM debian:bookworm-slim
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      ca-certificates curl bash git unzip less nano \
 && rm -rf /var/lib/apt/lists/*

# Install opencode (https://opencode.ai) and put it on PATH for interactive shells.
# Pin the version (see deploy/agent.Dockerfile): VERSION set => download the release
# directly and skip the rate-limited GitHub "latest" API lookup.
ARG OPENCODE_VERSION=1.17.7
RUN curl -fsSL https://opencode.ai/install | VERSION="${OPENCODE_VERSION}" bash \
 && ln -sf /root/.opencode/bin/opencode /usr/local/bin/opencode \
 && opencode --version

# Spawned shells inherit this env (the agent's PTY factory copies os.Environ()).
ENV PATH="/root/.opencode/bin:${PATH}" \
    SHELL=/bin/bash

COPY --from=build /out/constellate-agent /usr/local/bin/constellate-agent
# Entrypoint wrapper: enrolls the agent on first start, then runs connect.
# Used by deploy/compose.dev.yaml (overrides CMD). Running the image directly
# without the entrypoint override still runs `connect` as before.
COPY deploy/agent-entrypoint.sh /usr/local/bin/agent-entrypoint.sh
ENTRYPOINT ["/usr/local/bin/constellate-agent"]
CMD ["connect"]
