package domainresolver

import (
	goast "go/ast"
	gparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/99designs/gqlgen/codegen"
	"github.com/99designs/gqlgen/codegen/templates"
)

// importSpec mirrors gqlgen's internal/rewrite.Import. We can't import that
// package (it's internal), so we replicate the minimal shape needed to
// re-register hand-written imports via templates.CurrentImports.Reserve.
type importSpec struct {
	Alias      string
	ImportPath string
}

// astRewriter reads existing method/function bodies from a Go package directory
// so that hand-written implementations survive re-generation.
type astRewriter struct {
	fset  *token.FileSet
	files map[string]*goast.File // absolute filename → parsed AST
	used  map[*goast.FuncDecl]bool
}

// newASTRewriter parses all .go files in dir using ParseFile (ParseDir is deprecated).
// Returns nil with an error if the directory does not exist or contains no Go files.
func newASTRewriter(dir string) (*astRewriter, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	fset := token.NewFileSet()
	files := make(map[string]*goast.File)

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		filename := filepath.Join(dir, entry.Name())
		f, parseErr := gparser.ParseFile(fset, filename, nil, 0)
		if parseErr != nil {
			continue // skip files that don't parse (e.g. partially written)
		}
		files[filename] = f
	}

	if len(files) == 0 {
		return nil, os.ErrNotExist
	}

	return &astRewriter{fset: fset, files: files, used: make(map[*goast.FuncDecl]bool)}, nil
}

// getMethodBody returns the body source of method methodName on receiver typeName.
// typeName must match the struct name (without pointer prefix).
func (rw *astRewriter) getMethodBody(typeName, methodName string) string {
	for filename, file := range rw.files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*goast.FuncDecl)
			if !ok || fn.Name.Name != methodName || fn.Recv == nil || len(fn.Recv.List) == 0 {
				continue
			}
			recv := fn.Recv.List[0].Type
			if star, ok := recv.(*goast.StarExpr); ok {
				recv = star.X
			}
			ident, ok := recv.(*goast.Ident)
			if !ok || ident.Name != typeName {
				continue
			}
			rw.used[fn] = true

			return rw.bodySource(filename, fn.Body)
		}
	}

	return ""
}

// existingImports returns the import set declared in outFile (matched by
// absolute path). Used to preserve hand-written imports that only appear
// inside copied method bodies — gqlgen's renderer only auto-resolves
// imports for types it knows from codegen.Data, so anything else (e.g.
// a third-party package referenced solely in a method body) would be
// dropped and the regenerated file would fail to compile.
func (rw *astRewriter) existingImports(outFile string) []importSpec {
	file := rw.fileFor(outFile)
	if file == nil {
		return nil
	}
	var imps []importSpec
	for _, i := range file.Imports {
		path, err := strconv.Unquote(i.Path.Value)
		if err != nil {
			continue
		}
		alias := ""
		if i.Name != nil {
			alias = i.Name.Name
		}
		imps = append(imps, importSpec{Alias: alias, ImportPath: path})
	}

	return imps
}

// fileFor returns the parsed AST for outFile.
func (rw *astRewriter) fileFor(outFile string) *goast.File {
	target, err := filepath.Abs(outFile)
	if err != nil {
		return nil
	}
	for filename, file := range rw.files {
		abs, err := filepath.Abs(filename)
		if err != nil || abs != target {
			continue
		}

		return file
	}

	return nil
}

// remainingFuncs returns the source of all FuncDecl declarations in outFile that
// were not consumed by getMethodBody. Used to preserve hand-written helpers.
func (rw *astRewriter) remainingFuncs(outFile string) string {
	file := rw.fileFor(outFile)
	if file == nil {
		return ""
	}
	src, err := os.ReadFile(outFile)
	if err != nil {
		return ""
	}
	var buf strings.Builder
	for _, decl := range file.Decls {
		fn, ok := decl.(*goast.FuncDecl)
		if !ok || rw.used[fn] {
			continue
		}
		start := rw.fset.Position(fn.Pos()).Offset
		end := rw.fset.Position(fn.End()).Offset
		if buf.Len() > 0 {
			buf.WriteString("\n\n")
		}
		buf.WriteString(string(src[start:end]))
	}

	return buf.String()
}

func (rw *astRewriter) bodySource(filename string, body *goast.BlockStmt) string {
	src, err := os.ReadFile(filename)
	if err != nil {
		return ""
	}
	startOff := rw.fset.Position(body.Lbrace).Offset + 1
	endOff := rw.fset.Position(body.Rbrace).Offset
	return strings.TrimSpace(string(src[startOff:endOff]))
}

type domainMethodBuild struct {
	Object         *codegen.Object
	Field          *codegen.Field
	ReceiverType   string // resolved receiver type name used in the func decl
	Implementation string // "" → generate panic stub
}

// domainObjectBuild pairs a non-root codegen.Object with the resolver struct
// name the domain package should emit for it (post strip-prefix).
type domainObjectBuild struct {
	Object   *codegen.Object
	TypeName string // e.g. "CategoryResolver"
}

type domainFileBuild struct {
	// Receiver type names referenced by methods in this file. Empty when the
	// file has no methods of that kind.
	MutationStructName     string
	QueryStructName        string
	SubscriptionStructName string

	// Methods are emitted in append order. The order mirrors what the original
	// gqlgen resolvergen would produce for a single .resolvers.go file:
	// alphabetical by parent object name (data.Objects is alpha-sorted), then
	// schema-declaration order of fields.
	Methods []*domainMethodBuild

	// EmitMutationStruct/EmitQueryStruct/EmitSubscriptionStruct gate the
	// `type Mixin<Domain><Kind> struct{}` declaration so each is emitted
	// exactly once across the domain package.
	EmitMutationStruct     bool
	EmitQueryStruct        bool
	EmitSubscriptionStruct bool

	// Non-root object struct declarations (e.g. `type TodoResolver struct{}`).
	Objects []*domainObjectBuild

	RemainingSource string // unknown functions from the previous file version; emitted as a commented-out warning block

	// imports captured from the previous version of the file. Re-registered via
	// templates.CurrentImports.Reserve in Imports() so hand-written imports that
	// only appear inside copied method bodies survive regeneration. Mirrors
	// gqlgen's resolvergen plugin (see internal/rewrite.ExistingImports +
	// File.Imports() in plugin/resolvergen/resolver.go).
	imports []importSpec
}

// Imports re-registers every import from the previous file version with the
// active templates.CurrentImports so the rendered output keeps them. Returns
// "" because it's invoked from the template purely for side-effect ({{ .Imports }}).
// Imports unused by the final code are pruned by gqlgen's post-render formatting.
func (b *domainFileBuild) Imports() string {
	for _, imp := range b.imports {
		if imp.Alias == "" {
			_, _ = templates.CurrentImports.Reserve(imp.ImportPath)
		} else {
			_, _ = templates.CurrentImports.Reserve(imp.ImportPath, imp.Alias)
		}
	}

	return ""
}

// domainStructPrefix turns the raw schema-directory name into the receiver
// struct prefix used by the domain package — e.g.
//
//	"todos"            → "MixinTodos"
//	"business-process" → "MixinBusinessProcess"
//	"order_flow"       → "MixinOrderFlow"
//
// The split honours dashes and underscores as word boundaries so the
// generated struct name reads naturally even when the package name itself
// is the strip-only lowercase form (`businessprocess`, `orderflow`).
//
// The "Mixin" lead-in keeps the struct name from starting with the package
// name (which would trigger revive's package-stutter rule, e.g.
// todos.TodosMutation).
func domainStructPrefix(rawDomain string) string {
	pascal := pascalDomain(rawDomain)
	if pascal == "" {
		return ""
	}

	return "Mixin" + pascal
}

// pascalDomain returns the PascalCase form of a raw schema-directory name,
// treating dashes and underscores as word boundaries: "business-process" →
// "BusinessProcess", "order_flow" → "OrderFlow", "todos" → "Todos".
func pascalDomain(rawDomain string) string {
	if rawDomain == "" {
		return ""
	}
	parts := strings.FieldsFunc(rawDomain, func(r rune) bool {
		return r == '-' || r == '_'
	})
	var b strings.Builder
	for _, p := range parts {
		p = strings.ToLower(p)
		if p == "" {
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]))
		b.WriteString(p[1:])
	}

	return b.String()
}

// stripDomainPrefix removes the PascalCase form of rawDomain from the front
// of typeName, when typeName starts with it and the remainder begins with an
// uppercase letter (so we never split a word in half). If typeName equals the
// prefix exactly, the remainder is empty — callers render that as a bare
// "Resolver" type. If no prefix matches, typeName is returned unchanged.
//
//	("catalog", "CatalogCategory") → "Category"
//	("import",  "ImportStatus")    → "Status"
//	("import",  "Entity")          → "Entity"   (no prefix match)
//	("tasks",   "Task")            → "Task"     (Task does not start with Tasks)
//	("catalog", "Catalog")         → ""         (exact match → bare Resolver)
func stripDomainPrefix(rawDomain, typeName string) string {
	prefix := pascalDomain(rawDomain)
	if prefix == "" || typeName == "" {
		return typeName
	}
	if !strings.HasPrefix(typeName, prefix) {
		return typeName
	}
	rest := typeName[len(prefix):]
	if rest == "" {
		return ""
	}
	c := rest[0]
	if c < 'A' || c > 'Z' {
		return typeName
	}

	return rest
}

// objectResolverName returns the generated struct name for a non-root resolver
// in a domain package — the stripped GQL type name plus "Resolver", or just
// "Resolver" when the GQL type name equals the domain prefix.
func objectResolverName(rawDomain, gqlObjectName string) string {
	stripped := stripDomainPrefix(rawDomain, gqlObjectName)

	return stripped + "Resolver"
}

// receiverFor returns the receiver type name used for the given method.
// Root methods land on the kind-specific Mixin struct; non-root methods land
// on the (possibly stripped) <Type>Resolver in the domain package.
func receiverFor(m *domainMethodBuild, rawDomain, mutationType, queryType, subscriptionType string) string {
	if m.Object.Root {
		switch m.Object.Name {
		case "Mutation":
			return mutationType
		case "Query":
			return queryType
		case "Subscription":
			return subscriptionType
		}
	}

	return objectResolverName(rawDomain, m.Object.Name)
}

// restoreImpls fills in the Implementation field of each method by reading
// the previous body from disk via the AST rewriter.
//
// If the AST rewriter is nil (first generation of the domain package) or the
// receiver isn't found in the domain dir (first-time migration — body still
// lived in the root package when resolvergen captured prevImpl), falls back to
// the plugin's migratedImpls cache populated during Implement().
func restoreImpls(methods []*domainMethodBuild, rw *astRewriter, migrated map[string]string) {
	for _, m := range methods {
		body := ""
		if rw != nil {
			body = strings.TrimSpace(rw.getMethodBody(m.ReceiverType, m.Field.GoFieldName))
		}
		if body == "" && migrated != nil && m.Object != nil {
			body = strings.TrimSpace(migrated[m.Object.Name+"."+m.Field.GoFieldName])
		}
		m.Implementation = body
	}
}

func renderDomainFile(
	data *codegen.Data,
	pkgName string, // normalized Go package name (Domain.Pkg)
	rawDomain string, // raw schema-dir name — drives the Mixin prefix
	outFile string,
	build *domainFileBuild,
	rw *astRewriter, // nil if package is being created for the first time
	migrated map[string]string, // captured prevImpls from Implement() — nil-safe
) error {
	prefix := domainStructPrefix(rawDomain)
	mutationType := prefix + "Mutation"
	queryType := prefix + "Query"
	subscriptionType := prefix + "Subscription"

	for _, m := range build.Methods {
		m.ReceiverType = receiverFor(m, rawDomain, mutationType, queryType, subscriptionType)
	}

	restoreImpls(build.Methods, rw, migrated)

	for _, m := range build.Methods {
		if !m.Object.Root {
			continue
		}
		switch m.Object.Name {
		case "Mutation":
			build.MutationStructName = mutationType
		case "Query":
			build.QueryStructName = queryType
		case "Subscription":
			build.SubscriptionStructName = subscriptionType
		}
	}

	if rw != nil {
		build.RemainingSource = rw.remainingFuncs(outFile)
		build.imports = rw.existingImports(outFile)
	}

	return templates.Render(templates.Options{
		PackageName: pkgName,
		FileNotice:  domainFileNotice,
		Filename:    outFile,
		Data:        build,
		Packages:    data.Config.Packages,
		Template:    domainTemplate,
	})
}

// renderRootKindFile renders one of the per-kind constructor files
// (mutation.resolvers.go / query.resolvers.go / subscription.resolvers.go).
func renderRootKindFile(data *codegen.Data, outFile string, build any) error {
	return templates.Render(templates.Options{
		PackageName: data.Config.Resolver.Package,
		Filename:    outFile,
		Data:        build,
		Packages:    data.Config.Packages,
		Template:    rootKindTemplate,
	})
}

// renderObjectCtorsFile renders object.resolvers.go — per-object constructors
// for migrated domains plus wrapper structs/constructors for non-migrated
// domains.
func renderObjectCtorsFile(data *codegen.Data, outFile string, build any) error {
	return templates.Render(templates.Options{
		PackageName: data.Config.Resolver.Package,
		Filename:    outFile,
		Data:        build,
		Packages:    data.Config.Packages,
		Template:    objectCtorsTemplate,
	})
}

// rootKindTemplate emits one root-kind file (Mutation, Query, or Subscription).
// Each file owns:
//   - the Domain<Kind>Resolvers struct that value-embeds every per-domain
//     Mixin<Domain><Kind> struct;
//   - the (r *Resolver) <Kind>() constructor that returns the wrapper;
//   - the <kind>Resolver wrapper struct that embeds Domain<Kind>Resolvers.
//
// `Resolver` is a NAMED field (not embedded) on purpose: gqlgen requires
// per-object constructors like (r *Resolver) Task() generated.TaskResolver
// to satisfy ResolverRoot, but those methods would shadow the deeper
// promoted Task(ctx, id) coming from tasks.TasksQuery if *Resolver were
// embedded — so queryResolver would fail to satisfy generated.QueryResolver.
// Promoting *Resolver into the wrappers also offers nothing beyond access
// to user state, which manually written resolvers (e.g., Hello, Welcome)
// can reach via `r.Resolver.<field>`.
//
// Splitting per root kind across files (and across structs) avoids
// ambiguous selectors when a field name is reused across Query and
// Subscription (e.g. `userNotifications` as both a query and a
// subscription) — each wrapper sees only methods of its own kind.
const rootKindTemplate = `
{{ reserveImport .GeneratedPkg }}
{{ range $imp := .DomainImports }}{{ reserveImport $imp }}{{ end }}

type {{ .StructName }} struct {
{{- range $e := .Embeds }}
	{{ $e.Domain }}.{{ $e.TypeName }}
{{- end }}
}

func (r *Resolver) {{ .Kind }}() generated.{{ .Kind }}Resolver {
	return &{{ .WrapperName }}{Resolver: r}
}

type {{ .WrapperName }} struct {
	Resolver *Resolver
	{{ .StructName }}
}
`

// objectCtorsTemplate emits object.resolvers.go — per-object constructors
// for migrated domains plus root-package wrappers for non-migrated domains.
//
// The non-migrated wrappers mirror what default gqlgen would emit, so a
// project can keep existing field resolvers in *.resolvers.go
// (e.g. (r *todoResolver) User) compiling before the domain is migrated.
const objectCtorsTemplate = `
{{ reserveImport .GeneratedPkg }}
{{ range $imp := .DomainImports }}{{ reserveImport $imp }}{{ end }}

{{ range $c := .Ctors }}
func (r *Resolver) {{ $c.TypeName }}() generated.{{ $c.TypeName }}Resolver {
	return &{{ $c.Domain }}.{{ $c.StructName }}{}
}
{{ end }}

{{ range $rc := .RootCtors }}
func (r *Resolver) {{ $rc.TypeName }}() generated.{{ $rc.TypeName }}Resolver {
	return &{{ $rc.WrapperLc }}{r}
}

type {{ $rc.WrapperLc }} struct{ *Resolver }
{{ end }}
`

// domainTemplate is the gotpl template for domain package files.
//
// Key design decisions:
//  1. NO import of graph/generated — interfaces are satisfied structurally (Go duck typing).
//  2. Root resolvers → methods on <Domain>Mutation / <Domain>Query structs (embedded
//     into DomainResolvers in the root package, promoted up through Resolver).
//  3. Field resolvers → methods on {TypeName}Resolver struct.
//  4. Existing implementations are restored via the AST rewriter — method
//     bodies are looked up by receiver type name on disk and re-emitted verbatim.
const domainFileNotice = `// This file will be automatically regenerated based on the schema, any resolver
// implementations
// will be copied through when generating and any unknown code will be moved to the end.
// Code generated by github.com/99designs/gqlgen`

const domainTemplate = `
{{- reserveImport "context" -}}
{{- reserveImport "fmt" -}}

{{ .Imports }}

{{ range $m := .Methods }}
{{- if $m.Object.Root -}}
{{- if eq $m.Object.Name "Mutation" }}
// {{ $m.Field.GoFieldName }} is the resolver for the {{ $m.Field.Name }} field.
func (m *{{ $.MutationStructName }}) {{ $m.Field.GoFieldName }}{{ $m.Field.ShortResolverDeclaration }} {
	{{- if $m.Implementation }}
	{{ $m.Implementation }}
	{{- else }}
	panic(fmt.Errorf("not implemented: Mutation.{{ $m.Field.GoFieldName }}"))
	{{- end }}
}
{{- else if eq $m.Object.Name "Query" }}
// {{ $m.Field.GoFieldName }} is the resolver for the {{ $m.Field.Name }} field.
func (q *{{ $.QueryStructName }}) {{ $m.Field.GoFieldName }}{{ $m.Field.ShortResolverDeclaration }} {
	{{- if $m.Implementation }}
	{{ $m.Implementation }}
	{{- else }}
	panic(fmt.Errorf("not implemented: Query.{{ $m.Field.GoFieldName }}"))
	{{- end }}
}
{{- else if eq $m.Object.Name "Subscription" }}
// {{ $m.Field.GoFieldName }} is the resolver for the {{ $m.Field.Name }} field.
func (s *{{ $.SubscriptionStructName }}) {{ $m.Field.GoFieldName }}{{ $m.Field.ShortResolverDeclaration }} {
	{{- if $m.Implementation }}
	{{ $m.Implementation }}
	{{- else }}
	panic(fmt.Errorf("not implemented: Subscription.{{ $m.Field.GoFieldName }}"))
	{{- end }}
}
{{- end }}
{{- else }}
// {{ $m.Field.GoFieldName }} is the resolver for the {{ $m.Field.Name }} field.
func (r *{{ $m.ReceiverType }}) {{ $m.Field.GoFieldName }}{{ $m.Field.ShortResolverDeclaration }} {
	{{- if $m.Implementation }}
	{{ $m.Implementation }}
	{{- else }}
	panic(fmt.Errorf("not implemented: {{ $m.Object.Name }}.{{ $m.Field.GoFieldName }}"))
	{{- end }}
}
{{- end }}
{{ end }}

{{ if .EmitMutationStruct }}
type {{ .MutationStructName }} struct{}
{{ end }}

{{ if .EmitQueryStruct }}
type {{ .QueryStructName }} struct{}
{{ end }}

{{ if .EmitSubscriptionStruct }}
type {{ .SubscriptionStructName }} struct{}
{{ end }}

{{ range $obj := .Objects }}
// {{ $obj.TypeName }} implements generated.{{ ucFirst $obj.Object.Name }}Resolver structurally (Go duck typing).
type {{ $obj.TypeName }} struct{}
{{ end }}

{{ if (ne .RemainingSource "") }}
// !!! WARNING !!!
// The code below was going to be deleted when updating resolvers. It has been copied here so you have
// one last chance to move it out of harms way if you want. There are two reasons this happens:
//  - When renaming or deleting a resolver the old code will be put in here. You can safely delete
//    it when you're done.
//  - You have helper methods in this file. Move them out to keep these domain files clean.
/*
{{ .RemainingSource }}
*/
{{ end }}
`
