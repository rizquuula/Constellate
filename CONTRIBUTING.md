# Contributing to Constellate

Thanks for your interest in improving Constellate! Issues and pull requests are welcome. This guide
covers how to get set up, the conventions the project follows, and what a good contribution looks
like.

By participating you agree to abide by our [Code of Conduct](CODE_OF_CONDUCT.md).

---

## Before you start

- **[`DESIGN.md`](DESIGN.md) is canonical.** Read the relevant sections before any non-trivial change —
  it documents the architecture, the wire protocol, the data model, and the decisions behind them. PRs
  that fight the design will be hard to merge; if you think the design is wrong, open an issue to
  discuss it first.
- For anything larger than a bug fix or doc tweak, **open an issue first** so we can agree on the
  approach before you invest the time.

## Prerequisites

- **Go 1.25+** — if your system Go is older, `GOTOOLCHAIN=auto` (already set by the `Makefile`) fetches
  the right toolchain automatically.
- **Node 18+ / npm** — to build the web app (`make web`, run automatically by `make build`).
- **Docker + Compose v2** — only needed for the Dockerized topology tests (`make test-docker`).

## Getting set up

```bash
git clone https://github.com/rizquuula/Constellate.git
cd Constellate
make build      # builds both binaries (runs `make web` first) into ./bin
make test       # unit + integration + in-process E2E
```

See [`docs/usage.binary.md`](docs/usage.binary.md) and [`docs/usage.docker.md`](docs/usage.docker.md)
for running the hub and an agent locally.

## Development workflow

1. **Fork** the repo and create a topic branch off `main` (`git switch -c fix/short-description`).
2. Make your change, keeping it focused — one logical change per PR.
3. **Run the gates locally** (see below) until they pass.
4. Commit using [Conventional Commits](#commit-messages), then open a PR against `main`.

### Quality gates

These must pass before a PR is merged. The cheap tiers run in CI on every push/PR; please run them
locally first:

```bash
make lint        # golangci-lint (v2 config)
make test        # unit + integration + in-process E2E
```

For changes that touch the transport, the agent, PTY handling, or the browser flow, also run the
heavier tiers:

```bash
make test-e2e    # Playwright: a real browser drives a shell on an agent
make test-docker # hub + agent containers on a Docker network (dial-home across real boundaries)
```

New behavior should come with tests. The project follows a strict test pyramid (see `DESIGN.md` §16):

- **Domain** — pure unit tests, no mocks.
- **Use cases** — hand-written in-memory fakes (the `secondary/memory` stores double as fakes).
- **Adapters** — tested against the real thing (real SQLite file, real PTY, WS loopback) — never mock
  `*sql.DB` or sockets.

## Architecture & code rules

Constellate is **two bounded contexts in one Go module**, each its own hexagon, plus shared
infrastructure. A few rules are enforced by review (and some by lint):

- **Keep it pure Go.** `CGO_ENABLED=0` everywhere — the binaries are static and run on
  distroless/scratch. **Do not add cgo dependencies.**
- **Respect the boundaries.** `internal/hub` and `internal/agent` must **not** import each other; they
  share only `internal/transport` (the wire protocol) and `internal/platform` (log/id/config/version).
- **Respect the layering.** `domain/` is pure stdlib; `app/<usecase>` is glue and declares the SPI it
  needs in its own `ports.go` (consumer-side, no central `port/` package); adapters split into
  `primary/` (driving) and `secondary/` (driven) and translate DTOs at the boundary. `cmd/*/main.go`
  is the only wiring.
- **Wire-protocol changes** bump `transport.ProtocolVersion` and stay backward compatible within the
  supported window where possible — see `DESIGN.md` §6 and the versioning notes in §13.
- **Match the surrounding style** — naming, comment density, and idiom of the code you're editing.
- Run `gofmt` (or `go fmt ./...`); `make lint` will catch the rest.

## Commit messages

Use [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<optional scope>): <short summary>
```

Common types in this repo: `feat`, `fix`, `docs`, `refactor`, `test`, `build`, `style`, `chore`.
Examples from the history:

```
feat(web): Alt/Cmd+1/2/3 shortcuts to switch views
fix(sidebar): reveal project header actions (delete was invisible)
docs: split usage into binary and docker guides
```

Keep the summary in the imperative mood and under ~72 characters; put the *why* in the body when it
isn't obvious.

## Pull requests

- Describe **what** changed and **why**; link any related issue.
- Note which test tiers you ran.
- Keep PRs small and reviewable; split unrelated changes.
- Update docs (`README.md`, `DESIGN.md`, `docs/`) when behavior or interfaces change.

## Reporting bugs & security issues

- **Bugs:** open a GitHub issue with steps to reproduce, what you expected, what happened, and your OS
  / Go version.
- **Security vulnerabilities:** please do **not** open a public issue. Email the maintainer at
  **razifrizqullah@gmail.com** with details, and allow time for a fix before any public disclosure.

## License

By contributing, you agree that your contributions will be licensed under the
[MIT License](LICENSE) that covers the project.
