package gqldomainresolver

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
// By default — New() with no options — every domain in the schema is
// migrated. This is the greenfield configuration.
//
// To restrict migration to a subset of domains, pass WithEnabledDomains.
// This is used during incremental migration of an existing project, where
// domains are moved to their own packages one at a time. An explicit empty
// allowlist (WithEnabledDomains() with no arguments) makes the plugin a
// no-op — useful as a bootstrap step in a migration so the plugin can be
// wired up without producing any diff.
//
// Plugin is not safe for concurrent use. Construct one instance per
// api.Generate() call; gqlgen runs single-threaded today, but Implement()
// mutates internal state and parallel codegen would race.
type Plugin struct {
	// enabledSet is keyed by the *raw* schema directory name — that is what
	// the user sees on disk, so it is the user-facing key for the allowlist.
	//
	// nil means "WithEnabledDomains was never called" → all domains are
	// migrated (greenfield default). A non-nil empty map means "explicit
	// empty allowlist" → no domain is migrated (migration bootstrap).
	enabledSet map[string]bool

	// keywordPrefix disambiguates domain names that would otherwise be
	// invalid Go identifiers (Go keywords, "schema", names starting with
	// a digit). Defaults to DefaultKeywordPrefix.
	keywordPrefix string

	// migratedImpls captures the previous resolver body of each field whose
	// domain becomes enabled, so first-time migrations can rehydrate the
	// domain-package method from prevImpl after resolvergen has overwritten
	// the original file. See Implement().
	migratedImpls map[string]string
}

// Option configures a Plugin at construction time.
type Option func(*Plugin)

// WithEnabledDomains restricts migration to the listed schema-directory names.
// Names are matched against the *raw* directory name (e.g. "business-process",
// not the normalized "businessprocess"). Duplicates are deduplicated; empty
// entries are dropped.
//
// Calling this option — even with no arguments — switches the plugin out of
// the default "all domains" mode into an explicit allowlist. WithEnabledDomains()
// with no arguments produces an empty allowlist, which makes the plugin a
// no-op (useful as a bootstrap step in an incremental migration).
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
// digits afterwards. Invalid prefixes cause New() to return an error.
func WithKeywordPrefix(prefix string) Option {
	return func(p *Plugin) {
		p.keywordPrefix = prefix
	}
}

// New constructs the plugin. With no options every domain is migrated
// (greenfield default). Pass WithEnabledDomains to restrict migration to a
// specific subset — typically during incremental migration of an existing
// project.
//
// Returns an error if WithKeywordPrefix was passed an invalid prefix.
func New(opts ...Option) (*Plugin, error) {
	p := &Plugin{
		migratedImpls: map[string]string{},
		keywordPrefix: DefaultKeywordPrefix,
	}
	for _, opt := range opts {
		opt(p)
	}
	if err := validateKeywordPrefix(p.keywordPrefix); err != nil {
		return nil, fmt.Errorf("gqldomainresolver: %w", err)
	}

	return p, nil
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

func migratedImplKey(objectName, goFieldName string) string {
	return objectName + "." + goFieldName
}

func (p *Plugin) Name() string { return "gqldomainresolver" }

// domainFor returns the domain of a schema file, filtered through the
// allowlist. When WithEnabledDomains was never called (enabledSet == nil),
// every domain is migrated. When it was called — even with no arguments —
// only the listed names are migrated; everything else returns the zero
// domain (equivalent to a root field with no domain).
func (p *Plugin) domainFor(schemaPath string) domain {
	d := extractDomain(schemaPath, p.keywordPrefix)
	if d.IsZero() {
		return domain{}
	}
	if p.enabledSet != nil && !p.enabledSet[d.Raw] {
		return domain{}
	}

	return d
}

var _ interface {
	Implement(string, *codegen.Field) string
	GenerateCode(*codegen.Data) error
} = (*Plugin)(nil)
