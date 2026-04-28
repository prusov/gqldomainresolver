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

// fileGroup holds content for a single .go file in a domain package.
type fileGroup struct {
	fields  []*domainField
	objects []*codegen.Object
}

// domainData holds collected fields and non-root objects for a single domain.
type domainData struct {
	fields  []*domainField
	objects []*codegen.Object
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

		for schemaBase, fg := range groupBySchemaFile(d.fields, d.objects) {
			outFile := filepath.Join(domainDir, schemaBase+".go")
			if err := renderDomainFile(data, domain, outFile, fg, rw); err != nil {
				return fmt.Errorf("render %s: %w", outFile, err)
			}
		}
	}

	if err := p.renderDomainConstructors(data, domains); err != nil {
		return fmt.Errorf("render domain constructors: %w", err)
	}

	return nil
}

// renderDomainConstructors emits a single file in the root resolver package containing:
//
//	func (r *Resolver) Todo() generated.TodoResolver { return &todos.TodoResolver{} }
//
// One constructor per non-root domain object. Replaces the per-method delegation
// layer that resolvergen would otherwise generate on the (now-removed) todoResolver
// stub struct.
func (p *Plugin) renderDomainConstructors(data *codegen.Data, domains map[string]*domainData) error {
	type ctor struct {
		TypeName string // "Todo"
		Domain   string // "todos"
	}

	var ctors []ctor
	domainSet := map[string]bool{}
	for domain, d := range domains {
		for _, obj := range d.objects {
			ctors = append(ctors, ctor{TypeName: obj.Name, Domain: domain})
			domainSet[domain] = true
		}
	}
	if len(ctors) == 0 {
		return nil
	}
	sort.Slice(ctors, func(i, j int) bool { return ctors[i].TypeName < ctors[j].TypeName })

	domainImports := make([]string, 0, len(domainSet))
	for d := range domainSet {
		domainImports = append(domainImports, p.importPrefix+d)
	}
	sort.Strings(domainImports)

	build := struct {
		GeneratedPkg  string
		DomainImports []string
		Ctors         []ctor
	}{
		GeneratedPkg:  data.Config.Exec.ImportPath(),
		DomainImports: domainImports,
		Ctors:         ctors,
	}

	outFile := filepath.Join(data.Config.Resolver.Dir(), "domain_resolvers.go")

	return renderConstructorsFile(data, outFile, build)
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
