package domainresolver

import "github.com/99designs/gqlgen/codegen"

const moduleImportPrefix = "git.dd-team.online/dd/gqlgendomain/graph/resolver/"

// Plugin implements ResolverImplementer + CodeGenerator.
type Plugin struct {
	importPrefix string
}

func New() *Plugin {
	return &Plugin{importPrefix: moduleImportPrefix}
}

func (p *Plugin) Name() string { return "domain-resolver" }

// Compile-time assertions that Plugin implements both interfaces.
var _ interface {
	Implement(string, *codegen.Field) string
	GenerateCode(*codegen.Data) error
} = (*Plugin)(nil)
