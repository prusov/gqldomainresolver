package gqldomainresolver

import (
	"path/filepath"
	"strings"
)

// goKeywords holds reserved Go words that cannot be used as package names.
var goKeywords = map[string]bool{
	"break": true, "case": true, "chan": true, "const": true, "continue": true,
	"default": true, "defer": true, "else": true, "fallthrough": true, "for": true,
	"func": true, "go": true, "goto": true, "if": true, "import": true,
	"interface": true, "map": true, "package": true, "range": true, "return": true,
	"select": true, "struct": true, "switch": true, "type": true, "var": true,
}

// domain represents a schema domain extracted from a .graphqls file path.
//
// Raw is the schema directory name as it appears on disk (e.g.
// "business-process"). It is used to identify the source for the allowlist
// and for diagnostics.
//
// Pkg is the normalized form (e.g. "businessprocess") used everywhere on the
// Go side: output package directory, package name, import path leaf and
// struct prefix.
//
// An empty Pkg means "skip" — the field falls back to the safety-net resolver
// in the root package.
type domain struct {
	Raw string
	Pkg string
}

func (d domain) IsZero() bool { return d.Pkg == "" }

// extractDomain returns the domain of a .graphqls file path,
// or the zero domain for root-level schema files.
//
// Examples (default keyword prefix "gql"):
//
//	/abs/path/graph/schema/todos/todo.graphqls           → {Raw:"todos", Pkg:"todos"}
//	/abs/path/graph/schema/schema.graphqls               → {} (root)
//	/abs/path/graph/schema/common/directives.graphqls    → {Raw:"common", Pkg:"common"}
//	/abs/path/graph/schema/business-process/x.graphqls   → {Raw:"business-process", Pkg:"businessprocess"}
//	/abs/path/graph/schema/order_flow/x.graphqls         → {Raw:"order_flow", Pkg:"orderflow"}
//	/abs/path/graph/schema/import/x.graphqls             → {Raw:"import", Pkg:"gqlimport"}
//	/abs/path/graph/schema/2fa/x.graphqls                → {Raw:"2fa", Pkg:"gql2fa"}
func extractDomain(schemaPath, keywordPrefix string) domain {
	parts := strings.Split(filepath.ToSlash(schemaPath), "/")
	if len(parts) < 2 {
		return domain{}
	}
	parent := parts[len(parts)-2]
	// "schema" is reserved as the "root, no domain" marker — a literal directory
	// of that name still maps to the root convention rather than being normalized.
	if parent == "" || parent == "schema" {
		return domain{}
	}
	pkg := normalizeDomain(parent, keywordPrefix)
	if pkg == "" {
		return domain{}
	}

	return domain{Raw: parent, Pkg: pkg}
}

// normalizeDomain converts a schema directory name into a valid Go package
// identifier using the strip-only lowercase rule:
//
//  1. lowercase the whole string;
//  2. strip dashes and underscores entirely (no separators in the result);
//  3. if the result is a Go keyword, equals "schema", or starts with a digit,
//     prepend keywordPrefix;
//  4. if the result is empty (e.g. dir was "-" or "_"), return "".
//
// The keywordPrefix is itself assumed to be a valid identifier prefix (the
// plugin validates this in New()).
func normalizeDomain(name, keywordPrefix string) string {
	lower := strings.ToLower(name)
	stripped := strings.ReplaceAll(strings.ReplaceAll(lower, "-", ""), "_", "")
	if stripped == "" {
		return ""
	}
	if needsKeywordPrefix(stripped) {
		return keywordPrefix + stripped
	}

	return stripped
}

func needsKeywordPrefix(s string) bool {
	if s == "" {
		return false
	}
	if goKeywords[s] || s == "schema" {
		return true
	}
	c := s[0]

	return c >= '0' && c <= '9'
}
