# gqldomainresolver

A gqlgen plugin that splits GraphQL resolvers into isolated domain packages, eliminating the large `graph/generated` import from business logic.

## Problem

Standard gqlgen puts every resolver in one package that imports `graph/generated`. On large schemas this produces ~2 GB build artifacts because every file recompiles the whole generated graph.

## Solution: Two-tier resolver pattern

**Tier 1 — root package (`graph/resolver/*.resolvers.go`)**

For migrated domains the plugin's `Implement()` returns `""`, so the safety-net
template emits **no** method declarations for those fields — the corresponding
`*.resolvers.go` file is empty (header only). Methods reach callers via Go
method promotion: each generated wrapper (`mutationResolver` / `queryResolver`
/ `subscriptionResolver`) embeds the kind-specific
`DomainMutationResolvers` / `DomainQueryResolvers` / `DomainSubscriptionResolvers`,
which value-embeds the matching per-domain
`Mixin<Domain>Mutation/Query/Subscription` struct (each generated in its own
`mutation.resolvers.go` / `query.resolvers.go` / `subscription.resolvers.go`
file). Splitting per root kind avoids ambiguous selectors
when the same field name appears as both a `Query` and a `Subscription`
(e.g. `userNotifications`).

**Tier 2 — domain packages (`graph/resolver/<domain>/`)**

Actual business logic. These packages **never import `graph/generated`** — interfaces are satisfied by Go duck typing (matching method names and signatures).

## How domains are determined

Domain = name of the subdirectory containing the schema file:

| Schema file | Domain |
|---|---|
| `graph/schema/todos/todo.graphqls` | `todos` |
| `graph/schema/schema.graphqls` | *(none — root, gets panic stubs)* |
| `graph/schema/common/directives.graphqls` | `common` *(no resolver fields → no package generated)* |

The directory name is split into two views:

- **`Domain.Raw`** — the directory name as it appears on disk
  (`"business-process"`). It is what you pass to `WithEnabledDomains`.
- **`Domain.Pkg`** — the normalized form (`"businessprocess"`). It is the
  on-disk package directory, the Go package name and the leaf segment of
  the import path.

The `Mixin` struct prefix is built from `Raw` and treats `-` / `_` as word
boundaries (e.g. `business-process` → `MixinBusinessProcess`,
`order_flow` → `MixinOrderFlow`) so the receiver name reads naturally even
though the package itself is `businessprocess` / `orderflow`.

### Normalization (strip-only lowercase)

1. Lowercase the whole string.
2. Strip every dash and underscore.
3. If the result is a Go keyword, equals `"schema"`, or starts with a digit,
   prepend the keyword prefix (default `"gql"`, configurable via
   `WithKeywordPrefix`).
4. If the result is empty (e.g. dir was `"-"` or `"_"`), the field is
   skipped — no domain package is generated.

| Schema dir          | Package          |
|---------------------|------------------|
| `todos`             | `todos`          |
| `business-process`  | `businessprocess`|
| `order_flow`        | `orderflow`      |
| `OrderFlow`         | `orderflow`      |
| `import`            | `gqlimport`      |
| `type`              | `gqltype`        |
| `2fa`               | `gql2fa`         |

A literal directory called `schema` is reserved for the root convention
(`graph/schema/schema.graphqls` and friends) — fields under it stay in the
root resolver package.

### Collision detection

If two different raw directory names normalize to the same package
(e.g. `order-flow` and `order_flow` → `orderflow`), `GenerateCode` returns
an error pointing at both directories. Resolve by renaming one or by
choosing a keyword prefix that disambiguates them.

## Generated code conventions

| GraphQL location | Generated Go |
|---|---|
| `Mutation.createTodo` | method `(m *MixinTodosMutation) CreateTodo(ctx, input)` |
| `Query.todos` | method `(q *MixinTodosQuery) Todos(ctx)` |
| `Subscription.todoChanged` | method `(s *MixinTodosSubscription) TodoChanged(ctx)` |
| field resolver on `Todo.user` | method `(r *TodoResolver) User(ctx, obj)` |
| field resolver on `CatalogCategory.parent` (in `catalog/`) | method `(r *CategoryResolver) Parent(ctx, obj)` |

The `Mixin` prefix on root structs keeps the struct name from starting with
the package name (otherwise `revive`'s `package-stutters` rule triggers,
e.g. `todos.TodosMutation`). Methods reach each root wrapper via Go method
promotion through the kind-specific
`Domain{Mutation,Query,Subscription}Resolvers` struct.

Object resolver structs strip the PascalCase form of the domain directory
name from the front of the GQL type name (when the type name starts with it
and the remainder begins with an uppercase letter). The remainder + `Resolver`
becomes the struct name. This avoids stutter at the call site —
`catalog.CategoryResolver{}` instead of `catalog.CatalogCategoryResolver{}`.

| Domain (`Raw`) | GQL type | Generated struct |
|---|---|---|
| `catalog` | `CatalogCategory` | `CategoryResolver` |
| `import` | `ImportStatus` | `StatusResolver` |
| `import` | `Entity` | `EntityResolver` *(no prefix match)* |
| `tasks` | `Task` | `TaskResolver` *(`Task` does not start with `Tasks`)* |
| `catalog` | `Catalog` *(equals prefix)* | `Resolver` |

Root-package fields **without** a domain (e.g. `Query.hello` defined in the root
`schema.graphqls`) keep their classic resolver method on the root package and
their bodies are preserved across regeneration — write them by hand.

## Connecting to a project

### 1. Add the dependency

```bash
go get github.com/prusov/gqldomainresolver
```

### 2. Create a custom gqlgen entry point

Standard `go run github.com/99designs/gqlgen` doesn't know about plugins. Create a small main:

```go
// cmd/gqlgen/main.go
package main

import (
    "log"
    "os"

    "github.com/99designs/gqlgen/api"
    "github.com/99designs/gqlgen/codegen/config"
    "github.com/prusov/gqldomainresolver"
)

func main() {
    cfg, err := config.LoadConfig("gqlgen.yml")
    if err != nil {
        log.Fatal(err)
    }
    if err := api.Generate(cfg, api.AddPlugin(
        gqldomainresolver.New(
            gqldomainresolver.WithEnabledDomains("todos", "users"),
        ),
    )); err != nil {
        log.Fatal(err)
    }
}
```

Run it with:
```bash
go run ./cmd/gqlgen
```

### 3. Point gqlgen.yml at the safety-net resolver template

The plugin ships a resolver template at `github.com/prusov/gqldomainresolver/templates/resolver.gotpl`. Set it in `gqlgen.yml`:

```yaml
resolver:
  layout: follow-schema
  dir: graph/resolver
  package: resolver
  resolver_template: github.com/prusov/gqldomainresolver/templates/resolver.gotpl
```

This template skips method declarations for root fields that have a domain package (the plugin returns `""` from `Implement()`). Those methods reach callers via Go method promotion through the generated `Domain{Mutation,Query,Subscription}Resolvers` structs.

### 4. Create the Resolver struct

The plugin does not generate `graph/resolver/resolver.go`. Create it once:

```go
package resolver

type Resolver struct {
    DomainMutationResolvers
    DomainQueryResolvers
    DomainSubscriptionResolvers
}
```

Embed only the kinds that actually exist in your schema — if your schema has no `Subscription` root, drop `DomainSubscriptionResolvers` (the file that defines it isn't generated).

Everything else is generated on each `go run ./cmd/gqlgen` run, split per root kind:

| File | Contents |
|---|---|
| `graph/resolver/mutation.resolvers.go` | `DomainMutationResolvers`, `Mutation()` ctor, `mutationResolver` wrapper |
| `graph/resolver/query.resolvers.go` | `DomainQueryResolvers`, `Query()` ctor, `queryResolver` wrapper |
| `graph/resolver/subscription.resolvers.go` | `DomainSubscriptionResolvers`, `Subscription()` ctor, `subscriptionResolver` wrapper |
| `graph/resolver/object.resolvers.go` | per-object constructors (`(r *Resolver) Todo()` etc.) + root-package wrappers for non-migrated domains |

## Incremental migration with `WithEnabledDomains`

The plugin is **opt-in per domain**. With an empty allowlist (`New()` with no
options) the plugin is a no-op — adding it to a project introduces zero diff
in your existing resolvers. Domains are migrated one at a time by adding their
names to `WithEnabledDomains`.

This is the recommended path for retrofitting the plugin into an existing
large project, where a "big-bang" migration of every domain in one PR is
impractical.

### Recommended rollout

1. **PR 1 — wire the plugin without migrating anything.**

   ```go
   api.AddPlugin(gqldomainresolver.New()) // empty allowlist → no-op
   ```

   Set `resolver_template` in `gqlgen.yml`, add `cmd/gqlgen`, run
   `go run ./cmd/gqlgen`. Result: no changes to `graph/resolver/**`. The PR
   is purely infrastructure — easy to review, trivially revertable.

2. **PR 2..N — migrate one domain per PR.**

   ```go
   gqldomainresolver.New(
       gqldomainresolver.WithEnabledDomains("todos"), // add one name at a time
   )
   ```

   Each PR:
   - Adds one name to the list.
   - Regenerates code (`go run ./cmd/gqlgen`) — produces a new
     `graph/resolver/<domain>/` package and rewrites the corresponding root
     stubs to delegate via promotion.
   - **Resolver bodies migrate automatically.** On the first run with a
     freshly enabled domain the plugin captures the existing body of every
     resolver field (passed in by gqlgen as `prevImpl` to `Implement()` before
     the root file is overwritten) and replays it into the corresponding
     method in the new domain package. Hand-copying is no longer required.

   **What you must still do by hand after the auto-migration:**

   - **Verify imports.** Bodies are copied verbatim, but the *imports* of the
     old root file aren't. `goimports` (which gqlgen runs as part of code
     generation) usually adds the missing ones automatically, but
     module-internal imports for sibling packages can be ambiguous — open the
     generated `graph/resolver/<domain>/<file>.go` and check that all symbols
     resolve. Pay special attention to imports that were aliased.
   - **Move helper functions and unused symbols.** Only resolver method
     *bodies* migrate. Free functions, constants, type aliases, or
     non-resolver methods that lived in the same `*.resolvers.go` file stay
     in the root package. If a migrated body references such a helper, either
     move the helper into the domain package or export it and import from the
     root resolver package.
   - **Re-validate the resolver wiring.** Migrated bodies now run on
     `Mixin<Domain>Mutation` / `<Type>Resolver` receivers, not on
     `mutationResolver` / `<type>Resolver`. Code that accessed `r.Resolver`
     fields (DI handles, loggers, etc.) won't compile until you re-thread
     those dependencies — typically via fields on the domain struct that
     `object.resolvers.go` instantiates.
   - **Run the full test suite.** Compilation passing isn't enough — domain
     packages don't import `graph/generated`, so a missing wiring shows up
     only at runtime when the GraphQL handler dispatches a query.

3. **Roll back a domain by removing its name** from the list and regenerating.
   The domain falls back to root-package resolvers, but the **migrated bodies
   are not auto-copied back** — they stay in `graph/resolver/<domain>/` and
   the regenerated root file gets fresh panic stubs. Either keep the domain
   directory and live with the dual structure, copy the bodies back manually,
   or delete the domain directory if the migration was abandoned.

### Behavior of the allowlist

| Input | Effect |
|---|---|
| Empty / `nil` | Plugin is a no-op for **every** schema file. |
| `["todos"]` | Only `todos` is migrated; all other domains use root-package resolvers. |
| `["business-process"]`, `["import"]`, `["2fa"]` | Migrated — names are normalized for the Go side (`businessprocess`, `gqlimport`, `gql2fa`), but the allowlist key stays the raw on-disk name. |
| `["nonexistent"]` | Silently tolerated — useful for adding a name before its schema files land. |
| `["todos", "todos"]` | Deduplicated to `["todos"]`. |

Names are matched **case-sensitively against the raw schema-dir name**.
Pass what you wrote on disk (e.g. `"business-process"`), not the normalized
package name (`"businessprocess"`).

## Schema layout

```
graph/schema/
  schema.graphqls          ← root types (Query, Mutation); gets panic stubs
  todos/
    todo.graphqls           ← "todos" domain
  tasks/
    task.graphqls           ← "tasks" domain
```

## Preserving hand-written code

The plugin preserves resolver bodies in three different scenarios — each uses a different mechanism:

1. **Steady-state regeneration of an already-migrated domain.** The plugin parses the existing files in `graph/resolver/<domain>/` via AST and replays each method's body into the new file by matching receiver type + method name.

2. **First-time migration of a domain** (newly added to `WithEnabledDomains`). The domain directory doesn't exist yet, and by the time `GenerateCode()` runs gqlgen has already overwritten the root `*.resolvers.go`. Instead, the plugin captures `prevImpl` inside `Implement()` (which fires *before* the overwrite) and stashes it keyed by `<ObjectName>.<FieldName>`. When the new domain file is rendered, this cache is consulted as a fallback — the body lands in the right method on the right receiver in the domain package. See the migration section above for what to verify after this auto-move.

3. **Root-package stubs for non-migrated fields** (e.g. `Query.hello` defined in `schema.graphqls` with no domain). The default gqlgen `prevImpl` mechanism applies: bodies survive across regeneration as long as the field still exists in the schema.

Helper functions hand-written in a domain file but no longer referenced by a generated method don't disappear silently — they're moved into a commented-out `// !!! WARNING !!!` block at the bottom of the file. Salvage what you need, then delete the block.

## Generated layout

After `go run ./cmd/gqlgen` you get:

```
graph/resolver/
  resolver.go                         ← you write this once: type Resolver struct{ DomainMutationResolvers; DomainQueryResolvers; DomainSubscriptionResolvers }
  mutation.resolvers.go     ← generated: DomainMutationResolvers, mutationResolver, Mutation() ctor
  query.resolvers.go        ← generated: DomainQueryResolvers, queryResolver, Query() ctor
  subscription.resolvers.go ← generated: DomainSubscriptionResolvers, subscriptionResolver, Subscription() ctor
  object.resolvers.go       ← generated: per-object constructors + non-migrated wrapper types
  schema.resolvers.go       ← generated: methods for root fields without a domain (e.g. Query.hello)
  todos/
    todo.resolvers.go       ← generated: MixinTodosMutation, MixinTodosQuery, TodoResolver methods
  tasks/
    task.resolvers.go       ← generated: MixinTasksMutation, MixinTasksQuery, ...
  catalog/
    catalog-category.resolvers.go ← generated: MixinCatalogQuery, CategoryResolver (stripped)
```

The import paths used in the generated `*.resolvers.go` files are derived automatically from `resolver.dir` / `resolver.package` in `gqlgen.yml` — the plugin is module-agnostic.

## Limitations

- **Domain name = parent directory name** of the schema file. The name is normalized (strip-only lowercase) into a Go package identifier, so dashes, underscores, mixed case, Go keywords and leading digits are all supported. Two different directory names that normalize to the same package fail at codegen time with a clear collision error. The literal name `schema` remains reserved for the root convention.
- A given resolver field belongs to exactly one domain — the one of its `.graphqls` file. Splitting one root field across multiple domain packages isn't supported.
- The `resolver_template` in `gqlgen.yml` must be `github.com/prusov/gqldomainresolver/templates/resolver.gotpl` (or a compatible template that skips method emission when `Implement()` returns `""`). Using gqlgen's default template will cause duplicate method declarations on the root resolver.
- Only one plugin per gqlgen run can implement `ResolverImplementer` — don't combine with another plugin that hooks the same interface.

## Troubleshooting

**Compiled but the resolver isn't being called.** Check that the directory name is in your `WithEnabledDomains` list — the allowlist takes the *raw* on-disk name (e.g. `"business-process"`), not the normalized package name (`"businessprocess"`).

**`GenerateCode` failed with a collision error.** Two enabled directories normalize to the same package (e.g. `order-flow` and `order_flow` both become `orderflow`). Rename one of them, or pass `WithKeywordPrefix` to a value that disambiguates the conflict.

**Old code reappeared after I renamed a method.** Look for the `// !!! WARNING !!!` block near the bottom of the affected domain file — gqlgen preserves orphaned function bodies there so you don't lose work. Move what you still need elsewhere, then delete the block.

**`graph/generated` got imported in a domain package.** Domain packages must be `graph/generated`-free by design. If you see the import, it usually means a hand-written method body references a generated type directly. Use the model package instead (`model.Todo`, etc.) — the generated interfaces are satisfied structurally, no import required.
