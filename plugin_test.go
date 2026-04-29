package domainresolver

import "testing"

func TestDomainFor_EmptyAllowlist(t *testing.T) {
	p := New()
	if got := p.domainFor(todoSchema); got != "" {
		t.Errorf("empty allowlist must return \"\", got %q", got)
	}
	if got := p.domainFor("/abs/graph/schema/schema.graphqls"); got != "" {
		t.Errorf("root file must return \"\", got %q", got)
	}
}

func TestDomainFor_Allowlisted(t *testing.T) {
	p := New(WithEnabledDomains("todos"))
	if got := p.domainFor(todoSchema); got != "todos" {
		t.Errorf("todos must be returned, got %q", got)
	}
	if got := p.domainFor(userSchema); got != "" {
		t.Errorf("non-allowlisted users must return \"\", got %q", got)
	}
}

func TestDomainFor_InvalidNamesIgnored(t *testing.T) {
	// "schema", dash, Go keyword — all dropped silently.
	p := New(WithEnabledDomains("schema", "with-dash", "import"))
	if len(p.enabledSet) != 0 {
		t.Errorf("invalid names must not enter enabledSet, got %v", p.enabledSet)
	}
	if got := p.domainFor(todoSchema); got != "" {
		t.Errorf("expected \"\", got %q", got)
	}
}

func TestDomainFor_UnknownDomainTolerated(t *testing.T) {
	// Unknown name doesn't error and doesn't enable anything else.
	p := New(WithEnabledDomains("nonexistent"))
	if got := p.domainFor(todoSchema); got != "" {
		t.Errorf("expected \"\", got %q", got)
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
