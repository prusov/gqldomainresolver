package gqldomainresolver

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
// raw remembers the schema-directory name as it appears on disk; it is used
// for diagnostics (e.g. collision errors) but never for path/identifier
// generation — that uses the map key (the normalized Pkg).
type domainData struct {
	raw     string
	fields  []*domainField
	objects []*codegen.Object
}

func hasRootField(fields []*domainField, rootName string) bool {
	for _, f := range fields {
		if f.Object.Root && f.Object.Name == rootName {
			return true
		}
	}

	return false
}

// appendUniqueObject appends obj unless an object with the same name already exists.
func appendUniqueObject(objs []*codegen.Object, obj *codegen.Object) []*codegen.Object {
	for _, o := range objs {
		if o.Name == obj.Name {
			return objs
		}
	}

	return append(objs, obj)
}

// GenerateCode generates files in domain packages.
// Called by api.Generate() AFTER resolvergen.
func (p *Plugin) GenerateCode(data *codegen.Data) error {
	domains, migratedBases, err := p.collectDomains(data)
	if err != nil {
		return err
	}

	resolverDir := data.Config.Resolver.Dir()

	for pkg, d := range domains {
		if err := p.renderDomain(data, resolverDir, pkg, d); err != nil {
			return err
		}
	}

	if err := p.renderDomainConstructors(data, domains); err != nil {
		return fmt.Errorf("render domain constructors: %w", err)
	}

	return cleanupMigratedFiles(resolverDir, migratedBases)
}

// collectDomains walks data.Objects, groups resolver fields by normalized
// package name, and detects collisions where two raw schema-dir names
// normalize to the same package. The returned migratedBases set lists
// schema basenames whose root-package <base>.resolvers.go file was emptied
// out by Implement() and should be deleted.
func (p *Plugin) collectDomains(data *codegen.Data) (map[string]*domainData, map[string]bool, error) {
	domains := map[string]*domainData{}
	migratedBases := map[string]bool{}

	for _, obj := range data.Objects {
		for _, f := range obj.Fields {
			if !f.IsResolver {
				continue
			}
			// Group by the field's schema file, not the object's — needed for
			// root types (Mutation/Query) whose fields span multiple schema files.
			d := p.domainFor(f.Position.Src.Name)
			if d.IsZero() {
				continue
			}
			migratedBases[schemaBase(f.Position.Src.Name)] = true

			existing, ok := domains[d.Pkg]
			if !ok {
				existing = &domainData{raw: d.Raw}
				domains[d.Pkg] = existing
			} else if existing.raw != d.Raw {
				return nil, nil, fmt.Errorf("gqldomainresolver: schema directories %q and %q both normalize to package %q — rename one or change the keyword prefix",
					existing.raw, d.Raw, d.Pkg)
			}
			existing.fields = append(existing.fields, &domainField{Object: obj, Field: f})

			// Only non-root types need a generated struct (e.g. TodoResolver).
			if !obj.Root {
				existing.objects = appendUniqueObject(existing.objects, obj)
			}
		}
	}

	return domains, migratedBases, nil
}

// renderDomain writes every per-file output under resolverDir/pkg, picking
// kind-owners so each Mixin<Domain><Kind> struct is declared exactly once.
func (p *Plugin) renderDomain(data *codegen.Data, resolverDir, pkg string, d *domainData) error {
	domainDir := filepath.Join(resolverDir, pkg)
	if err := os.MkdirAll(domainDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", domainDir, err)
	}

	// On first run rw is nil (no package yet); renderDomainFile handles that.
	rw, _ := newASTRewriter(domainDir)

	groups := groupBySchemaFile(d.fields, d.objects)

	bases := make([]string, 0, len(groups))
	for b := range groups {
		bases = append(bases, b)
	}
	sort.Strings(bases)

	// Pick the alphabetically first base name with root fields of each kind
	// so the type is declared exactly once per domain package.
	mutationOwner, queryOwner, subscriptionOwner := "", "", ""
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
		outFile := filepath.Join(domainDir, base+".resolvers.go")

		build := buildDomainFile(fg, d.raw)
		build.EmitMutationStruct = base == mutationOwner
		build.EmitQueryStruct = base == queryOwner
		build.EmitSubscriptionStruct = base == subscriptionOwner

		if err := renderDomainFile(data, pkg, d.raw, outFile, build, rw, p.migratedImpls); err != nil {
			return fmt.Errorf("render %s: %w", outFile, err)
		}
	}

	return nil
}

// cleanupMigratedFiles deletes the root-package <base>.resolvers.go files
// for schema basenames whose fields were fully handled by domain packages.
// Implement() returned "" for every such field, so the safety-net template
// produced a header-only file with no methods — safe to remove.
func cleanupMigratedFiles(resolverDir string, migratedBases map[string]bool) error {
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

// buildDomainFile collects the fields in a fileGroup into a single Methods
// list. The append order mirrors what gqlgen's resolvergen would produce:
// alphabetical by parent object name (data.Objects is alpha-sorted upstream),
// then schema-declaration order of fields.
func buildDomainFile(fg *fileGroup, rawDomain string) *domainFileBuild {
	build := &domainFileBuild{}

	for _, obj := range fg.objects {
		build.Objects = append(build.Objects, &domainObjectBuild{
			Object:   obj,
			TypeName: objectResolverName(rawDomain, obj.Name),
		})
	}

	for _, df := range fg.fields {
		build.Methods = append(build.Methods, &domainMethodBuild{Object: df.Object, Field: df.Field})
	}

	return build
}

// ctor is a per-object constructor for a migrated domain — emits
// `(r *Resolver) Todo() generated.TodoResolver { return &todos.TodoResolver{} }`.
//
// TypeName is the GQL type name (used both as the constructor method name and
// as the suffix on the generated.<TypeName>Resolver interface that gqlgen
// emits). StructName is the actual struct in the domain package, which may
// differ when the GQL type name shares the domain prefix and gets stripped
// (e.g. catalog/CatalogCategory → catalog.CategoryResolver).
type ctor struct {
	TypeName   string // "CatalogCategory" — drives method name + generated.<...>Resolver
	StructName string // "CategoryResolver" — actual struct in the domain package
	Pkg        string // "catalog" — domain package import-path leaf
}

// embed is a per-domain root struct value-embedded into one of the
// kind-specific Domain{Mutation,Query,Subscription}Resolvers structs.
type embed struct {
	TypeName string // "MixinTodosMutation"
	Pkg      string // "todos" — domain package import-path leaf
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
		if !p.domainFor(obj.Position.Src.Name).IsZero() {
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

// rootKindSpec describes one of the per-kind constructor files emitted in
// the root resolver package. There is exactly one spec per root kind
// (Mutation/Query/Subscription).
type rootKindSpec struct {
	hasRoot     bool
	kind        string // "Mutation" / "Query" / "Subscription"
	structName  string // "DomainMutationResolvers"
	wrapperName string // "mutationResolver"
	fileName    string // "mutation.resolvers.go"
	embeds      []embed
}

// renderDomainConstructors emits the constructor files in the root resolver
// package, one file per root kind plus an object-constructor file:
//
//   - mutation.resolvers.go     — DomainMutationResolvers,
//     Mutation() ctor, mutationResolver wrapper.
//   - query.resolvers.go        — DomainQueryResolvers,
//     Query() ctor, queryResolver wrapper.
//   - subscription.resolvers.go — DomainSubscriptionResolvers,
//     Subscription() ctor, subscriptionResolver wrapper.
//   - object.resolvers.go       — per-object constructors like
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

	for pkg, d := range domains {
		prefix := domainStructPrefix(d.raw)

		if hasRootField(d.fields, "Mutation") {
			mutationEmbeds = append(mutationEmbeds, embed{TypeName: prefix + "Mutation", Pkg: pkg})
		}
		if hasRootField(d.fields, "Query") {
			queryEmbeds = append(queryEmbeds, embed{TypeName: prefix + "Query", Pkg: pkg})
		}
		if hasRootField(d.fields, "Subscription") {
			subscriptionEmbeds = append(subscriptionEmbeds, embed{TypeName: prefix + "Subscription", Pkg: pkg})
		}

		for _, obj := range d.objects {
			ctors = append(ctors, ctor{
				TypeName:   obj.Name,
				StructName: objectResolverName(d.raw, obj.Name),
				Pkg:        pkg,
			})
			objectDomains[pkg] = true
		}
	}

	rootCtors := p.collectRootCtors(data.Objects)

	// Root-type emission is driven by the schema, not by which domains are
	// migrated. The custom resolver template emits methods on
	// mutationResolver/queryResolver/subscriptionResolver whenever those root
	// types exist in the schema, so the wrapper structs and constructors must
	// always be present too — otherwise an empty allowlist (or partial
	// migration that skips a root) leaves the package un-compilable.
	resolverDir := data.Config.Resolver.Dir()
	resolverImport := data.Config.Resolver.ImportPath()
	generatedPkg := data.Config.Exec.ImportPath()

	kinds := []rootKindSpec{
		{data.MutationRoot != nil, "Mutation", "DomainMutationResolvers", "mutationResolver", "mutation.resolvers.go", mutationEmbeds},
		{data.QueryRoot != nil, "Query", "DomainQueryResolvers", "queryResolver", "query.resolvers.go", queryEmbeds},
		{data.SubscriptionRoot != nil, "Subscription", "DomainSubscriptionResolvers", "subscriptionResolver", "subscription.resolvers.go", subscriptionEmbeds},
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

	objectFile := filepath.Join(resolverDir, "object.resolvers.go")

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
		if es[i].Pkg != es[j].Pkg {
			return es[i].Pkg < es[j].Pkg
		}

		return es[i].TypeName < es[j].TypeName
	})
}

// embedDomainImports returns the sorted, deduplicated set of domain package
// import paths referenced by the given embeds.
func embedDomainImports(es []embed, resolverImport string) []string {
	set := map[string]bool{}
	for _, e := range es {
		set[e.Pkg] = true
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
		g.objects = appendUniqueObject(g.objects, obj)
	}

	return groups
}
