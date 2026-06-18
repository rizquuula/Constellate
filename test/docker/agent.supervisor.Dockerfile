FROM golang:1.25 AS build
ENV GOPROXY=https://goproxy.cn,direct GOTOOLCHAIN=auto CGO_ENABLED=0 GOOS=linux
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -trimpath -ldflags "-s -w" -o /out/constellate-agent ./cmd/agent

FROM alpine:3
RUN apk add --no-cache ca-certificates bash
COPY --from=build /out/constellate-agent /usr/local/bin/constellate-agent
# Supervisor entrypoint: shell stays PID 1, connect runs as a child.
# Killing connect does NOT kill the container; the session-host (setsid-spawned)
# stays alive — this is required for the connect-restart survival test.
COPY deploy/agent-supervisor-entrypoint.sh /usr/local/bin/agent-supervisor-entrypoint.sh
RUN chmod +x /usr/local/bin/agent-supervisor-entrypoint.sh
ENTRYPOINT ["/bin/sh", "/usr/local/bin/agent-supervisor-entrypoint.sh"]
