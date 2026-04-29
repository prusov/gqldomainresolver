package domainresolver

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/99designs/gqlgen/codegen"
	"github.com/99designs/gqlgen/codegen/templates"
)

// domainField is a resolver field bound to its parent object.
type domainField struct {
	Object *codegen.Object
	Field  *codegen.Field
}

// domainData holds collected fields and non-root objects for a single domain.
type domainData struct {
	fields  []*domainField
	objects []*codegen.Object
}

// hasRootField reports whether fields contains a resolver field on the
// root type named rootName ("Mutation" / "Query" / "Subscription").
func hasRootField(fields []*domainField, rootName string) bool {
	for _, f := range fields {
		if f.Object.Root && f.Object.Name == rootName {
			return true
		}
	}

	return false
}

// GenerateCode generates files in domain packages.
// Called by api.Generate() AFTER resolvergen.
func (p *Plugin) GenerateCode(data *codegen.Data) error {
	resolverDir := data.Config.Resolver.Dir()

	domains := map[string]*domainData{}
	migratedBases := map[string]bool{}

	for _, obj := range data.Objects {
		for _, f := range obj.Fields {
			if !f.IsResolver {
				continue
			}
			// Group by the field's schema file, not the object's — needed for
			// root types (Mutation/Query) whose fields span multiple schema files.
			domain := p.domainFor(f.Position.Src.Name)
			if domain == "" {
				continue
			}
			migratedBases[schemaBase(f.Position.Src.Name)] = true
			if domains[domain] == nil {
				domains[domain] = &domainData{}
			}
			d := domains[domain]
			d.fields = append(d.fields, &domainField{Object: obj, Field: f})

			// Only non-root types need a generated struct (e.g. TodoResolver).
			if !obj.Root {
				found := false
				for _, o := range d.objects {
					if o.Name == obj.Name {
						found = true
						break
					}
				}
				if !found {
					d.objects = append(d.objects, obj)
				}
			}
		}
	}

	for domain, d := range domains {
		domainDir := filepath.Join(resolverDir, domain)
		if err := os.MkdirAll(domainDir, 0755); err != nil {
			return fmt.Errorf("mkdir %s: %w", domainDir, err)
		}

		// On first run rw is nil (no package yet); renderDomainFile handles that.
		rw, _ := newASTRewriter(domainDir)

		groups := groupBySchemaFile(d.fields, d.objects)

		// Pick the alphabetically first base name with root fields of each kind
		// so the type is declared exactly once per domain package.
		bases := make([]string, 0, len(groups))
		for b := range groups {
			bases = append(bases, b)
		}
		sort.Strings(bases)

		mutationOwner := ""
		queryOwner := ""
		subscriptionOwner := ""
		for _, b := range bases {
			fg := groups[b]
			if mutationOwner == "" && hasRootField(fg.fields, "Mutation") {
				mutationOwner = b
			}
			if queryOwner == "" && hasRootField(fg.fields, "Query") {
				queryOwner = b
			}
			if subscriptionOwner == "" && hasRootField(fg.fields, "Subscription") {
				subscriptionOwner = b
			}
		}

		for _, base := range bases {
			fg := groups[base]
			outFile := filepath.Join(domainDir, base+".go")

			build := buildDomainFile(fg)
			build.EmitMutationStruct = base == mutationOwner
			build.EmitQueryStruct = base == queryOwner
			build.EmitSubscriptionStruct = base == subscriptionOwner

			if err := renderDomainFile(data, domain, outFile, build, rw, p.migratedImpls); err != nil {
				return fmt.Errorf("render %s: %w", outFile, err)
			}
		}
	}

	if err := p.renderDomainConstructors(data, domains); err != nil {
		return fmt.Errorf("render domain constructors: %w", err)
	}

	// Every resolver field in a schema file shares the same domain (extracted
	// from the file path). When that domain is enabled, Implement() returns ""
	// for all of its fields, so the safety-net template emits zero methods —
	// leaving resolvergen's <base>.resolvers.go with header+imports only.
	// We know these basenames from the loop above, so we can delete the files
	// directly without scanning the resolver dir.
	for base := range migratedBases {
		path := filepath.Join(resolverDir, base+".resolvers.go")
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %s: %w", path, err)
		}
	}

	return nil
}

// schemaBase returns the file name of a schema path without the .graphqls
// suffix (e.g. "todos/todo.graphqls" → "todo"). Matches the basename used by
// gqlgen's resolvergen when naming output files.
func schemaBase(schemaPath string) string {
	return strings.TrimSuffix(filepath.Base(filepath.ToSlash(schemaPath)), ".graphqls")
}

// buildDomainFile classifies the fields in a fileGroup into the categories
// that domainTemplate consumes (mutation/query/subscription methods, object
// methods, and non-root object structs).
func buildDomainFile(fg *fileGroup) *domainFileBuild {
	build := &domainFileBuild{Objects: fg.objects}

	for _, df := range fg.fields {
		m := &domainMethodBuild{Object: df.Object, Field: df.Field}
		switch {
		case df.Object.Root && df.Object.Name == "Mutation":
			build.MutationMethods = append(build.MutationMethods, m)
		case df.Object.Root && df.Object.Name == "Query":
			build.QueryMethods = append(build.QueryMethods, m)
		case df.Object.Root && df.Object.Name == "Subscription":
			build.SubscriptionMethods = append(build.SubscriptionMethods, m)
		case !df.Object.Root:
			build.ObjectMethods = append(build.ObjectMethods, m)
		}
	}

	return build
}

// ctor is a per-object constructor for a migrated domain — emits
// `(r *Resolver) Todo() generated.TodoResolver { return &todos.TodoResolver{} }`.
type ctor struct {
	TypeName string // "Todo"
	Domain   string // "todos"
}

// embed is a per-domain root struct value-embedded into one of the
// kind-specific Domain{Mutation,Query,Subscription}Resolvers structs.
type embed struct {
	TypeName string // "MixinTodosMutation"
	Domain   string // "todos"
}

// rootCtor is a per-object constructor for an UN-migrated domain — emits a
// constructor returning a root-package wrapper struct (e.g. todoResolver),
// matching what default gqlgen produces. Keeps gradual-migration projects
// compiling: hand-written field-resolver methods on the wrapper survive
// regen until the domain is added to the allowlist.
type rootCtor struct {
	TypeName  string // "Todo"
	WrapperLc string // "todoResolver"
}

// collectRootCtors selects non-root objects with resolver fields whose
// domain is NOT in the allowlist.
func (p *Plugin) collectRootCtors(objects []*codegen.Object) []rootCtor {
	var out []rootCtor
	for _, obj := range objects {
		if obj.Root || !obj.HasResolvers() {
			continue
		}
		if p.domainFor(obj.Position.Src.Name) != "" {
			continue
		}
		out = append(out, rootCtor{
			TypeName:  obj.Name,
			WrapperLc: templates.LcFirst(obj.Name) + "Resolver",
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TypeName < out[j].TypeName })

	return out
}

// renderDomainConstructors emits the constructor files in the root resolver
// package, one file per root kind plus an object-constructor file:
//
//   - domain_mutation_resolvers.go     — DomainMutationResolvers,
//     Mutation() ctor, mutationResolver wrapper.
//   - domain_query_resolvers.go        — DomainQueryResolvers,
//     Query() ctor, queryResolver wrapper.
//   - domain_subscription_resolvers.go — DomainSubscriptionResolvers,
//     Subscription() ctor, subscriptionResolver wrapper.
//   - domain_object_resolvers.go       — per-object constructors like
//     (r *Resolver) Todo() returning &todos.TodoResolver{}, plus
//     root-package wrappers for non-migrated domains (see rootCtor).
//
// Splitting per root kind avoids ambiguous selectors when a field name is
// reused across Query and Subscription. The import path for domain packages
// is derived from data.Config.Resolver.ImportPath() — the plugin is
// module-agnostic.
func (p *Plugin) renderDomainConstructors(data *codegen.Data, domains map[string]*domainData) error {
	var ctors []ctor
	var mutationEmbeds, queryEmbeds, subscriptionEmbeds []embed
	objectDomains := map[string]bool{}

	for domain, d := range domains {
		prefix := domainStructPrefix(domain)

		if hasRootField(d.fields, "Mutation") {
			mutationEmbeds = append(mutationEmbeds, embed{TypeName: prefix + "Mutation", Domain: domain})
		}
		if hasRootField(d.fields, "Query") {
			queryEmbeds = append(queryEmbeds, embed{TypeName: prefix + "Query", Domain: domain})
		}
		if hasRootField(d.fields, "Subscription") {
			subscriptionEmbeds = append(subscriptionEmbeds, embed{TypeName: prefix + "Subscription", Domain: domain})
		}

		for _, obj := range d.objects {
			ctors = append(ctors, ctor{TypeName: obj.Name, Domain: domain})
			objectDomains[domain] = true
		}
	}

	rootCtors := p.collectRootCtors(data.Objects)

	// Root-type emission is driven by the schema, not by which domains are
	// migrated. The custom resolver template emits methods on
	// mutationResolver/queryResolver/subscriptionResolver whenever those root
	// types exist in the schema, so the wrapper structs and constructors must
	// always be present too — otherwise an empty allowlist (or partial
	// migration that skips a root) leaves the package un-compilable.
	hasMutation := data.MutationRoot != nil
	hasQuery := data.QueryRoot != nil
	hasSubscription := data.SubscriptionRoot != nil

	resolverDir := data.Config.Resolver.Dir()
	resolverImport := data.Config.Resolver.ImportPath()
	generatedPkg := data.Config.Exec.ImportPath()

	kinds := []struct {
		hasRoot     bool
		kind        string // "Mutation" / "Query" / "Subscription"
		structName  string // "DomainMutationResolvers"
		wrapperName string // "mutationResolver"
		fileName    string // "domain_mutation_resolvers.go"
		embeds      []embed
	}{
		{hasMutation, "Mutation", "DomainMutationResolvers", "mutationResolver", "domain_mutation_resolvers.go", mutationEmbeds},
		{hasQuery, "Query", "DomainQueryResolvers", "queryResolver", "domain_query_resolvers.go", queryEmbeds},
		{hasSubscription, "Subscription", "DomainSubscriptionResolvers", "subscriptionResolver", "domain_subscription_resolvers.go", subscriptionEmbeds},
	}

	for _, k := range kinds {
		outFile := filepath.Join(resolverDir, k.fileName)

		if !k.hasRoot {
			// Root kind absent from the schema — make sure no stale file
			// from a previous schema lingers.
			if err := os.Remove(outFile); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove %s: %w", outFile, err)
			}
			continue
		}

		sortEmbeds(k.embeds)
		domainImports := embedDomainImports(k.embeds, resolverImport)

		build := struct {
			GeneratedPkg  string
			DomainImports []string
			Embeds        []embed
			StructName    string
			WrapperName   string
			Kind          string
		}{
			GeneratedPkg:  generatedPkg,
			DomainImports: domainImports,
			Embeds:        k.embeds,
			StructName:    k.structName,
			WrapperName:   k.wrapperName,
			Kind:          k.kind,
		}
		if err := renderRootKindFile(data, outFile, build); err != nil {
			return err
		}
	}

	// Older plugin versions wrote everything to a single domain_resolvers.go.
	// If a project upgrades, the stale file would conflict with the new
	// per-kind files (duplicate declarations). Remove it on every run — cheap,
	// idempotent, no-op once the migration is done.
	staleAggregateFile := filepath.Join(resolverDir, "domain_resolvers.go")
	if err := os.Remove(staleAggregateFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", staleAggregateFile, err)
	}

	objectFile := filepath.Join(resolverDir, "domain_object_resolvers.go")
	if len(ctors) == 0 && len(rootCtors) == 0 {
		if err := os.Remove(objectFile); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %s: %w", objectFile, err)
		}

		return nil
	}

	sort.Slice(ctors, func(i, j int) bool { return ctors[i].TypeName < ctors[j].TypeName })

	domainImports := make([]string, 0, len(objectDomains))
	for d := range objectDomains {
		domainImports = append(domainImports, resolverImport+"/"+d)
	}
	sort.Strings(domainImports)

	objBuild := struct {
		GeneratedPkg  string
		DomainImports []string
		Ctors         []ctor
		RootCtors     []rootCtor
	}{
		GeneratedPkg:  generatedPkg,
		DomainImports: domainImports,
		Ctors:         ctors,
		RootCtors:     rootCtors,
	}

	return renderObjectCtorsFile(data, objectFile, objBuild)
}

func sortEmbeds(es []embed) {
	sort.Slice(es, func(i, j int) bool {
		if es[i].Domain != es[j].Domain {
			return es[i].Domain < es[j].Domain
		}

		return es[i].TypeName < es[j].TypeName
	})
}

// embedDomainImports returns the sorted, deduplicated set of domain package
// import paths referenced by the given embeds.
func embedDomainImports(es []embed, resolverImport string) []string {
	set := map[string]bool{}
	for _, e := range es {
		set[e.Domain] = true
	}
	out := make([]string, 0, len(set))
	for d := range set {
		out = append(out, resolverImport+"/"+d)
	}
	sort.Strings(out)

	return out
}

// fileGroup holds content for a single .go file in a domain package.
type fileGroup struct {
	fields  []*domainField
	objects []*codegen.Object
}

// groupBySchemaFile groups fields and objects by schema file base name.
// "todos/todo.graphqls" → "todo" → fileGroup{...}
func groupBySchemaFile(fields []*domainField, objects []*codegen.Object) map[string]*fileGroup {
	groups := map[string]*fileGroup{}

	getGroup := func(schemaPath string) *fileGroup {
		base := schemaBase(schemaPath)
		if groups[base] == nil {
			groups[base] = &fileGroup{}
		}

		return groups[base]
	}

	for _, df := range fields {
		g := getGroup(df.Field.Position.Src.Name)
		g.fields = append(g.fields, df)
	}

	for _, obj := range objects {
		g := getGroup(obj.Position.Src.Name)
		// An object may appear multiple times via different fields — dedupe.
		found := false
		for _, o := range g.objects {
			if o.Name == obj.Name {
				found = true
				break
			}
		}
		if !found {
			g.objects = append(g.objects, obj)
		}
	}

	return groups
}
