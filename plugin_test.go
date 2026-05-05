package gqldomainresolver

import (
	"os"
	"strings"
	"testing"

	"github.com/99designs/gqlgen/codegen"
	"github.com/99designs/gqlgen/codegen/config"
)

func dataWithObjects(objs ...*codegen.Object) *codegen.Data {
	return &codegen.Data{Objects: objs}
}

func TestValidateAllowlist_NilAllowlistAlwaysOK(t *testing.T) {
	t.Parallel()
	p := mustNew(t) // greenfield: enabledSet == nil
	if err := p.validateAllowlist(dataWithObjects(objWithResolverField("Todo", false, todoSchema))); err != nil {
		t.Errorf("expected nil error for greenfield default, got %v", err)
	}
}

func TestValidateAllowlist_EmptyAllowlistOK(t *testing.T) {
	t.Parallel()
	// Migration-bootstrap: explicit empty allowlist is a no-op, must not error.
	p := mustNew(t, WithEnabledDomains())
	if err := p.validateAllowlist(dataWithObjects(objWithResolverField("Todo", false, todoSchema))); err != nil {
		t.Errorf("expected nil error for empty allowlist, got %v", err)
	}
}

func TestValidateAllowlist_KnownDomainsOK(t *testing.T) {
	t.Parallel()
	p := mustNew(t, WithEnabledDomains("todos", "users"))
	data := dataWithObjects(
		objWithResolverField("Todo", false, todoSchema),
		objWithResolverField("User", false, userSchema),
	)
	if err := p.validateAllowlist(data); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestValidateAllowlist_UnknownNamesAggregated(t *testing.T) {
	t.Parallel()
	// Three unknowns + one valid — only the unknowns must surface, sorted.
	p := mustNew(t, WithEnabledDomains("todos", "Todos", "user", "billing"))
	data := dataWithObjects(
		objWithResolverField("Todo", false, todoSchema),
		objWithResolverField("User", false, userSchema),
	)
	err := p.validateAllowlist(data)
	if err == nil {
		t.Fatal("expected error for unknown allowlist entries")
	}
	msg := err.Error()
	for _, want := range []string{`"Todos"`, `"billing"`, `"user"`} {
		if !strings.Contains(msg, want) {
			t.Errorf("missing %s in %q", want, msg)
		}
	}
	if strings.Contains(msg, `"todos"`) {
		t.Errorf("valid name leaked: %q", msg)
	}
	// Sorted: "Todos" < "billing" < "user" by Go's strings.Sort (ASCII upper < lower).
	if strings.Index(msg, `"Todos"`) > strings.Index(msg, `"billing"`) ||
		strings.Index(msg, `"billing"`) > strings.Index(msg, `"user"`) {
		t.Errorf("unknown names not sorted: %q", msg)
	}
}

func TestValidateAllowlist_UnknownExcludeAggregated(t *testing.T) {
	t.Parallel()
	p := mustNew(t, WithExcludedDomains("billing", "Users"))
	data := dataWithObjects(
		objWithResolverField("Todo", false, todoSchema),
		objWithResolverField("User", false, userSchema),
	)
	err := p.validateAllowlist(data)
	if err == nil {
		t.Fatal("expected error for unknown exclude entries")
	}
	msg := err.Error()
	if !strings.Contains(msg, "WithExcludedDomains") {
		t.Errorf("error must reference WithExcludedDomains: %q", msg)
	}
	for _, want := range []string{`"Users"`, `"billing"`} {
		if !strings.Contains(msg, want) {
			t.Errorf("missing %s in %q", want, msg)
		}
	}
}

func TestValidateAllowlist_UnknownEnabledAndExcluded(t *testing.T) {
	t.Parallel()
	// Both options have unknown entries — enabled is checked first and
	// surfaces; the exclude error only matters once the allowlist is clean.
	p := mustNew(t, WithEnabledDomains("typo"), WithExcludedDomains("billing"))
	data := dataWithObjects(objWithResolverField("Todo", false, todoSchema))
	err := p.validateAllowlist(data)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "WithEnabledDomains") {
		t.Errorf("expected WithEnabledDomains error first, got %q", err)
	}
}

func TestDomainFor_Excluded(t *testing.T) {
	t.Parallel()
	// Greenfield + exclude: everything migrates except listed names.
	p := mustNew(t, WithExcludedDomains("users"))
	if got := p.domainFor(todoSchema); got != (domain{Raw: "todos", Pkg: "todos"}) {
		t.Errorf("todos must still migrate, got %+v", got)
	}
	if got := p.domainFor(userSchema); !got.IsZero() {
		t.Errorf("excluded users must return zero domain, got %+v", got)
	}
}

func TestDomainFor_EnabledMinusExcluded(t *testing.T) {
	t.Parallel()
	// Allowlist intersected with exclude: enabled = {todos, users},
	// excluded = {users} → only todos migrates.
	p := mustNew(t,
		WithEnabledDomains("todos", "users"),
		WithExcludedDomains("users"),
	)
	if got := p.domainFor(todoSchema); got != (domain{Raw: "todos", Pkg: "todos"}) {
		t.Errorf("todos must migrate, got %+v", got)
	}
	if got := p.domainFor(userSchema); !got.IsZero() {
		t.Errorf("excluded users must return zero domain, got %+v", got)
	}
}

func TestDomainFor_ExcludeMatchesRawName(t *testing.T) {
	t.Parallel()
	// Symmetric to TestDomainFor_AllowlistMatchesRawName — exclude uses
	// the raw on-disk name, normalized form does not match.
	p := mustNew(t, WithExcludedDomains("business-process"))
	got := p.domainFor("/abs/graph/schema/business-process/x.graphqls")
	if !got.IsZero() {
		t.Errorf("expected zero domain when raw name matches exclude, got %+v", got)
	}
	p2 := mustNew(t, WithExcludedDomains("businessprocess"))
	got2 := p2.domainFor("/abs/graph/schema/business-process/x.graphqls")
	if got2 != (domain{Raw: "business-process", Pkg: "businessprocess"}) {
		t.Errorf("normalized name in exclude must not match raw dir, got %+v", got2)
	}
}

func TestDomainFor_EmptyExcludeIsNoOp(t *testing.T) {
	t.Parallel()
	p := mustNew(t, WithExcludedDomains())
	if got := p.domainFor(todoSchema); got != (domain{Raw: "todos", Pkg: "todos"}) {
		t.Errorf("empty exclude must not block migration, got %+v", got)
	}
}

func TestWithExcludedDomains_KeepsRawNamesAndDedup(t *testing.T) {
	t.Parallel()
	p := mustNew(t, WithExcludedDomains("todos", "todos", "with-dash", ""))
	if len(p.excludedSet) != 2 {
		t.Errorf("expected 2 unique entries, got %d: %v", len(p.excludedSet), p.excludedSet)
	}
	if !p.excludedSet["todos"] || !p.excludedSet["with-dash"] {
		t.Errorf("missing expected entries: %v", p.excludedSet)
	}
	if _, ok := p.excludedSet[""]; ok {
		t.Errorf("empty entry must not be added: %v", p.excludedSet)
	}
}

func TestWithExcludedDomains_MultipleCallsMerge(t *testing.T) {
	t.Parallel()
	p := mustNew(t, WithExcludedDomains("todos"), WithExcludedDomains("users"))
	if !p.excludedSet["todos"] || !p.excludedSet["users"] {
		t.Errorf("expected both options to merge, got %v", p.excludedSet)
	}
}

func TestDomainFor_NoOptionsMigratesEverything(t *testing.T) {
	t.Parallel()
	// Greenfield default: New() with no options migrates every domain.
	p := mustNew(t)
	if got := p.domainFor(todoSchema); got != (domain{Raw: "todos", Pkg: "todos"}) {
		t.Errorf("default config must migrate todos, got %+v", got)
	}
	if got := p.domainFor(userSchema); got != (domain{Raw: "users", Pkg: "users"}) {
		t.Errorf("default config must migrate users, got %+v", got)
	}
	// Schema files with no domain (root-level) still return zero.
	if got := p.domainFor("/abs/graph/schema/schema.graphqls"); !got.IsZero() {
		t.Errorf("root file must return zero domain, got %+v", got)
	}
}

func TestDomainFor_ExplicitEmptyAllowlistIsNoOp(t *testing.T) {
	t.Parallel()
	// WithEnabledDomains() with no arguments → explicit empty allowlist →
	// nothing is migrated. This is the migration-bootstrap configuration.
	p := mustNew(t, WithEnabledDomains())
	if got := p.domainFor(todoSchema); !got.IsZero() {
		t.Errorf("explicit empty allowlist must return zero domain, got %+v", got)
	}
	if got := p.domainFor(userSchema); !got.IsZero() {
		t.Errorf("explicit empty allowlist must return zero domain, got %+v", got)
	}
}

func TestDomainFor_Allowlisted(t *testing.T) {
	t.Parallel()
	p := mustNew(t, WithEnabledDomains("todos"))
	got := p.domainFor(todoSchema)
	if got != (domain{Raw: "todos", Pkg: "todos"}) {
		t.Errorf("todos must be returned, got %+v", got)
	}
	if got := p.domainFor(userSchema); !got.IsZero() {
		t.Errorf("non-allowlisted users must return zero domain, got %+v", got)
	}
}

func TestDomainFor_AllowlistMatchesRawName(t *testing.T) {
	t.Parallel()
	// User adds the schema-dir name verbatim ("business-process"), not the
	// normalized package name. domainFor must match on Raw and return the
	// normalized Pkg.
	p := mustNew(t, WithEnabledDomains("business-process"))
	got := p.domainFor("/abs/graph/schema/business-process/x.graphqls")
	if got != (domain{Raw: "business-process", Pkg: "businessprocess"}) {
		t.Errorf("got %+v", got)
	}
	// Normalized form in the allowlist does NOT match a different raw dir.
	p2 := mustNew(t, WithEnabledDomains("businessprocess"))
	if got := p2.domainFor("/abs/graph/schema/business-process/x.graphqls"); !got.IsZero() {
		t.Errorf("expected zero domain when allowlist uses normalized name, got %+v", got)
	}
}

func TestWithEnabledDomains_KeepsRawNames(t *testing.T) {
	t.Parallel()
	p := mustNew(t, WithEnabledDomains("schema", "with-dash", "import", ""))
	if !p.enabledSet["schema"] || !p.enabledSet["with-dash"] || !p.enabledSet["import"] {
		t.Errorf("expected all non-empty raw names to be kept, got %v", p.enabledSet)
	}
	if _, ok := p.enabledSet[""]; ok {
		t.Errorf("empty entry must not be added: %v", p.enabledSet)
	}
}

func TestDomainFor_KeywordDirNormalises(t *testing.T) {
	t.Parallel()
	p := mustNew(t, WithEnabledDomains("import"))
	got := p.domainFor("/abs/graph/schema/import/x.graphqls")
	if got != (domain{Raw: "import", Pkg: "gqlimport"}) {
		t.Errorf("got %+v", got)
	}
}

func TestDomainFor_UnknownDomainTolerated(t *testing.T) {
	t.Parallel()
	p := mustNew(t, WithEnabledDomains("nonexistent"))
	if got := p.domainFor(todoSchema); !got.IsZero() {
		t.Errorf("expected zero domain, got %+v", got)
	}
}

func TestWithEnabledDomains_Deduplicates(t *testing.T) {
	t.Parallel()
	p := mustNew(t, WithEnabledDomains("todos", "todos", "users"))
	if len(p.enabledSet) != 2 {
		t.Errorf("expected 2 unique entries, got %d: %v", len(p.enabledSet), p.enabledSet)
	}
	if !p.enabledSet["todos"] || !p.enabledSet["users"] {
		t.Errorf("missing expected entries: %v", p.enabledSet)
	}
}

func TestWithEnabledDomains_MultipleCallsMerge(t *testing.T) {
	t.Parallel()
	p := mustNew(t, WithEnabledDomains("todos"), WithEnabledDomains("users"))
	if !p.enabledSet["todos"] || !p.enabledSet["users"] {
		t.Errorf("expected both options to merge, got %v", p.enabledSet)
	}
}

func TestKeywordPrefix(t *testing.T) {
	t.Parallel()
	t.Run("default", func(t *testing.T) {
		t.Parallel()
		p := mustNew(t)
		if p.keywordPrefix != DefaultKeywordPrefix {
			t.Errorf("expected default prefix %q, got %q", DefaultKeywordPrefix, p.keywordPrefix)
		}
	})
	t.Run("override", func(t *testing.T) {
		t.Parallel()
		p := mustNew(t, WithKeywordPrefix("dom"), WithEnabledDomains("import"))
		got := p.domainFor("/abs/graph/schema/import/x.graphqls")
		if got != (domain{Raw: "import", Pkg: "domimport"}) {
			t.Errorf("got %+v", got)
		}
	})
}

func TestNew_InvalidPrefixReturnsError(t *testing.T) {
	t.Parallel()
	cases := []string{"", "Foo", "1foo", "foo-bar", "foo_bar", "FOO"}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			t.Parallel()
			p, err := New(WithKeywordPrefix(c))
			if err == nil {
				t.Errorf("expected error for prefix %q", c)
			}
			if p != nil {
				t.Errorf("expected nil plugin for prefix %q, got %+v", c, p)
			}
		})
	}
}

func TestMutateConfig_InjectsEmbeddedTemplate(t *testing.T) {
	t.Parallel()
	p := mustNew(t)
	cfg := &config.Config{}
	if err := p.MutateConfig(cfg); err != nil {
		t.Fatalf("MutateConfig: %v", err)
	}
	if cfg.Resolver.ResolverTemplate == "" {
		t.Fatal("expected ResolverTemplate to be set")
	}
	got, err := os.ReadFile(cfg.Resolver.ResolverTemplate)
	if err != nil {
		t.Fatalf("read injected template: %v", err)
	}
	if string(got) != resolverTemplate {
		t.Errorf("injected template content does not match embedded resolverTemplate")
	}
}

func TestMutateConfig_RespectsExistingTemplate(t *testing.T) {
	t.Parallel()
	p := mustNew(t)
	const custom = "custom/path/resolver.gotpl"
	cfg := &config.Config{Resolver: config.ResolverConfig{ResolverTemplate: custom}}
	if err := p.MutateConfig(cfg); err != nil {
		t.Fatalf("MutateConfig: %v", err)
	}
	if cfg.Resolver.ResolverTemplate != custom {
		t.Errorf("expected ResolverTemplate to be left untouched, got %q", cfg.Resolver.ResolverTemplate)
	}
}

func TestPlugin_Name(t *testing.T) {
	t.Parallel()
	if got := mustNew(t).Name(); got != "gqldomainresolver" {
		t.Errorf("Name() = %q, want %q", got, "gqldomainresolver")
	}
}
