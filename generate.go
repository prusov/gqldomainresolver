package domainresolver

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/99designs/gqlgen/codegen"
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

// hasRootMutation reports whether the domain has any root Mutation fields.
func (d *domainData) hasRootMutation() bool {
	for _, f := range d.fields {
		if f.Object.Root && f.Object.Name == "Mutation" {
			return true
		}
	}
	return false
}

// hasRootQuery reports whether the domain has any root Query fields.
func (d *domainData) hasRootQuery() bool {
	for _, f := range d.fields {
		if f.Object.Root && f.Object.Name == "Query" {
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
			domain := extractDomain(f.Position.Src.Name)
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

		// newASTRewriter returns nil if the package doesn't exist yet — intentionally ignored.
		// On first run rw will be nil; renderDomainFile handles that case.
		rw, _ := newASTRewriter(domainDir)

		groups := groupBySchemaFile(d.fields, d.objects)

		// Determine which file owns the Mutation/Query struct decls. Pick the
		// alphabetically first base name that has root fields of that kind so
		// the type is declared exactly once per domain package.
		bases := make([]string, 0, len(groups))
		for b := range groups {
			bases = append(bases, b)
		}
		sort.Strings(bases)

		mutationOwner := ""
		queryOwner := ""
		for _, b := range bases {
			fg := groups[b]
			if mutationOwner == "" && fg.hasRootMutation() {
				mutationOwner = b
			}
			if queryOwner == "" && fg.hasRootQuery() {
				queryOwner = b
			}
		}

		for _, base := range bases {
			fg := groups[base]
			outFile := filepath.Join(domainDir, base+".go")

			build := buildDomainFile(fg)
			build.EmitMutationStruct = (base == mutationOwner)
			build.EmitQueryStruct = (base == queryOwner)

			if err := renderDomainFile(data, domain, outFile, build, rw); err != nil {
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
// that domainTemplate consumes (mutation methods, query methods, object methods,
// non-root object structs).
func buildDomainFile(fg *fileGroup) *domainFileBuild {
	build := &domainFileBuild{Objects: fg.objects}

	for _, df := range fg.fields {
		m := &domainMethodBuild{Object: df.Object, Field: df.Field}
		switch {
		case df.Object.Root && df.Object.Name == "Mutation":
			build.MutationMethods = append(build.MutationMethods, m)
		case df.Object.Root && df.Object.Name == "Query":
			build.QueryMethods = append(build.QueryMethods, m)
		case !df.Object.Root:
			build.ObjectMethods = append(build.ObjectMethods, m)
		}
	}

	return build
}

// renderDomainConstructors emits domain_resolvers.go in the root resolver package.
// It owns:
//   - the DomainResolvers struct value-embedding every <Domain>Mutation/Query
//     so root-field methods are promoted up to mutationResolver/queryResolver.
//   - Mutation()/Query() constructors and the mutationResolver/queryResolver wrappers.
//   - per-object constructors like (r *Resolver) Todo() returning &todos.TodoResolver{}.
func (p *Plugin) renderDomainConstructors(data *codegen.Data, domains map[string]*domainData) error {
	type ctor struct {
		TypeName string // "Todo"
		Domain   string // "todos"
	}
	type embed struct {
		TypeName string // "TodosMutation"
		Domain   string // "todos"
	}

	var ctors []ctor
	var embeds []embed
	domainSet := map[string]bool{}

	for domain, d := range domains {
		prefix := domainStructPrefix(domain)

		if d.hasRootMutation() {
			embeds = append(embeds, embed{TypeName: prefix + "Mutation", Domain: domain})
			domainSet[domain] = true
		}
		if d.hasRootQuery() {
			embeds = append(embeds, embed{TypeName: prefix + "Query", Domain: domain})
			domainSet[domain] = true
		}

		for _, obj := range d.objects {
			ctors = append(ctors, ctor{TypeName: obj.Name, Domain: domain})
			domainSet[domain] = true
		}
	}

	if len(ctors) == 0 && len(embeds) == 0 {
		return nil
	}

	sort.Slice(ctors, func(i, j int) bool { return ctors[i].TypeName < ctors[j].TypeName })
	sort.Slice(embeds, func(i, j int) bool {
		if embeds[i].Domain != embeds[j].Domain {
			return embeds[i].Domain < embeds[j].Domain
		}
		return embeds[i].TypeName < embeds[j].TypeName
	})

	domainImports := make([]string, 0, len(domainSet))
	for d := range domainSet {
		domainImports = append(domainImports, p.importPrefix+d)
	}
	sort.Strings(domainImports)

	build := struct {
		GeneratedPkg  string
		DomainImports []string
		Ctors         []ctor
		Embeds        []embed
	}{
		GeneratedPkg:  data.Config.Exec.ImportPath(),
		DomainImports: domainImports,
		Ctors:         ctors,
		Embeds:        embeds,
	}

	outFile := filepath.Join(data.Config.Resolver.Dir(), "domain_resolvers.go")

	return renderConstructorsFile(data, outFile, build)
}

// fileGroup holds content for a single .go file in a domain package.
type fileGroup struct {
	fields  []*domainField
	objects []*codegen.Object
}

func (fg *fileGroup) hasRootMutation() bool {
	for _, f := range fg.fields {
		if f.Object.Root && f.Object.Name == "Mutation" {
			return true
		}
	}
	return false
}

func (fg *fileGroup) hasRootQuery() bool {
	for _, f := range fg.fields {
		if f.Object.Root && f.Object.Name == "Query" {
			return true
		}
	}
	return false
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
		// Deduplicate: an object may appear multiple times via different fields.
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
