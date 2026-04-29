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
// each wrapper (mutationResolver / queryResolver / subscriptionResolver) embeds
// the kind-specific DomainMutationResolvers / DomainQueryResolvers /
// DomainSubscriptionResolvers, which embed the per-domain Mixin*Mutation/Query/
// Subscription structs, whose methods are promoted up through the wrapper.
//
// Behavior, in evaluation order:
//   - field whose domain is enabled → "" (template skips; method lives in the
//     domain package). Applies to root and non-root fields alike — non-root
//     methods are reached via the per-object constructor returning the
//     domain-package resolver.
//   - prevImpl != "" → preserve hand-written code in the root file. Critical
//     for the gradual migration case: a project's existing field resolvers
//     (e.g. (r *todoResolver) User) must survive regen until their domain
//     is enabled.
//   - otherwise → panic stub.
func (p *Plugin) Implement(prevImpl string, field *codegen.Field) string {
	if p.domainFor(field.Position.Src.Name) != "" {
		// Stash the existing body so first-time migrations can rehydrate the
		// domain-package method from it. Only meaningful bodies are kept —
		// empty/panic stubs would just overwrite legit prior generations.
		if prevImpl != "" && field.Object != nil {
			p.migratedImpls[migratedImplKey(field.Object.Name, field.GoFieldName)] = prevImpl
		}
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
