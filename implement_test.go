package domainresolver

import (
	"strings"
	"testing"
)

func TestImplement(t *testing.T) {
	mutationObj := makeObject("Mutation", true)
	queryObj := makeObject("Query", true)
	todoObj := makeObject("Todo", false)

	tests := []struct {
		name       string
		prevImpl   string
		field      *domainField
		wantPrefix string // "" → exact match against want
		want       string
	}{
		{
			name:  "root field with domain returns empty (template skips method)",
			field: &domainField{Object: mutationObj, Field: makeFieldWithPos("CreateTodo", mutationObj, todoSchema)},
			want:  "",
		},
		{
			name: "root field with domain wins over prevImpl",
			// prevImpl from an older codegen run must be wiped so the method
			// reaches callers via promotion, not a stub in the root package.
			prevImpl: "return todos.MutationCreateTodo(ctx, input)",
			field:    &domainField{Object: mutationObj, Field: makeFieldWithPos("CreateTodo", mutationObj, todoSchema)},
			want:     "",
		},
		{
			name:       "root field without domain returns panic stub",
			field:      &domainField{Object: queryObj, Field: makeFieldWithPos("Hello", queryObj, "/abs/graph/schema/schema.graphqls")},
			wantPrefix: `panic(fmt.Errorf("not implemented:`,
		},
		{
			name:     "root field without domain preserves prevImpl",
			prevImpl: `return "world", nil`,
			field:    &domainField{Object: queryObj, Field: makeFieldWithPos("Hello", queryObj, "/abs/graph/schema/schema.graphqls")},
			want:     `return "world", nil`,
		},
		{
			name:  "non-root field returns panic stub by default",
			field: &domainField{Object: todoObj, Field: makeFieldWithPos("User", todoObj, todoSchema)},
			// non-root fields normally never reach Implement (constructor route),
			// but this is the defensive fallback.
			wantPrefix: `panic(fmt.Errorf("not implemented:`,
		},
	}

	p := New(WithEnabledDomains("todos"))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.Implement(tt.prevImpl, tt.field.Field)
			if tt.wantPrefix != "" {
				if !strings.HasPrefix(got, tt.wantPrefix) {
					t.Errorf("got %q, want prefix %q", got, tt.wantPrefix)
				}

				return
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPanicStub_Format(t *testing.T) {
	mutationObj := makeObject("Mutation", true)
	field := makeFieldWithPos("CreateTodo", mutationObj, todoSchema)
	field.Name = "createTodo"

	got := panicStub(field)
	want := `panic(fmt.Errorf("not implemented: CreateTodo - createTodo"))`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
