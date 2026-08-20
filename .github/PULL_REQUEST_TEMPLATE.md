<!-- Thanks for contributing to mindwire! -->

## What & why

<!-- What does this change and why? Link any related issue: "Closes #123". -->

## Component

- [ ] daemon (Go)
- [ ] `mindwire` (TypeScript)
- [ ] docs
- [ ] repo / CI

## Type of change

- [ ] Bug fix
- [ ] New feature
- [ ] New agent adapter
- [ ] Breaking change
- [ ] Docs only

## Checklist

- [ ] I read [CONTRIBUTING.md](../blob/main/CONTRIBUTING.md).
- [ ] Daemon: `go build ./...`, `go vet ./...`, and `go test ./...` pass.
- [ ] SDK: `bun --filter='mindwire' run typecheck`, `run build`, and `run test` pass.
- [ ] I added/updated tests where it made sense.
- [ ] I updated docs (READMEs / API tables) for any behavior change.
- [ ] New adapter: registered via blank import in `cmd/daemon/main.go`; core stays agent-agnostic.
