package domainresolver

import (
	"fmt"

	"github.com/99designs/gqlgen/codegen"
)

// DefaultKeywordPrefix is the prefix used by normalizeDomain when a domain
// name collides with a Go keyword, equals "schema", or starts with a digit.
const DefaultKeywordPrefix = "gql"

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
	// enabledSet is keyed by the *raw* schema directory name — that is what
	// the user sees on disk, so it is the user-facing key for the allowlist.
	enabledSet map[string]bool

	// keywordPrefix disambiguates domain names that would otherwise be
	// invalid Go identifiers (Go keywords, "schema", names starting with
	// a digit). Defaults to DefaultKeywordPrefix.
	keywordPrefix string

	// migratedImpls captures the previous resolver body of each field whose
	// domain becomes enabled. resolvergen passes prevImpl (the body of the
	// hand-written method on the root-package wrapper, e.g. *todoResolver) into
	// Implement() and then overwrites the source file. By the time
	// GenerateCode() runs the body is gone from disk, so the AST rewriter on
	// the domain package can't find it on first migration. We stash it here
	// keyed by "<ObjectName>.<GoFieldName>" so renderDomainFile can fall back
	// to it when no body is found in the domain package.
	migratedImpls map[string]string
}

// Option configures a Plugin at construction time.
type Option func(*Plugin)

// WithEnabledDomains enables migration for the listed schema-directory names.
// Names are matched against the *raw* directory name (e.g. "business-process",
// not the normalized "businessprocess"). Duplicates are deduplicated; empty
// entries are dropped.
func WithEnabledDomains(domains ...string) Option {
	return func(p *Plugin) {
		if p.enabledSet == nil {
			p.enabledSet = map[string]bool{}
		}
		for _, d := range domains {
			if d == "" {
				continue
			}
			p.enabledSet[d] = true
		}
	}
}

// WithKeywordPrefix overrides the prefix used to disambiguate domain names
// that collide with Go keywords, "schema", or that start with a digit.
// The default is DefaultKeywordPrefix ("gql"), so e.g. an "import" directory
// produces package "gqlimport" and "2fa" produces "gql2fa".
//
// The prefix must be a non-empty valid Go identifier prefix: it must start
// with an ASCII lowercase letter and may contain only lowercase letters and
// digits afterwards. Invalid prefixes cause New() to panic.
func WithKeywordPrefix(prefix string) Option {
	return func(p *Plugin) {
		p.keywordPrefix = prefix
	}
}

// New constructs the plugin. With no options the allowlist is empty and the
// plugin is a no-op — call WithEnabledDomains to migrate specific domains.
//
// Panics if WithKeywordPrefix was passed an invalid prefix.
func New(opts ...Option) *Plugin {
	p := &Plugin{
		migratedImpls: map[string]string{},
		keywordPrefix: DefaultKeywordPrefix,
	}
	for _, opt := range opts {
		opt(p)
	}
	if err := validateKeywordPrefix(p.keywordPrefix); err != nil {
		panic(fmt.Sprintf("domainresolver: %v", err))
	}

	return p
}

// validateKeywordPrefix rejects empty/non-identifier prefixes early so a bad
// configuration crashes loudly at New() instead of producing illegal package
// names deep inside codegen.
func validateKeywordPrefix(prefix string) error {
	if prefix == "" {
		return fmt.Errorf("WithKeywordPrefix: prefix must not be empty")
	}
	for i, r := range prefix {
		isLower := r >= 'a' && r <= 'z'
		isDigit := r >= '0' && r <= '9'
		if i == 0 && !isLower {
			return fmt.Errorf("WithKeywordPrefix: prefix must start with a lowercase letter, got %q", prefix)
		}
		if !isLower && !isDigit {
			return fmt.Errorf("WithKeywordPrefix: prefix must contain only lowercase letters and digits, got %q", prefix)
		}
	}

	return nil
}

// migratedImplKey is the lookup key for stashed prevImpl bodies.
func migratedImplKey(objectName, goFieldName string) string {
	return objectName + "." + goFieldName
}

func (p *Plugin) Name() string { return "domain-resolver" }

// domainFor returns the domain of a schema file, filtered through the
// allowlist. Domains not in the allowlist (or any domain when the allowlist
// is empty) are returned as the zero Domain — equivalent to a root field
// with no domain.
func (p *Plugin) domainFor(schemaPath string) Domain {
	d := extractDomain(schemaPath, p.keywordPrefix)
	if d.IsZero() {
		return Domain{}
	}
	if !p.enabledSet[d.Raw] {
		return Domain{}
	}

	return d
}

var _ interface {
	Implement(string, *codegen.Field) string
	GenerateCode(*codegen.Data) error
} = (*Plugin)(nil)
