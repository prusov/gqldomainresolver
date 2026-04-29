package domainresolver

import "github.com/99designs/gqlgen/codegen"

// Plugin implements gqlgen's ResolverImplementer + CodeGenerator.
//
// The import path for generated domain packages is derived at codegen time
// from the resolver section of gqlgen.yml — the plugin is module-agnostic.
type Plugin struct{}

func New() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "domain-resolver" }

var _ interface {
	Implement(string, *codegen.Field) string
	GenerateCode(*codegen.Data) error
} = (*Plugin)(nil)
