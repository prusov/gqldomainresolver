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
	return &astRewriter{fset: fset, files: files}, nil
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
			return rw.bodySource(filename, fn.Body)
		}
	}
	return ""
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
	Methods []*domainMethodBuild
	Objects []*codegen.Object // for generating struct types (XxxResolver struct{})
}

func renderDomainFile(
	data *codegen.Data,
	pkgName string,
	outFile string,
	fg *fileGroup,
	rw *astRewriter, // nil if package is being created for the first time
) error {
	domainDir := filepath.Dir(outFile)
	build := &domainFileBuild{Objects: fg.objects}

	for _, df := range fg.fields {
		impl := ""
		if df.Object.Root {
			// rewrite.Rewriter only handles methods with receivers, so free functions
			// (MutationXxx / QueryXxx) need a custom lookup via go/parser.
			prefix := "Query"
			if df.Object.Name == "Mutation" {
				prefix = "Mutation"
			}
			impl = getFreeFuncBody(domainDir, prefix+df.Field.GoFieldName)
		} else if rw != nil {
			impl = rw.getMethodBody(receiverTypeName(df), df.Field.GoFieldName)
		}

		build.Methods = append(build.Methods, &domainMethodBuild{
			Object:         df.Object,
			Field:          df.Field,
			Implementation: strings.TrimSpace(impl),
		})
	}

	return templates.Render(templates.Options{
		PackageName: pkgName,
		Filename:    outFile,
		Data:        build,
		Packages:    data.Config.Packages,
		Template:    domainTemplate,
	})
}

// receiverTypeName returns the receiver type name for rewriter method lookup.
// Field resolver methods → methods on XxxResolver struct.
// Query/Mutation → free functions, handled separately via getFreeFuncBody.
func receiverTypeName(df *domainField) string {
	if df.Object.Root {
		return ""
	}
	return df.Object.Name + "Resolver"
}

// getFreeFuncBody extracts the body of a top-level free function (no receiver) from .go files in dir.
// Used to preserve hand-written implementations across re-generations.
// Uses ParseFile per file to avoid the deprecated ParseDir/ast.Package APIs.
func getFreeFuncBody(dir, funcName string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	fset := token.NewFileSet()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		filename := filepath.Join(dir, entry.Name())
		f, err := gparser.ParseFile(fset, filename, nil, 0)
		if err != nil {
			continue
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*goast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Name.Name != funcName {
				continue
			}
			src, err := os.ReadFile(filename)
			if err != nil {
				return ""
			}
			// fn.Body.Lbrace points to '{', so +1 skips it.
			// fn.Body.Rbrace points to '}', so we stop before it.
			startOff := fset.Position(fn.Body.Lbrace).Offset + 1
			endOff := fset.Position(fn.Body.Rbrace).Offset
			return strings.TrimSpace(string(src[startOff:endOff]))
		}
	}
	return ""
}

// renderConstructorsFile renders the domain_resolvers.go file in the root resolver package.
// build is a struct with GeneratedPkg (string), DomainImports ([]string), and Ctors ([]struct{TypeName, Domain string}).
func renderConstructorsFile(data *codegen.Data, outFile string, build any) error {
	return templates.Render(templates.Options{
		PackageName: data.Config.Resolver.Package,
		Filename:    outFile,
		Data:        build,
		Packages:    data.Config.Packages,
		Template:    constructorsTemplate,
	})
}

// constructorsTemplate emits one constructor per non-root domain object:
//
//	func (r *Resolver) Todo() generated.TodoResolver { return &todos.TodoResolver{} }
//
// The generated package and each domain package are reserved via reserveImport
// so goimports doesn't strip them.
const constructorsTemplate = `
{{ reserveImport .GeneratedPkg }}
{{ range $imp := .DomainImports }}{{ reserveImport $imp }}{{ end }}

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
//  2. Query/Mutation → free functions Query{Name}() / Mutation{Name}().
//  3. Field resolvers → methods on {TypeName}Resolver struct.
//  4. Existing implementations are restored via rewriter (methods) or getFreeFuncBody (free funcs).
const domainTemplate = `
{{- reserveImport "context" -}}
{{- reserveImport "fmt" -}}

{{ range $m := .Methods }}
{{- if $m.Object.Root }}
{{- if eq $m.Object.Name "Mutation" }}
func Mutation{{ $m.Field.GoFieldName }}{{ $m.Field.ShortResolverDeclaration }} {
	{{- if $m.Implementation }}
	{{ $m.Implementation }}
	{{- else }}
	panic(fmt.Errorf("not implemented: Mutation{{ $m.Field.GoFieldName }}"))
	{{- end }}
}
{{- else }}
func Query{{ $m.Field.GoFieldName }}{{ $m.Field.ShortResolverDeclaration }} {
	{{- if $m.Implementation }}
	{{ $m.Implementation }}
	{{- else }}
	panic(fmt.Errorf("not implemented: Query{{ $m.Field.GoFieldName }}"))
	{{- end }}
}
{{- end }}
{{- else }}
func (r *{{ ucFirst $m.Object.Name }}Resolver) {{ $m.Field.GoFieldName }}{{ $m.Field.ShortResolverDeclaration }} {
	{{- if $m.Implementation }}
	{{ $m.Implementation }}
	{{- else }}
	panic(fmt.Errorf("not implemented: {{ $m.Object.Name }}.{{ $m.Field.GoFieldName }}"))
	{{- end }}
}
{{- end }}
{{ end }}

{{ range $obj := .Objects }}
// {{ ucFirst $obj.Name }}Resolver implements generated.{{ ucFirst $obj.Name }}Resolver structurally (Go duck typing).
type {{ ucFirst $obj.Name }}Resolver struct{}
{{ end }}
`
