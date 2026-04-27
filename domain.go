package domainresolver

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

// extractDomain returns the domain name from a .graphqls file path,
// or an empty string for root-level schema files.
//
// Examples:
//
//	/abs/path/graph/schema/todos/todo.graphqls        → "todos"
//	/abs/path/graph/schema/schema.graphqls            → "" (root)
//	/abs/path/graph/schema/common/directives.graphqls → "common"
//	/abs/path/graph/schema/business-process/x.graphqls → "" (dash invalid)
//	/abs/path/graph/schema/import/x.graphqls          → "" (reserved word)
func extractDomain(schemaPath string) string {
	parts := strings.Split(filepath.ToSlash(schemaPath), "/")
	if len(parts) < 2 {
		return ""
	}
	parent := parts[len(parts)-2]
	if !isValidDomain(parent) {
		return ""
	}
	return parent
}

func isValidDomain(name string) bool {
	if name == "" || name == "schema" {
		return false
	}
	// Dash makes the name an invalid Go identifier.
	if strings.Contains(name, "-") {
		return false
	}
	return !goKeywords[name]
}
