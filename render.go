package domainresolver

import (
	goast "go/ast"
	gparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/99designs/gqlgen/codegen"
	"github.com/99designs/gqlgen/codegen/templates"
)

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

func renderDomainFile(
	data *codegen.Data,
	pkgName string,
	outFile string,
	build *domainFileBuild,
	rw *astRewriter, // nil if package is being created for the first time
) error {
	prefix := domainStructPrefix(pkgName)
	mutationType := prefix + "Mutation"
	queryType := prefix + "Query"
	subscriptionType := prefix + "Subscription"

	for _, m := range build.MutationMethods {
		impl := ""
		if rw != nil {
			impl = rw.getMethodBody(mutationType, m.Field.GoFieldName)
		}
		m.Implementation = strings.TrimSpace(impl)
	}
	for _, m := range build.QueryMethods {
		impl := ""
		if rw != nil {
			impl = rw.getMethodBody(queryType, m.Field.GoFieldName)
		}
		m.Implementation = strings.TrimSpace(impl)
	}
	for _, m := range build.SubscriptionMethods {
		impl := ""
		if rw != nil {
			impl = rw.getMethodBody(subscriptionType, m.Field.GoFieldName)
		}
		m.Implementation = strings.TrimSpace(impl)
	}
	for _, m := range build.ObjectMethods {
		impl := ""
		if rw != nil {
			impl = rw.getMethodBody(m.Object.Name+"Resolver", m.Field.GoFieldName)
		}
		m.Implementation = strings.TrimSpace(impl)
	}

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

func (r *Resolver) Mutation() generated.MutationResolver {
	return &mutationResolver{Resolver: r}
}
func (r *Resolver) Query() generated.QueryResolver {
	return &queryResolver{Resolver: r}
}
{{ if .HasSubscription }}
func (r *Resolver) Subscription() generated.SubscriptionResolver {
	return &subscriptionResolver{Resolver: r}
}
{{ end }}

type mutationResolver struct {
	Resolver *Resolver
	DomainResolvers
}

type queryResolver struct {
	Resolver *Resolver
	DomainResolvers
}

{{ if .HasSubscription }}
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
