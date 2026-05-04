package gqldomainresolver_test

import (
	"fmt"

	"github.com/prusov/gqldomainresolver"
)

// Minimal construction. With no options the allowlist is empty and the plugin
// is a no-op — it must be combined with WithEnabledDomains to migrate a
// domain.
func ExampleNew() {
	plugin, err := gqldomainresolver.New()
	if err != nil {
		panic(err)
	}
	fmt.Println(plugin.Name())
	// Output: gqldomainresolver
}

// Enable migration for one or more domains by their raw schema-directory name.
// Names that are not present in the schema are silently tolerated, so an
// allowlist can be edited ahead of the corresponding schema directory.
func ExampleWithEnabledDomains() {
	plugin, err := gqldomainresolver.New(
		gqldomainresolver.WithEnabledDomains("todos", "users", "business-process"),
	)
	if err != nil {
		panic(err)
	}
	_ = plugin
}

// Override the prefix used when a domain name collides with a Go keyword,
// equals "schema", or starts with a digit. The default is "gql"
// (DefaultKeywordPrefix), so the directory "import" produces package
// "gqlimport". Passing "dom" produces "domimport" instead.
func ExampleWithKeywordPrefix() {
	plugin, err := gqldomainresolver.New(
		gqldomainresolver.WithKeywordPrefix("dom"),
		gqldomainresolver.WithEnabledDomains("import"),
	)
	if err != nil {
		panic(err)
	}
	_ = plugin
}
