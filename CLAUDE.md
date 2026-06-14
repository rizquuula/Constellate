# CLAUDE.md

Self-hosted control plane: a hub (public VPS) brokers browser ↔ per-machine agents that **dial home**
and own the PTYs. **[`DESIGN.md`](DESIGN.md) is canonical** — read it before non-trivial work.

## Must know
- **Go 1.25**, but local toolchain may be older → always `export GOTOOLCHAIN=auto` before `go` commands
  (the Makefile/CI/Dockerfiles already do).
- **`CGO_ENABLED=0`** everywhere — deps are pure Go (incl. `modernc.org/sqlite`), so binaries are static
  and run on distroless/scratch. Keep it that way; don't add cgo deps.
- Two hexagons in one module — `internal/hub` and `internal/agent` — sharing only `internal/transport`
  (wire protocol) and `internal/platform` (log/id/config/version). **Neither context imports the other.**
- Layering: `domain/` is pure stdlib; `app/<usecase>` is glue and declares the SPI it needs in its own
  `ports.go`; adapters split `primary/` (driving) and `secondary/` (driven) and translate DTOs at the
  boundary. `cmd/*/main.go` is the only wiring.
- M0 is **localhost/private net, plain `ws://`, shared dev token** — no TLS/auth until M5.

## Commands
- `make build` · `make test` (unit + integration + in-proc E2E) · `make test-docker` (hub + 2 agent
  containers) · `make lint` (golangci-lint **v2** config).
- Binaries: `constellate-hub serve|migrate|version` · `constellate-agent connect|status|version`.

## Status
M0 done (scaffold + dial-home: online→offline→online proven in-proc and across containers). Next: M1
(first live terminal). Milestone roadmap in `DESIGN.md` §18.
