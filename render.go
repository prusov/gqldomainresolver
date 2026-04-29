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

// Import mirrors gqlgen's internal/rewrite.Import. We can't import that
// package (it's internal), so we replicate the minimal shape needed to
// re-register hand-written imports via templates.CurrentImports.Reserve.
type Import struct {
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
func (rw *astRewriter) existingImports(outFile string) []Import {
	target, err := filepath.Abs(outFile)
	if err != nil {
		return nil
	}
	for filename, file := range rw.files {
		abs, err := filepath.Abs(filename)
		if err != nil || abs != target {
			continue
		}
		var imps []Import
		for _, i := range file.Imports {
			path, err := strconv.Unquote(i.Path.Value)
			if err != nil {
				continue
			}
			alias := ""
			if i.Name != nil {
				alias = i.Name.Name
			}
			imps = append(imps, Import{Alias: alias, ImportPath: path})
		}

		return imps
	}

	return nil
}

// remainingFuncs returns the source of all FuncDecl declarations in outFile that
// were not consumed by getMethodBody. Used to preserve hand-written helpers.
func (rw *astRewriter) remainingFuncs(outFile string) string {
	file, ok := rw.files[outFile]
	if !ok {
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
	Implementation string // "" → generate panic stub
}

type domainFileBuild struct {
	// Root-method emission.
	MutationStructName     string               // e.g., "TodosMutation". Empty when no mutation methods in this file.
	QueryStructName        string               // e.g., "TodosQuery". Empty when no query methods in this file.
	SubscriptionStructName string               // e.g., "TodosSubscription". Empty when no subscription methods in this file.
	MutationMethods        []*domainMethodBuild // methods on MutationStructName
	QueryMethods           []*domainMethodBuild // methods on QueryStructName
	SubscriptionMethods    []*domainMethodBuild // methods on SubscriptionStructName
	EmitMutationStruct     bool                 // declare `type <MutationStructName> struct{}` in this file
	EmitQueryStruct        bool                 // declare `type <QueryStructName> struct{}` in this file
	EmitSubscriptionStruct bool                 // declare `type <SubscriptionStructName> struct{}` in this file

	// Per-object (non-root) resolver emission — unchanged behavior.
	ObjectMethods []*domainMethodBuild
	Objects       []*codegen.Object

	RemainingSource string // unknown functions from the previous file version; emitted as a commented-out warning block

	// imports captured from the previous version of the file. Re-registered via
	// templates.CurrentImports.Reserve in Imports() so hand-written imports that
	// only appear inside copied method bodies survive regeneration. Mirrors
	// gqlgen's resolvergen plugin (see internal/rewrite.ExistingImports +
	// File.Imports() in plugin/resolvergen/resolver.go).
	imports []Import
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

// domainStructPrefix turns "todos" → "MixinTodos", used to derive the struct
// names MixinTodosMutation / MixinTodosQuery exposed by the domain package.
// The "Mixin" lead-in keeps the struct name from starting with the package
// name (which would trigger revive's package-stutter rule, e.g. todos.TodosMutation).
func domainStructPrefix(domain string) string {
	if domain == "" {
		return ""
	}
	return "Mixin" + strings.ToUpper(domain[:1]) + domain[1:]
}

// restoreImpls fills in the Implementation field of each method by reading
// the previous body from disk via the AST rewriter. receiverType returns the
// receiver type name to look up for a given method (the same name for all
// root methods, per-object for ObjectMethods).
//
// If the AST rewriter is nil (first generation of the domain package) or the
// receiver isn't found in the domain dir (first-time migration — body still
// lived in the root package when resolvergen captured prevImpl), falls back to
// the plugin's migratedImpls cache populated during Implement().
func restoreImpls(methods []*domainMethodBuild, rw *astRewriter, migrated map[string]string, receiverType func(*domainMethodBuild) string) {
	for _, m := range methods {
		body := ""
		if rw != nil {
			body = strings.TrimSpace(rw.getMethodBody(receiverType(m), m.Field.GoFieldName))
		}
		if body == "" && migrated != nil && m.Object != nil {
			body = strings.TrimSpace(migrated[m.Object.Name+"."+m.Field.GoFieldName])
		}
		m.Implementation = body
	}
}

func renderDomainFile(
	data *codegen.Data,
	pkgName string,
	outFile string,
	build *domainFileBuild,
	rw *astRewriter, // nil if package is being created for the first time
	migrated map[string]string, // captured prevImpls from Implement() — nil-safe
) error {
	prefix := domainStructPrefix(pkgName)
	mutationType := prefix + "Mutation"
	queryType := prefix + "Query"
	subscriptionType := prefix + "Subscription"

	restoreImpls(build.MutationMethods, rw, migrated, func(*domainMethodBuild) string { return mutationType })
	restoreImpls(build.QueryMethods, rw, migrated, func(*domainMethodBuild) string { return queryType })
	restoreImpls(build.SubscriptionMethods, rw, migrated, func(*domainMethodBuild) string { return subscriptionType })
	restoreImpls(build.ObjectMethods, rw, migrated, func(m *domainMethodBuild) string { return m.Object.Name + "Resolver" })

	if len(build.MutationMethods) > 0 {
		build.MutationStructName = mutationType
	}
	if len(build.QueryMethods) > 0 {
		build.QueryStructName = queryType
	}
	if len(build.SubscriptionMethods) > 0 {
		build.SubscriptionStructName = subscriptionType
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

// renderConstructorsFile renders the domain_resolvers.go file in the root resolver package.
// build is a struct passed through to constructorsTemplate (see renderDomainConstructors).
func renderConstructorsFile(data *codegen.Data, outFile string, build any) error {
	return templates.Render(templates.Options{
		PackageName: data.Config.Resolver.Package,
		Filename:    outFile,
		Data:        build,
		Packages:    data.Config.Packages,
		Template:    constructorsTemplate,
	})
}

// constructorsTemplate emits domain_resolvers.go in the root resolver package.
//
// Method-promotion layout:
//
//	mutationResolver { Resolver *Resolver; DomainResolvers }
//	queryResolver    { Resolver *Resolver; DomainResolvers }
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
// DomainResolvers is value-embedded twice — once in Resolver (so user code
// can call r.CreateTodo() directly) and once in each wrapper (so the methods
// reach the wrapper at depth 1 unambiguously). Both copies are zero-sized
// since each per-domain Mutation/Query struct is empty.
const constructorsTemplate = `
{{ reserveImport .GeneratedPkg }}
{{ range $imp := .DomainImports }}{{ reserveImport $imp }}{{ end }}

type DomainResolvers struct {
{{- range $e := .Embeds }}
	{{ $e.Domain }}.{{ $e.TypeName }}
{{- end }}
}

{{ if .HasMutation }}
func (r *Resolver) Mutation() generated.MutationResolver {
	return &mutationResolver{Resolver: r}
}

type mutationResolver struct {
	Resolver *Resolver
	DomainResolvers
}
{{ end }}

{{ if .HasQuery }}
func (r *Resolver) Query() generated.QueryResolver {
	return &queryResolver{Resolver: r}
}

type queryResolver struct {
	Resolver *Resolver
	DomainResolvers
}
{{ end }}

{{ if .HasSubscription }}
func (r *Resolver) Subscription() generated.SubscriptionResolver {
	return &subscriptionResolver{Resolver: r}
}

type subscriptionResolver struct {
	Resolver *Resolver
	DomainResolvers
}
{{ end }}

{{ range $c := .Ctors }}
func (r *Resolver) {{ $c.TypeName }}() generated.{{ $c.TypeName }}Resolver {
	return &{{ $c.Domain }}.{{ $c.TypeName }}Resolver{}
}
{{ end }}

{{/* Per-object ctors + wrapper structs for non-root types whose domain is not
     enabled. Mirrors what default gqlgen would emit. Lets the project keep
     existing field resolvers in *.resolvers.go (e.g. (r *todoResolver) User)
     compiling before the domain is migrated. */}}
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

{{ if .EmitMutationStruct }}
type {{ .MutationStructName }} struct{}
{{ end }}

{{ if .EmitQueryStruct }}
type {{ .QueryStructName }} struct{}
{{ end }}

{{ if .EmitSubscriptionStruct }}
type {{ .SubscriptionStructName }} struct{}
{{ end }}

{{ range $m := .MutationMethods }}
func (m *{{ $.MutationStructName }}) {{ $m.Field.GoFieldName }}{{ $m.Field.ShortResolverDeclaration }} {
	{{- if $m.Implementation }}
	{{ $m.Implementation }}
	{{- else }}
	panic(fmt.Errorf("not implemented: Mutation.{{ $m.Field.GoFieldName }}"))
	{{- end }}
}
{{ end }}

{{ range $m := .QueryMethods }}
func (q *{{ $.QueryStructName }}) {{ $m.Field.GoFieldName }}{{ $m.Field.ShortResolverDeclaration }} {
	{{- if $m.Implementation }}
	{{ $m.Implementation }}
	{{- else }}
	panic(fmt.Errorf("not implemented: Query.{{ $m.Field.GoFieldName }}"))
	{{- end }}
}
{{ end }}

{{ range $m := .SubscriptionMethods }}
func (s *{{ $.SubscriptionStructName }}) {{ $m.Field.GoFieldName }}{{ $m.Field.ShortResolverDeclaration }} {
	{{- if $m.Implementation }}
	{{ $m.Implementation }}
	{{- else }}
	panic(fmt.Errorf("not implemented: Subscription.{{ $m.Field.GoFieldName }}"))
	{{- end }}
}
{{ end }}

{{ range $m := .ObjectMethods }}
func (r *{{ ucFirst $m.Object.Name }}Resolver) {{ $m.Field.GoFieldName }}{{ $m.Field.ShortResolverDeclaration }} {
	{{- if $m.Implementation }}
	{{ $m.Implementation }}
	{{- else }}
	panic(fmt.Errorf("not implemented: {{ $m.Object.Name }}.{{ $m.Field.GoFieldName }}"))
	{{- end }}
}
{{ end }}

{{ range $obj := .Objects }}
// {{ ucFirst $obj.Name }}Resolver implements generated.{{ ucFirst $obj.Name }}Resolver structurally (Go duck typing).
type {{ ucFirst $obj.Name }}Resolver struct{}
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
