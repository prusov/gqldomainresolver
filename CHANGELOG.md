# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

While the major version is `0`, the public API is allowed to break between
minor versions; breaking changes will be called out explicitly here.

## [Unreleased]

## [0.1.0] - TBD

### Added
- Initial public release.
- Two-tier resolver layout: root-package thin delegation + per-domain packages with the actual business logic.
- `New(opts ...Option) (*Plugin, error)` constructor that returns an error instead of panicking on bad config.
- `WithEnabledDomains(...string)` for incremental, opt-in migration.
- `WithKeywordPrefix(string)` to override the default `"gql"` prefix used when a domain directory name collides with a Go keyword, equals `"schema"`, or starts with a digit.
- Strip-only-lowercase domain → package name normalisation.
- `Mixin<Domain>` lead-in on per-domain receiver types to avoid `revive`'s `package-stutters`.
- AST round-trip preserves hand-written method bodies and helper functions across regeneration.
- Migrated-impl cache: bodies stashed during `Implement()` are rehydrated on first-time migration.

[Unreleased]: https://github.com/prusov/domainresolver/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/prusov/domainresolver/releases/tag/v0.1.0
