package domainresolver

import (
	"fmt"
	"os"
	"path/filepath"
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

// GenerateCode generates files in domain packages.
// Called by api.Generate() AFTER resolvergen.
func (p *Plugin) GenerateCode(data *codegen.Data) error {
	resolverDir := data.Config.Resolver.Dir()

	type domainData struct {
		fields  []*domainField
		objects []*codegen.Object
	}

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

	return nil
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
