package domainresolver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/99designs/gqlgen/codegen"
	gqlast "github.com/vektah/gqlparser/v2/ast"
)

// makeObject creates a minimal codegen.Object for testing.
func makeObject(name string, root bool) *codegen.Object {
	return &codegen.Object{
		Definition: &gqlast.Definition{Name: name},
		Root:       root,
	}
}

// makeFieldWithPos creates a codegen.Field with a schema file position set.
func makeFieldWithPos(goName string, obj *codegen.Object, schemaPath string, args ...*codegen.FieldArgument) *codegen.Field {
	return &codegen.Field{
		FieldDefinition: &gqlast.FieldDefinition{
			Name: goName,
			Position: &gqlast.Position{
				Src: &gqlast.Source{Name: schemaPath},
			},
		},
		GoFieldName: goName,
		Object:      obj,
		Args:        args,
	}
}

// makeObjectWithPos creates a codegen.Object with a schema file position set.
func makeObjectWithPos(name string, root bool, schemaPath string) *codegen.Object {
	return &codegen.Object{
		Definition: &gqlast.Definition{
			Name: name,
			Position: &gqlast.Position{
				Src: &gqlast.Source{Name: schemaPath},
			},
		},
		Root: root,
	}
}

// objWithResolverField builds an Object with a single resolver field so that
// HasResolvers() reports true.
func objWithResolverField(name string, root bool, schemaPath string) *codegen.Object {
	o := makeObjectWithPos(name, root, schemaPath)
	o.Fields = []*codegen.Field{{
		FieldDefinition: &gqlast.FieldDefinition{
			Name:     "f",
			Position: &gqlast.Position{Src: &gqlast.Source{Name: schemaPath}},
		},
		IsResolver: true,
	}}

	return o
}

// writeFile writes content to dir/name with a fatal-on-error wrapper.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("writeFile: %v", err)
	}
}

// mustNew constructs a Plugin or fails the test.
func mustNew(t *testing.T, opts ...Option) *Plugin {
	t.Helper()
	p, err := New(opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return p
}

// Schema-path constants reused across the test suite.
const (
	todoSchema = "/abs/graph/schema/todos/todo.graphqls"
	userSchema = "/abs/graph/schema/users/user.graphqls"
	todoType   = "Todo"
	userType   = "User"
)
