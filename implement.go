package domainresolver

import (
	"fmt"
	"strings"

	"github.com/99designs/gqlgen/codegen"
	"github.com/99designs/gqlgen/codegen/templates"
)

// Implement is called by resolvergen for each resolver field.
// Returns the method body string written into .resolvers.go in the root package.
func (p *Plugin) Implement(prevImpl string, field *codegen.Field) string {
	if prevImpl != "" {
		// Method already exists — return unchanged to preserve manual code.
		return prevImpl
	}

	domain := extractDomain(field.Position.Src.Name)
	if domain == "" {
		// Root schema or invalid directory name — no domain package to delegate to.
		// Once ImplementationRender is set, gqlgen no longer falls back to its default
		// panic stub for empty results, so we must emit it ourselves.
		return panicStub(field)
	}

	// Register domain package import explicitly via CurrentImports.
	//
	// Why not goimports: the domain function (e.g. todos.MutationCreateTodo) doesn't
	// exist at generation time, so goimports won't add an import for a missing symbol.
	// templates.CurrentImports is initialized by resolvergen BEFORE Implement() is called.
	importPath := p.importPrefix + domain
	alias := domain
	if _, err := templates.CurrentImports.Reserve(importPath); err != nil {
		// Alias collision — fallback to domain+"domain".
		alias = domain + "domain"
		if _, err2 := templates.CurrentImports.Reserve(importPath, alias); err2 != nil {
			return panicStub(field)
		}
	}

	return buildDelegation(alias, field)
}

// panicStub mirrors the default body resolvergen produces when no plugin is
// registered: panic(fmt.Errorf("not implemented: GoFieldName - graphqlName")).
// fmt is reserved by the resolver template, so no extra import is needed.
func panicStub(field *codegen.Field) string {
	return fmt.Sprintf(`panic(fmt.Errorf("not implemented: %v - %v"))`,
		field.GoFieldName, field.Name)
}

// buildDelegation builds the delegation method body.
//
// Convention:
//
//	Query    → todos.QueryTodos(ctx, ...)
//	Mutation → todos.MutationCreateTodo(ctx, input)
//	Field    → todos.TodoResolver{}.Something(ctx, obj)
func buildDelegation(pkgAlias string, field *codegen.Field) string {
	args := delegationArgs(field)

	if field.Object.Root {
		prefix := "Query"
		if field.Object.Name == "Mutation" {
			prefix = "Mutation"
		}
		return fmt.Sprintf("return %s.%s%s(%s)", pkgAlias, prefix, field.GoFieldName, args)
	}

	// Field resolver (not Query or Mutation):
	// (&pkgAlias.TypeNameResolver{}).MethodName(ctx, obj, ...)
	return fmt.Sprintf("return (&%s.%sResolver{}).%s(%s)",
		pkgAlias, field.Object.Name, field.GoFieldName, args)
}

// delegationArgs builds the argument list for the delegation call using
// the Go parameter names from the method signature (not gqlgen's internal fc.Args expressions).
func delegationArgs(field *codegen.Field) string {
	parts := []string{"ctx"}
	if !field.Object.Root {
		parts = append(parts, "obj")
	}
	for _, arg := range field.Args {
		parts = append(parts, arg.VarName)
	}
	return strings.Join(parts, ", ")
}
