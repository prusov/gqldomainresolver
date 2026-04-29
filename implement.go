package domainresolver

import (
	"fmt"

	"github.com/99designs/gqlgen/codegen"
)

// Implement is called by resolvergen for each resolver field.
// Returns the method body string written into .resolvers.go in the root package.
//
// Returning "" signals the safety-net template (graph/templates/resolver.gotpl)
// to skip emitting a method declaration entirely. We rely on Go method promotion:
// Resolver embeds DomainResolvers which embeds per-domain Mutation/Query structs,
// whose methods are promoted up through mutationResolver{*Resolver}.
//
// Behavior, in evaluation order:
//   - root field with domain → "" (template skips; method comes via promotion).
//     This must beat prevImpl so legacy hand-shaped delegation stubs
//     (return todos.MutationCreateTodo(...)) are deleted on regen.
//   - prevImpl != "" → preserve hand-written code (e.g. manual Hello body).
//   - root field without domain → panic stub (no domain package to delegate to).
//   - non-root field → panic stub (defensive; constructor returns a domain-package
//     resolver directly so the template should not emit these).
func (p *Plugin) Implement(prevImpl string, field *codegen.Field) string {
	domain := p.domainFor(field.Position.Src.Name)

	if field.Object.Root && domain != "" {
		return ""
	}

	if prevImpl != "" {
		return prevImpl
	}

	return panicStub(field)
}

// panicStub mirrors the default body resolvergen produces when no plugin is
// registered: panic(fmt.Errorf("not implemented: GoFieldName - graphqlName")).
// fmt is reserved by the resolver template, so no extra import is needed.
func panicStub(field *codegen.Field) string {
	return fmt.Sprintf(`panic(fmt.Errorf("not implemented: %v - %v"))`,
		field.GoFieldName, field.Name)
}
