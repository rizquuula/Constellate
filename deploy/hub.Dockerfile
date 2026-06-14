FROM golang:1.25 AS build
ENV GOPROXY=https://goproxy.cn,direct GOTOOLCHAIN=auto CGO_ENABLED=0 GOOS=linux
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=0.1.0
ARG COMMIT=docker
RUN go build -trimpath -ldflags "-s -w -X github.com/rizquuula/Constellate/internal/platform/version.Version=${VERSION} -X github.com/rizquuula/Constellate/internal/platform/version.Commit=${COMMIT}" -o /out/constellate-hub ./cmd/hub

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/constellate-hub /usr/local/bin/constellate-hub
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/constellate-hub"]
CMD ["serve"]
