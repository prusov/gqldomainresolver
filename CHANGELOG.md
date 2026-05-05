# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed
- The plugin now injects its own safety-net resolver template via gqlgen's
  `ConfigMutator` hook, materializing the embedded `resolver.gotpl` to a
  temp file at codegen time. Consumers no longer need to set
  `resolver_template` in `gqlgen.yml` or rely on `go mod vendor` /
  module-cache copies to surface the file at a stable path. Setting
  `resolver_template` explicitly is still honoured and overrides the
  bundled template, so existing configurations keep working unchanged.

### Migration
- Existing consumers can drop
  `resolver_template: vendor/github.com/prusov/gqldomainresolver/resolver.gotpl`
  (or any other path that points at the bundled template) from `gqlgen.yml`.

## [1.0.2] - 2026-05-05

### Changed
- Restored the original gqlgen `// !!! WARNING !!!` comment block above the
  `RemainingSource` dump in the safety-net `resolver.gotpl`, so users see
  the same guidance gqlgen ships with when old resolver code is preserved.

## [1.0.1] - 2026-05-05

### Changed
- Moved the safety-net resolver template from `templates/resolver.gotpl` to
  the repository root (`resolver.gotpl`) and embedded it via `//go:embed`,
  so `go mod vendor` includes the file in the vendor tree. Users can now
  point `resolver_template` at
  `vendor/github.com/prusov/gqldomainresolver/resolver.gotpl` directly
  instead of copying the file into their repo.

### Breaking
- Anyone copying the template out of the module cache must update the path
  from `templates/resolver.gotpl` to `resolver.gotpl`.

## [1.0.0] - 2026-05-04

Initial release.

### Added
- `gqldomainresolver.New(opts ...Option) (*Plugin, error)` — gqlgen plugin
  that splits the resolver package into per-domain Go packages, decoupling
  domain code from `graph/generated`.
- `WithEnabledDomains(domains ...string)` — incremental-migration allowlist
  keyed by raw schema-directory names. Calling with no arguments produces
  an explicit empty allowlist (plugin is a no-op — migration bootstrap).
  Fails codegen if any listed name doesn't match a schema directory, to
  catch typos and case mismatches.
- `WithExcludedDomains(domains ...string)` — denylist keyed by raw
  schema-directory names. Excluded domains stay in the root resolver
  package. Combinable with `WithEnabledDomains` (allowlist first, then
  exclude subtracts) or used standalone with the greenfield default for a
  "migrate everything except these" configuration. Fails codegen on names
  that don't match any schema directory, mirroring `WithEnabledDomains`.
- `WithKeywordPrefix(prefix string)` — override for the prefix prepended to
  domain names that collide with Go keywords, equal `schema`, or start with
  a digit. Default `gql` (`DefaultKeywordPrefix`).
- Two-tier output layout: thin Tier-1 root resolver package
  (`mutation.resolvers.go` / `query.resolvers.go` /
  `subscription.resolvers.go` / `object.resolvers.go`), plus per-domain
  Tier-2 packages that satisfy gqlgen interfaces structurally.
- Hand-written method bodies preserved across regeneration via AST
  extraction; first-time migrations rehydrate from `prevImpl` cache.
- Domain-collision diagnostics aggregate every clash into one error so a
  single codegen run surfaces the full list to fix.
- Safety-net resolver template (`templates/resolver.gotpl`).
