// Package gqldomainresolver is a gqlgen plugin that splits the generated resolver
// package into per-domain Go packages.
//
// The standard gqlgen layout puts every resolver method in a single
// graph/resolver package, which forces every file to import
// graph/generated. In large schemas this collapses the build cache: any edit
// to a resolver invalidates the whole generated artifact (often hundreds of
// MB) and rebuilds slow to a crawl. gqldomainresolver keeps the original
// generated package intact for wiring and moves the resolver bodies out into
// per-domain packages that have no dependency on graph/generated. The
// per-domain packages satisfy the gqlgen interfaces by Go duck typing
// (matching method names and signatures), and the root resolver wires them
// together via Go method promotion.
//
// # Domain extraction
//
// A domain is the parent directory name of a .graphqls schema file, e.g.
//
//	graph/schema/todos/todo.graphqls           → domain "todos"
//	graph/schema/business-process/x.graphqls   → domain "business-process"
//	graph/schema/schema.graphqls               → no domain (stays in root)
//
// The directory name is normalized to a Go package identifier using the
// strip-only lowercase rule (see normalizeDomain). Names that collide with a
// Go keyword, equal "schema", or start with a digit get a configurable
// prefix prepended (default "gql", override with [WithKeywordPrefix]).
//
// # Opt-in migration
//
// The plugin is opt-in per domain via [WithEnabledDomains]. With an empty
// allowlist the plugin is a no-op — adding it to a project introduces no
// diff until a specific domain is enabled. This enables incremental
// migration of large projects, one domain at a time.
//
// # Wiring
//
// Construct the plugin and pass it to api.Generate alongside the standard
// gqlgen plugins. The plugin must run after resolvergen because it relies on
// resolvergen's prevImpl handling for first-time migrations.
//
//	plugin, err := gqldomainresolver.New(gqldomainresolver.WithEnabledDomains("todos"))
//	if err != nil {
//	    log.Fatal(err)
//	}
//	if err := api.Generate(cfg, api.AddPlugin(plugin)); err != nil {
//	    log.Fatal(err)
//	}
//
// gqlgen.yml must point resolver_template at the safety-net template shipped
// alongside this plugin so non-migrated fields still get a panic stub.
//
// See the package README for a full integration walkthrough.
package gqldomainresolver
