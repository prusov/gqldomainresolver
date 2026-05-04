package domainresolver

import "testing"

func TestDomainFor_EmptyAllowlist(t *testing.T) {
	t.Parallel()
	p := mustNew(t)
	if got := p.domainFor(todoSchema); !got.IsZero() {
		t.Errorf("empty allowlist must return zero domain, got %+v", got)
	}
	if got := p.domainFor("/abs/graph/schema/schema.graphqls"); !got.IsZero() {
		t.Errorf("root file must return zero domain, got %+v", got)
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

func TestPlugin_Name(t *testing.T) {
	t.Parallel()
	if got := mustNew(t).Name(); got != "domainresolver" {
		t.Errorf("Name() = %q, want %q", got, "domainresolver")
	}
}
