# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

While the major version is `0`, the public API is allowed to break between
minor versions; breaking changes will be called out explicitly here.

## [Unreleased]

### Added
- `WithExcludedDomains(domains ...string)` — denylist keyed by raw
  schema-directory names. Excluded domains stay in the root resolver
  package. Combinable with `WithEnabledDomains` (allowlist first, then
  exclude subtracts) or used standalone with the greenfield default for a
  "migrate everything except these" configuration. Fails codegen on names
  that don't match any schema directory, mirroring `WithEnabledDomains`.

## [0.1.0] - 2026-05-04

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
