# Migrating an existing gqlgen project to gqldomainresolver

This guide describes how to retrofit `gqldomainresolver` into a project that
already uses gqlgen with a single `graph/resolver` package. The goal is to
move resolver bodies into per-domain packages so domain code no longer
imports `graph/generated`, without a "big-bang" rewrite.

The migration is incremental: each step keeps the project compiling, linting,
testing and behaving the same as before. The recommended cadence is one
domain per pull request.

> If you are starting a new project, ignore this document. Use the greenfield
> path in [README.md](./README.md): `New()` with no options migrates every
> domain at once.

---

## How the plugin behaves during migration

### Two gqlgen execution phases

1. **`Implement(prevImpl, field)`** — called by `resolvergen` for every
   resolver field. The returned string becomes the method body in the root
   `*.resolvers.go` file. Returning `""` makes the safety-net template skip
   the method entirely.
2. **`GenerateCode(data)`** — called after `resolvergen`. Produces the
   per-domain Tier-2 packages and the consolidated wiring file.

### `Implement` behavior for a migrated domain

For a field whose domain is in the allowlist:

```go
if domainEnabled {
    if prevImpl != "" {
        p.migratedImpls[key] = prevImpl
    }
    return "" // template emits no method in the root package
}
return prevImpl // (or panic stub) for non-migrated fields
```

For non-migrated fields, `prevImpl` is returned as-is, so existing bodies in
the root `*.resolvers.go` survive regeneration.

### `migratedImpls` — automatic body transfer on first migration

When a domain is enabled for the first time, its resolver bodies still live
in the root `*.resolvers.go`. `resolvergen` calls `Implement` and passes
`prevImpl`; the plugin caches it. By the time `GenerateCode` runs, the root
files have already been overwritten — but `GenerateCode` retrieves the
cached bodies from `migratedImpls` and writes them into the new Tier-2
files. No hand-copying is required.

### What is generated in the root package

The plugin generates a small set of files in the root resolver package on
every run, regardless of how many domains are migrated. These contain the
`Mutation()`/`Query()`/`Subscription()` constructors, the wrapper structs
that glue Tier-2 methods into gqlgen's interface contract, and the
per-object resolver constructors. Hand-written code must not duplicate
these definitions.

---

## Prerequisites

- gqlgen-based project with a single `graph/resolver` package containing
  all resolver bodies.
- Existing custom gqlgen entry point (or willingness to add one — gqlgen's
  default `go run github.com/99designs/gqlgen` cannot load plugins).
- A way to run code generation, build, lint and tests from a single command
  per step.

---

## Step 1 — Wire the plugin in as a no-op

PR 1 is pure infrastructure. The plugin is added but configured to migrate
no domain, so the existing `graph/resolver/**` is unchanged.

1. Add the dependency:

   ```bash
   go get github.com/prusov/gqldomainresolver
   ```

2. Create a custom gqlgen entry point if you don't already have one. Use
   `WithEnabledDomains()` with **no arguments** — this is the explicit
   empty-allowlist mode:

   ```go
   // cmd/gqlgen/main.go
   plugin, err := gqldomainresolver.New(
       gqldomainresolver.WithEnabledDomains(), // explicit empty → no-op
   )
   if err != nil { log.Fatal(err) }
   if err := api.Generate(cfg, api.AddPlugin(plugin)); err != nil {
       log.Fatal(err)
   }
   ```

   > Calling `WithEnabledDomains()` is **not** the same as omitting it.
   > Without the option the plugin migrates every domain (greenfield
   > default). With the option but no arguments it is a no-op. This
   > distinction is the foundation of incremental migration.

3. Configure `gqlgen.yml`. The plugin injects its own safety-net resolver
   template, so no `resolver_template` entry is required:

   ```yaml
   resolver:
     layout: follow-schema
     dir: graph/resolver
     package: resolver
   ```

   If you previously set
   `resolver_template: vendor/github.com/prusov/gqldomainresolver/resolver.gotpl`
   (or a copied path), you can drop the line entirely. Setting it explicitly
   is still honoured — the plugin yields to a custom override.

4. Run code generation. The expected diff is purely structural:
   - The plugin emits the wiring file(s) in the root resolver package.
   - The wrapper struct definitions and `Mutation()`/`Query()`
     constructors that previously lived in `*.resolvers.go` move into the
     generated wiring file. Delete those duplicates from
     `*.resolvers.go` if your old files contained them.
   - **No resolver method body changes.**

5. `go build ./... && lint && test` must be green.

**Done when**: the plugin is loaded, generation runs, build/lint/tests pass,
and no resolver behavior has changed.

---

## Step 2 — Extract shared helpers (if any) into a `graph`-generated-free package

Skip this step if your root resolver package has no helper functions that
domain bodies will reach for.

If the root `graph/resolver/` package contains helper functions used by many
resolvers (selection-set introspection, pagination flags, shared lookup
switches, etc.), move them into a new package — e.g. `graph/resolver/shared/`
— that does **not** import `graph/generated` or `graph/resolver`.

Why this is required:

- Tier-2 packages cannot import `graph/resolver` for helpers — `graph/resolver`
  imports `graph/generated`, and pulling that in defeats the whole point of
  the migration.
- `graph/resolver` will eventually import each Tier-2 package (for the
  embed). If Tier-2 imports back into `graph/resolver` for helpers, you get
  a cyclic import.

Recommended approach:

1. Create `graph/resolver/shared/` with the helper functions exported
   (capitalized names). Allowed dependencies: `context`, standard library,
   gqlgen runtime, your own model/repo packages — but **not**
   `graph/generated` and **not** `graph/resolver`.
2. In the existing root resolver package, leave thin unexported aliases
   delegating to `shared.*` so the existing call sites still compile. This
   lets you defer the call-site rename to a later cleanup PR.
3. Build, lint, test.

**Done when**: helpers are reachable as `shared.X(...)`, the root resolver
still compiles, and behavior is unchanged.

---

## Step 3 — Migrate the first (pilot) domain

Pick the smallest domain — one with the fewest resolver methods and the
narrowest dependency surface — as a canary. The pattern you establish here
will repeat for every subsequent domain.

1. Add the **raw** schema-directory name to the allowlist:

   ```go
   gqldomainresolver.WithEnabledDomains("todos")
   ```

   The raw name is what's on disk (`business-process`, not `businessprocess`;
   `import`, not `gqlimport`).

2. Run code generation. The plugin will:
   - Strip methods of the migrated domain from the root `*.resolvers.go`.
   - Create `graph/resolver/<pkg>/*.go` with the **real** bodies, transferred
     from `prevImpl` via the `migratedImpls` cache.
   - Update the wiring file to embed the Tier-2 mixin structs and route
     constructors to the new packages.

3. Fix imports in `graph/resolver/<pkg>/*.go`. The plugin transfers method
   bodies verbatim, but the import block of the Tier-2 file is built from
   scratch. `goimports` resolves most imports automatically, but it will
   miss anything ambiguous (typically project-internal sibling packages or
   aliased imports).

   To recover the original imports without guessing:

   ```bash
   # diff the deleted root file(s) of this domain
   git diff HEAD -- graph/resolver/<file>.resolvers.go | grep -E '^[-+]\s*"'

   # or show the previous full file
   git show HEAD:graph/resolver/<file>.resolvers.go | head -50
   ```

   Replace any helper calls that previously used local unexported names
   with their `shared.*` equivalents.

4. **Move helper functions and unrelated symbols by hand.** Only resolver
   *method bodies* migrate automatically. Free functions, constants, type
   aliases, or non-resolver methods that lived in the same root
   `*.resolvers.go` stay in the root package. If a migrated body references
   one, either move the helper into the domain package or extract it into
   `shared/`.

5. **Re-thread DI dependencies.** Migrated bodies now run on
   `Mixin<Domain>Mutation` / `<Type>Resolver` receivers, not on
   `mutationResolver` / `<type>Resolver`. Code that accessed `r.Resolver`
   fields (loggers, services, repositories) won't compile until you wire
   those dependencies through the domain struct that the wiring file
   instantiates.

6. **Delete the now-empty root `*.resolvers.go` files of this domain.**
   After generation they contain only the package header — no methods —
   because the template skipped emission. They are harmless to keep but
   create review noise on subsequent runs.

   The plugin removes them automatically (see `cleanupMigratedFiles`),
   but if any are left behind because the file was hand-edited or
   gqlgen wrote an unexpected basename, find them with:

   ```bash
   git status --short graph/resolver/ | awk '/^ ?D /{print $2}'
   ```

   and remove with `git rm`.

7. **Stage as renames, not delete+add.** git's similarity threshold for
   rename detection is 50%. After fixing imports, run:

   ```bash
   git add -A graph/resolver/
   git status --short graph/resolver/
   ```

   You should see `R  old -> new` lines, not `D` + `??`. If a file shows
   `D`+`A` instead, the Tier-2 content has drifted too far from the original
   — usually an over-eager edit. Use `git diff --cached --find-renames=40`
   to verify, and trim unnecessary changes until git detects the rename.
   Without proper rename staging, GitHub's PR view loses the rename marker
   and the diff becomes unreadable.

8. Build, lint, test. Hit a real GraphQL endpoint locally for at least one
   query and one mutation in the migrated domain. Compilation passing is
   not enough — domain packages don't import `graph/generated`, so a
   missing wiring shows up only at runtime.

9. Verify the Tier-2 package is `graph/generated`-free:

   ```bash
   go list -deps ./graph/resolver/<pkg>/... | grep generated || echo OK
   ```

**Done when**: the domain compiles, tests pass, behavior matches, the Tier-2
package does not depend on `graph/generated`, and the diff stages as
renames.

---

## Step 4 — Roll out to the remaining domains

Apply Step 3 to each remaining domain — one PR per domain. Order from
smallest to largest so review pattern-recognition kicks in early. Do not
combine two domains in one PR; review fatigue defeats the cadence.

### Domains with dashes, mixed case, or Go keywords

The plugin normalizes raw directory names to valid Go package identifiers
automatically:

| Schema directory (raw)   | Go package (normalized) |
|--------------------------|-------------------------|
| `business-process`       | `businessprocess`       |
| `order_flow`             | `orderflow`             |
| `OrderFlow`              | `orderflow`             |
| `import`                 | `gqlimport`             |
| `2fa`                    | `gql2fa`                |

The allowlist always takes the **raw** name as it appears on disk
(case-sensitive):

```go
WithEnabledDomains("business-process", "import", "2fa") // not "businessprocess" etc.
```

A name that does not match any directory in the schema fails codegen with a
clear error — typos and case mismatches don't silently degrade to a no-op.

The keyword prefix defaults to `gql` and applies to Go keywords, the literal
name `schema`, and names starting with a digit. Override with
`WithKeywordPrefix("...")` if needed.

If two raw directory names normalize to the same package (e.g. `order-flow`
and `order_flow`), code generation fails with a clear collision error.
Rename one, or pass a `WithKeywordPrefix` value that disambiguates.

### Schema files at the root of `graph/schema/`

Schema files placed directly under `graph/schema/` (not in any subdirectory)
have no domain. The plugin leaves their resolvers in the root package; they
keep using gqlgen's standard `prevImpl` mechanism, so existing bodies
survive regeneration. This is a permanent, supported state — you don't have
to relocate every root file.

If you do want to consolidate root files into new domains for cleanliness,
each new domain folder is a separate PR following Step 3.

---

### Inverting the allowlist for large projects

For projects with dozens of domains where most are ready to migrate but a
few large or in-flight ones aren't, maintaining an ever-growing
`WithEnabledDomains(...)` list becomes noisy. Once the migration has covered
"almost everything", flip the configuration to a denylist:

```go
plugin, err := gqldomainresolver.New(
    gqldomainresolver.WithExcludedDomains(
        "billing",       // mid-flight refactor, blocked on team X
        "legacy-import", // scheduled for deletion in Q3
    ),
)
```

This switches the plugin to greenfield-with-exceptions: every domain not in
the list is migrated automatically, including newly added ones. The
remaining holdouts get migrated by deleting their entry from
`WithExcludedDomains` (one PR per removed entry — same review cadence as
adding to the allowlist).

`WithEnabledDomains` and `WithExcludedDomains` can also be combined: the
allowlist is applied first, then the exclude list subtracts. Useful for
temporarily parking a single domain out of an already-enabled set without
rewriting the allowlist. Both lists fail codegen on names that don't match
any schema directory, so typos surface loudly in either mode.

The choice of mode is a one-way switch in spirit, not in code: flip when
the list of excluded domains becomes shorter than the list of enabled ones.

---

## Step 5 — Drop the allowlist

Once every domain (or every domain you intend to migrate) has been moved,
remove `WithEnabledDomains` entirely from the gqlgen entry point:

```diff
- plugin, err := gqldomainresolver.New(
-     gqldomainresolver.WithEnabledDomains(
-         "todos", "users", "billing", /* ... */
-     ),
- )
+ plugin, err := gqldomainresolver.New()
```

Run code generation. The diff should be empty: with all domains already in
the allowlist, removing the option is equivalent to "all domains enabled by
default". The project is now in the same configuration as a greenfield
project.

Any new domain added to the schema later is migrated automatically without
touching the entry point.

---

## Step 6 — Cleanup

1. If Step 2 introduced delegating aliases in the root package, replace the
   call sites with direct `shared.*` calls and remove the aliases.
2. Update internal docs 
   to describe the two-tier resolver layout — new resolvers go in the
   Tier-2 package of their domain.
3. Add a CI check: Tier-2 packages must not import `graph/generated`.
   Simple `go list -deps` or `grep` is enough:

   ```bash
   for d in graph/resolver/*/; do
       if go list -deps "./$d..." 2>/dev/null | grep -q '/graph/generated$'; then
           echo "ERROR: $d depends on graph/generated"; exit 1
       fi
   done
   ```

4. Measure build size and `go build ./...` time before-and-after; record
   the wins for posterity (and future arguments in favor of the layout).

---

## Migration principles

- **One domain per PR.** Do not combine the bootstrap step (Step 1) with a
  domain migration. Do not migrate two domains in one PR.
- After every step: regenerate, build, lint, test. All must be green
  before pushing.
- Do not edit generated root files (`*.resolvers.go`, the wiring file) by
  hand — they are overwritten on every run. Tier-2 files are hand-edited;
  their bodies are preserved on regeneration via AST extraction.
- **Move only — do not refactor.** When migrating a domain, copy the body
  verbatim. Forbidden in the same PR: renaming locals, reordering
  operations, adding/removing logging or metrics, tightening validation,
  fixing edge cases noticed in passing. The only acceptable code change is
  rewriting helper references (`localFn(...)` → `shared.LocalFn(...)`).
  Anything else lands in a follow-up PR after the move is in.
- Every step must be revertable with a single `git revert` without breaking
  the build.
- **Plugin bugs found mid-migration**: stop, do not patch the vendored
  plugin in place. Roll back the in-progress migration, file an issue (or
  fix and submit upstream), upgrade to the fixed version, then retry.

---

## Stacking the work into a reviewable series of PRs

A monolithic "migrate everything" branch is unreviewable. Split the work
into a stack of small PRs, each branching off the previous one in the chain
until merged into `main`:

| # | Branch / PR | Contents | Review effort |
|---|---|---|---|
| 1 | `chore/gqldomainresolver-bootstrap` | Dependency, custom `cmd/gqlgen/main.go` with `WithEnabledDomains()` (empty), template copy, `gqlgen.yml` change. **No domain migration.** | Small — reviewer evaluates the approach. |
| 2 | `refactor/resolver-shared-helpers` | New `graph/resolver/shared/` package, alias delegators. | Medium. |
| 3 | `feat/domain-resolver-<pilot>` | First domain migration. | Reference PR — establishes the pattern. |
| 4..N | `feat/domain-resolver-<name>` | One domain per PR. | Mostly mechanical pattern-match by the reviewer. |
| Final | `chore/resolver-cleanup` | Drop `WithEnabledDomains`, replace alias call sites, add CI check, update docs. | Small. |

Each branch must build and test on its own. Target each PR's `base` at the
previous PR's branch (PR #4 → PR #3 → PR #2 → PR #1 → `main`). Once PR #1
merges, rebase the rest of the stack onto `main` and force-push with
`--force-with-lease`.

If the original work happened on one big branch and the commits are not
already atomic per domain, it is often easier to recreate each domain PR
from `main` by checking out the final state of the relevant files and
running generation fresh, rather than cherry-picking partial commits.

---

## Pre-merge checklist for each domain PR

- [ ] Code generation runs clean; the diff is what you expect.
- [ ] Empty root `*.resolvers.go` files of the migrated domain are deleted.
- [ ] Files are staged as `R  old -> new` (not `D` + `??`). Verify with
      `git status --short graph/resolver/`.
- [ ] Tier-2 package does not depend on `graph/generated`
      (`go list -deps ./graph/resolver/<pkg>/...`).
- [ ] `go build ./...` is green.
- [ ] Linter is green.
- [ ] Tests are green.
- [ ] Local GraphQL smoke test: at least one query and one mutation from
      the migrated domain return the same result as before.

---

## Risks and mitigations

| Risk | Mitigation |
|---|---|
| `migratedImpls` cache miss — bodies do not transfer to Tier-2 | After regen, diff Tier-2 files against the previous root files. Recover bodies from `git show HEAD:<old-path>` if needed. |
| Duplicate type definitions after Step 1 (`type xyzResolver struct` exists in both `*.resolvers.go` and the wiring file) | Compiler points at the duplicate. Delete the version in `*.resolvers.go` — the wiring file is authoritative. |
| Cyclic import between Tier-1 and Tier-2 | Move the offending helper into `shared/`. Tier-2 must depend only on `shared/`, never on `graph/resolver`. |
| Tier-2 accidentally pulls in `graph/generated` | CI check from Step 6 catches it. Usually caused by referencing a generated type directly in a body — switch to the `model.*` form. |
| Custom directives (auth, idempotency) regress | Validate on the pilot domain. Directives apply at the Tier-1 layer; Tier-2 should not see a difference. |
| Concurrent merge conflicts in `master` while the stack is open | New resolvers in already-migrated domains land in Tier-2 automatically on the next regen. New resolvers in non-migrated domains stay in the root — no conflict with the stack. |
| Runtime regression invisible to unit tests | Mandatory local smoke test on every domain PR; for large domains, run a curated set of representative queries. |
