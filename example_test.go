package gqldomainresolver_test

import (
	"fmt"

	"github.com/prusov/gqldomainresolver"
)

// Greenfield default. With no options every domain in the schema is migrated
// to its own Tier-2 package — typically what a new project wants.
func ExampleNew() {
	plugin, err := gqldomainresolver.New()
	if err != nil {
		panic(err)
	}
	fmt.Println(plugin.Name())
	// Output: gqldomainresolver
}

// Restrict migration to a subset of domains. Used during incremental
// migration of an existing project, where domains move to Tier-2 packages
// one at a time. Names are matched against the *raw* schema-directory name.
// Names that are not present in the schema are silently tolerated.
func ExampleWithEnabledDomains() {
	plugin, err := gqldomainresolver.New(
		gqldomainresolver.WithEnabledDomains("todos", "users", "business-process"),
	)
	if err != nil {
		panic(err)
	}
	_ = plugin
}

// Migration bootstrap: WithEnabledDomains() with no arguments produces an
// explicit empty allowlist, so the plugin is a no-op. This lets a project
// wire the plugin into its build before migrating any domain — the first
// PR introduces zero diff in the existing resolvers.
func ExampleWithEnabledDomains_bootstrap() {
	plugin, err := gqldomainresolver.New(
		gqldomainresolver.WithEnabledDomains(),
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
