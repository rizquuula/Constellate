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
ENTRYPOINT ["/usr/local/bin/constellate-agent"]
CMD ["connect"]
