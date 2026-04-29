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

	return nil
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

// embed is a per-domain root struct value-embedded into DomainResolvers.
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

// renderDomainConstructors emits domain_resolvers.go in the root resolver
// package. It owns:
//   - the DomainResolvers struct that value-embeds every <Domain>Mutation/Query
//     so root-field methods are promoted up to mutationResolver/queryResolver;
//   - Mutation()/Query()/Subscription() constructors and their wrapper structs;
//   - per-object constructors like (r *Resolver) Todo() returning &todos.TodoResolver{};
//   - root-package wrappers for non-migrated domains (see rootCtor).
//
// The import path for domain packages is derived from data.Config.Resolver.ImportPath()
// — the plugin is module-agnostic.
func (p *Plugin) renderDomainConstructors(data *codegen.Data, domains map[string]*domainData) error {
	var ctors []ctor
	var embeds []embed
	domainSet := map[string]bool{}

	for domain, d := range domains {
		prefix := domainStructPrefix(domain)

		if hasRootField(d.fields, "Mutation") {
			embeds = append(embeds, embed{TypeName: prefix + "Mutation", Domain: domain})
			domainSet[domain] = true
		}
		if hasRootField(d.fields, "Query") {
			embeds = append(embeds, embed{TypeName: prefix + "Query", Domain: domain})
			domainSet[domain] = true
		}
		if hasRootField(d.fields, "Subscription") {
			embeds = append(embeds, embed{TypeName: prefix + "Subscription", Domain: domain})
			domainSet[domain] = true
		}

		for _, obj := range d.objects {
			ctors = append(ctors, ctor{TypeName: obj.Name, Domain: domain})
			domainSet[domain] = true
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

	if !hasMutation && !hasQuery && !hasSubscription &&
		len(ctors) == 0 && len(embeds) == 0 && len(rootCtors) == 0 {
		return nil
	}

	sort.Slice(ctors, func(i, j int) bool { return ctors[i].TypeName < ctors[j].TypeName })
	sort.Slice(embeds, func(i, j int) bool {
		if embeds[i].Domain != embeds[j].Domain {
			return embeds[i].Domain < embeds[j].Domain
		}

		return embeds[i].TypeName < embeds[j].TypeName
	})

	resolverImport := data.Config.Resolver.ImportPath()
	domainImports := make([]string, 0, len(domainSet))
	for d := range domainSet {
		domainImports = append(domainImports, resolverImport+"/"+d)
	}
	sort.Strings(domainImports)

	build := struct {
		GeneratedPkg    string
		DomainImports   []string
		Ctors           []ctor
		Embeds          []embed
		RootCtors       []rootCtor
		HasMutation     bool
		HasQuery        bool
		HasSubscription bool
	}{
		GeneratedPkg:    data.Config.Exec.ImportPath(),
		DomainImports:   domainImports,
		Ctors:           ctors,
		Embeds:          embeds,
		RootCtors:       rootCtors,
		HasMutation:     hasMutation,
		HasQuery:        hasQuery,
		HasSubscription: hasSubscription,
	}

	outFile := filepath.Join(data.Config.Resolver.Dir(), "domain_resolvers.go")

	return renderConstructorsFile(data, outFile, build)
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
		base := strings.TrimSuffix(filepath.Base(filepath.ToSlash(schemaPath)), ".graphqls")
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
