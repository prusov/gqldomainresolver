package domainresolver

import (
	"log/slog"

	"github.com/99designs/gqlgen/codegen"
)

// Plugin implements gqlgen's ResolverImplementer + CodeGenerator.
//
// The import path for generated domain packages is derived at codegen time
// from the resolver section of gqlgen.yml — the plugin is module-agnostic.
//
// The plugin is opt-in per domain: it migrates only domains explicitly listed
// via WithEnabledDomains. With an empty allowlist the plugin is a no-op —
// adding the plugin to a project introduces no diff until the user enables
// a specific domain. This enables incremental migration of large projects.
type Plugin struct {
	enabledSet map[string]bool
}

// Option configures a Plugin at construction time.
type Option func(*Plugin)

// WithEnabledDomains enables migration for the listed domains.
// Names that fail isValidDomain (Go keywords, dashes, "schema") are silently
// dropped; duplicates are deduplicated. Unknown names (no matching schema
// directory) are tolerated — they simply have no effect.
func WithEnabledDomains(domains ...string) Option {
	return func(p *Plugin) {
		if p.enabledSet == nil {
			p.enabledSet = map[string]bool{}
		}
		for _, d := range domains {
			if !isValidDomain(d) {
				slog.Warn("domainresolver: ignored invalid domain name", "name", d)
				continue
			}
			p.enabledSet[d] = true
		}
	}
}

// New constructs the plugin. With no options the allowlist is empty and the
// plugin is a no-op — call WithEnabledDomains to migrate specific domains.
func New(opts ...Option) *Plugin {
	p := &Plugin{}
	for _, opt := range opts {
		opt(p)
	}

	return p
}

func (p *Plugin) Name() string { return "domain-resolver" }

// domainFor returns the domain of a schema file, filtered through the
// allowlist. Domains not in the allowlist (or any domain when the allowlist
// is empty) are returned as "" — equivalent to a root field with no domain.
func (p *Plugin) domainFor(schemaPath string) string {
	d := extractDomain(schemaPath)
	if d == "" {
		return ""
	}
	if !p.enabledSet[d] {
		return ""
	}

	return d
}

var _ interface {
	Implement(string, *codegen.Field) string
	GenerateCode(*codegen.Data) error
} = (*Plugin)(nil)
