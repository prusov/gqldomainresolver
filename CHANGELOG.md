# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

While the major version is `0`, the public API is allowed to break between
minor versions; breaking changes will be called out explicitly here.

## [Unreleased]

### Changed
- `WithEnabledDomains` now fails codegen if any listed name doesn't match a
  schema directory (previously silently no-op'd). Catches typos and
  case mismatches like `WithEnabledDomains("Todos")` for a `todos/` dir.
- Domain-collision diagnostics aggregate every clash into one error
  (previously bailed on the first), so a single codegen run surfaces the
  full list to fix.

## [0.1.0] - Initial release

### Added
- `gqldomainresolver.New(opts ...Option) (*Plugin, error)` — gqlgen plugin
  that splits the resolver package into per-domain Go packages, decoupling
  domain code from `graph/generated`.
- `WithEnabledDomains(domains ...string)` — incremental-migration allowlist
  keyed by raw schema-directory names. Calling with no arguments produces
  an explicit empty allowlist (plugin is a no-op — migration bootstrap).
- `WithKeywordPrefix(prefix string)` — override for the prefix prepended to
  domain names that collide with Go keywords, equal `schema`, or start with
  a digit. Default `gql` (`DefaultKeywordPrefix`).
- Two-tier output layout: thin Tier-1 root resolver package
  (`mutation.resolvers.go` / `query.resolvers.go` /
  `subscription.resolvers.go` / `object.resolvers.go`), plus per-domain
  Tier-2 packages that satisfy gqlgen interfaces structurally.
- Hand-written method bodies preserved across regeneration via AST
  extraction; first-time migrations rehydrate from `prevImpl` cache.
- Safety-net resolver template (`templates/resolver.gotpl`).
