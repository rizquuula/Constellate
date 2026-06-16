# Frontend output is arch-independent — build it once on the native platform
# rather than emulating node under QEMU for each target arch.
FROM --platform=$BUILDPLATFORM node:22 AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# Build on the native BUILDPLATFORM and cross-compile to TARGETARCH — with
# CGO_ENABLED=0 this is a free, fast cross-build (no QEMU-emulated Go compile).
FROM --platform=$BUILDPLATFORM golang:1.25 AS build
ENV GOPROXY=https://goproxy.cn,direct GOTOOLCHAIN=auto CGO_ENABLED=0 GOOS=linux
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /web/dist ./web/dist
ARG VERSION=0.1.0
ARG COMMIT=docker
ARG TARGETARCH
RUN GOARCH=${TARGETARCH} go build -trimpath -ldflags "-s -w -X github.com/rizquuula/Constellate/internal/platform/version.Version=${VERSION} -X github.com/rizquuula/Constellate/internal/platform/version.Commit=${COMMIT}" -o /out/constellate-hub ./cmd/hub

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/constellate-hub /usr/local/bin/constellate-hub
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/constellate-hub"]
CMD ["serve"]
