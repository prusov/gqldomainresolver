# Contributing

Thanks for your interest in `gqldomainresolver`. This document covers the dev loop.

## Build & test

```bash
go build ./...
go test -race ./...
```

`go test -race` is the canonical entry point — every test runs with the race
detector enabled.

## Integration testing against a real gqlgen project

Unit tests cover most code paths but the rendering pipeline is best exercised
end-to-end. The companion repo at <https://github.com/prusov/gqlgendomain>
("gqlgendomain") is a working sample that drives `gqldomainresolver` through
`go run ./cmd/gqlgen` and then runs an HTTP integration test
(`cmd/server/main_test.go`). When making non-trivial changes here:

1. Clone gqlgendomain as a sibling directory (`../gqlgendomain`).
2. In its `go.mod`, ensure `replace github.com/prusov/gqldomainresolver => ../gqldomainresolver` points at your working copy.
3. Run the full pipeline:
   ```bash
   (cd ../gqlgendomain && go run ./cmd/gqlgen && go build ./... && ./scripts/test.sh)
   ```

That pipeline catches resolver-delegation regressions that pure unit tests
cannot.

## Coding conventions

- Use `new()` for pointer values; do not introduce `&local` indirection.
- Comments only when the **why** is non-obvious — names should carry the **what**.
- Empty line before `return` (except single-line functions).
- All comments in English.

## Pull requests

- Keep PRs focused; one logical change per PR.
- Update `CHANGELOG.md` under `## [Unreleased]`.
- Make sure `go test -race ./...` passes locally.
