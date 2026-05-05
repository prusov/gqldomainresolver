# gqldomainresolver

[![CI](https://github.com/prusov/gqldomainresolver/actions/workflows/ci.yml/badge.svg)](https://github.com/prusov/gqldomainresolver/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/prusov/gqldomainresolver.svg)](https://pkg.go.dev/github.com/prusov/gqldomainresolver)

A gqlgen plugin that splits resolvers into per-domain Go packages, so domain
code no longer imports `graph/generated`.

Requires Go 1.26+. Licensed under [MIT](./LICENSE).

## Why

Standard gqlgen puts every resolver in one package that imports
`graph/generated`. On large schemas every edit invalidates a build artifact
that can reach gigabytes — incremental compilation grinds to a halt.

## How

The plugin produces a two-tier layout:

- **Tier 1 — root resolver package.** Thin glue: `Mutation()` / `Query()` /
  `Subscription()` constructors, wrapper structs, embeds of the per-domain
  mixins. Methods reach callers via Go method promotion.
- **Tier 2 — per-domain packages.** One package per subdirectory of
  `graph/schema/`. Real business logic lives here. These packages **never
  import `graph/generated`** — gqlgen interfaces are satisfied structurally.

A *domain* is the parent directory name of a `.graphqls` file:
`graph/schema/todos/todo.graphqls` → domain `todos`. Files placed directly
under `graph/schema/` have no domain and stay in the root package.

## Quick start (new project)

### 1. Install

```bash
go get github.com/prusov/gqldomainresolver
```

### 2. Custom gqlgen entry point

gqlgen's default `go run github.com/99designs/gqlgen` cannot load plugins:

```go
// cmd/gqlgen/main.go
package main

import (
    "log"

    "github.com/99designs/gqlgen/api"
    "github.com/99designs/gqlgen/codegen/config"
    "github.com/prusov/gqldomainresolver"
)

func main() {
    cfg, err := config.LoadConfig("gqlgen.yml")
    if err != nil {
        log.Fatal(err)
    }
    plugin, err := gqldomainresolver.New()
    if err != nil {
        log.Fatal(err)
    }
    if err := api.Generate(cfg, api.AddPlugin(plugin)); err != nil {
        log.Fatal(err)
    }
}
```

`New()` with no options migrates every domain in the schema. New domains
added later are picked up automatically.

### 3. Point `resolver_template` at the safety-net template

gqlgen reads `resolver_template` from the local filesystem — a Go module
path won't work. The template is embedded in the package, so `go mod vendor`
copies it into your vendor tree and you can reference it directly:

```yaml
# gqlgen.yml
resolver:
  layout: follow-schema
  dir: graph/resolver
  package: resolver
  resolver_template: vendor/github.com/prusov/gqldomainresolver/resolver.gotpl
```

If you don't vendor, copy the file out of the module cache and commit it:

```bash
cp "$(go env GOMODCACHE)"/github.com/prusov/gqldomainresolver@*/resolver.gotpl \
   cmd/gqlgen/resolver.gotpl
```

```yaml
resolver_template: cmd/gqlgen/resolver.gotpl
```

With the vendor approach, `go mod vendor` keeps the template in sync with
the module version automatically. With the copy approach, re-copy after
upgrading the module — an out-of-date copy is the most common source of
mismatched output.

### 4. Write the root `Resolver` struct once

The plugin does not generate `graph/resolver/resolver.go`. Create it:

```go
package resolver

type Resolver struct {
    DomainMutationResolvers
    DomainQueryResolvers
    DomainSubscriptionResolvers
}
```

Drop any embed whose root type your schema doesn't define.

### 5. Generate and fill in resolver bodies

```bash
go run ./cmd/gqlgen
```

Each domain gets `graph/resolver/<domain>/*.resolvers.go` with panic stubs.
Replace each `panic(...)` with the real implementation — bodies are
preserved across regeneration via AST extraction.

## Migrating an existing project

Big-bang migration is impractical for any non-trivial codebase. The plugin
supports incremental migration via `WithEnabledDomains` — wire the plugin in
as a no-op first, then move one domain per PR. For projects that want the
greenfield default minus a handful of large or in-flight domains, pair
`New()` with `WithExcludedDomains("...")`.

See **[MIGRATION.md](./MIGRATION.md)** for the full playbook.

## Reference

- Godoc: <https://pkg.go.dev/github.com/prusov/gqldomainresolver>
- Domain-name normalization, keyword prefix, allowlist semantics — see
  godoc on [`New`], [`WithEnabledDomains`], [`WithExcludedDomains`],
  [`WithKeywordPrefix`].

[`New`]: https://pkg.go.dev/github.com/prusov/gqldomainresolver#New
[`WithEnabledDomains`]: https://pkg.go.dev/github.com/prusov/gqldomainresolver#WithEnabledDomains
[`WithExcludedDomains`]: https://pkg.go.dev/github.com/prusov/gqldomainresolver#WithExcludedDomains
[`WithKeywordPrefix`]: https://pkg.go.dev/github.com/prusov/gqldomainresolver#WithKeywordPrefix

## Limitations

- A given resolver field belongs to exactly one domain — splitting one root
  field across multiple domain packages isn't supported.
- Only one plugin per gqlgen run can implement `ResolverImplementer` — don't
  combine with another plugin that hooks the same interface.
- Two raw directory names that normalize to the same Go package (e.g.
  `order-flow` and `order_flow`) fail at codegen with a clear collision
  error. Rename one or pass `WithKeywordPrefix` to disambiguate.
