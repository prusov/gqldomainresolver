package domainresolver

import "testing"

func TestDomainFor_EmptyAllowlist(t *testing.T) {
	p := New()
	if got := p.domainFor(todoSchema); !got.IsZero() {
		t.Errorf("empty allowlist must return zero Domain, got %+v", got)
	}
	if got := p.domainFor("/abs/graph/schema/schema.graphqls"); !got.IsZero() {
		t.Errorf("root file must return zero Domain, got %+v", got)
	}
}

func TestDomainFor_Allowlisted(t *testing.T) {
	p := New(WithEnabledDomains("todos"))
	got := p.domainFor(todoSchema)
	if got != (Domain{Raw: "todos", Pkg: "todos"}) {
		t.Errorf("todos must be returned, got %+v", got)
	}
	if got := p.domainFor(userSchema); !got.IsZero() {
		t.Errorf("non-allowlisted users must return zero Domain, got %+v", got)
	}
}

func TestDomainFor_AllowlistMatchesRawName(t *testing.T) {
	// User adds the schema-dir name verbatim ("business-process"), not the
	// normalized package name. domainFor must match on Raw and return the
	// normalized Pkg.
	p := New(WithEnabledDomains("business-process"))
	got := p.domainFor("/abs/graph/schema/business-process/x.graphqls")
	if got != (Domain{Raw: "business-process", Pkg: "businessprocess"}) {
		t.Errorf("got %+v", got)
	}
	// Normalized form in the allowlist does NOT match a different raw dir.
	p2 := New(WithEnabledDomains("businessprocess"))
	if got := p2.domainFor("/abs/graph/schema/business-process/x.graphqls"); !got.IsZero() {
		t.Errorf("expected zero Domain when allowlist uses normalized name, got %+v", got)
	}
}

func TestWithEnabledDomains_KeepsRawNames(t *testing.T) {
	// Names that previously failed isValidDomain (Go keywords, dashes,
	// "schema") are now kept — normalization handles them. Empty strings
	// are still dropped.
	p := New(WithEnabledDomains("schema", "with-dash", "import", ""))
	if !p.enabledSet["schema"] || !p.enabledSet["with-dash"] || !p.enabledSet["import"] {
		t.Errorf("expected all non-empty raw names to be kept, got %v", p.enabledSet)
	}
	if _, ok := p.enabledSet[""]; ok {
		t.Errorf("empty entry must not be added: %v", p.enabledSet)
	}
}

func TestDomainFor_KeywordDirNormalises(t *testing.T) {
	p := New(WithEnabledDomains("import"))
	got := p.domainFor("/abs/graph/schema/import/x.graphqls")
	if got != (Domain{Raw: "import", Pkg: "gqlimport"}) {
		t.Errorf("got %+v", got)
	}
}

func TestDomainFor_UnknownDomainTolerated(t *testing.T) {
	// Unknown name doesn't error and doesn't enable anything else.
	p := New(WithEnabledDomains("nonexistent"))
	if got := p.domainFor(todoSchema); !got.IsZero() {
		t.Errorf("expected zero Domain, got %+v", got)
	}
}

func TestWithEnabledDomains_Deduplicates(t *testing.T) {
	p := New(WithEnabledDomains("todos", "todos", "users"))
	if len(p.enabledSet) != 2 {
		t.Errorf("expected 2 unique entries, got %d: %v", len(p.enabledSet), p.enabledSet)
	}
	if !p.enabledSet["todos"] || !p.enabledSet["users"] {
		t.Errorf("missing expected entries: %v", p.enabledSet)
	}
}

func TestWithEnabledDomains_MultipleCallsMerge(t *testing.T) {
	p := New(WithEnabledDomains("todos"), WithEnabledDomains("users"))
	if !p.enabledSet["todos"] || !p.enabledSet["users"] {
		t.Errorf("expected both options to merge, got %v", p.enabledSet)
	}
}

func TestWithKeywordPrefix_OverridesDefault(t *testing.T) {
	p := New(WithKeywordPrefix("dom"), WithEnabledDomains("import"))
	got := p.domainFor("/abs/graph/schema/import/x.graphqls")
	if got != (Domain{Raw: "import", Pkg: "domimport"}) {
		t.Errorf("got %+v", got)
	}
}

func TestNew_InvalidPrefixPanics(t *testing.T) {
	cases := []string{"", "Foo", "1foo", "foo-bar", "foo_bar", "FOO"}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("expected panic for prefix %q", c)
				}
			}()
			_ = New(WithKeywordPrefix(c))
		})
	}
}

func TestNew_DefaultPrefix(t *testing.T) {
	p := New()
	if p.keywordPrefix != DefaultKeywordPrefix {
		t.Errorf("expected default prefix %q, got %q", DefaultKeywordPrefix, p.keywordPrefix)
	}
}
