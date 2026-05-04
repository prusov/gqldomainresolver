package domainresolver

import (
	"strings"
	"testing"
)

func TestImplement(t *testing.T) {
	t.Parallel()
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
			// Enabled domain owns the field — method lives in the domain pkg
			// (e.g. todos.TodoResolver.User), so root file must drop it.
			name:  "non-root field with enabled domain returns empty",
			field: &domainField{Object: todoObj, Field: makeFieldWithPos("User", todoObj, todoSchema)},
			want:  "",
		},
		{
			// Domain present in schema path but NOT in allowlist — gradual
			// migration mid-flight. prevImpl from the existing root file must
			// survive regen; otherwise we silently delete user code.
			name:     "non-root field with disabled domain preserves prevImpl",
			prevImpl: `return &model.User{ID: "real"}, nil`,
			field:    &domainField{Object: makeObject("Other", false), Field: makeFieldWithPos("User", makeObject("Other", false), "/abs/graph/schema/other/x.graphqls")},
			want:     `return &model.User{ID: "real"}, nil`,
		},
		{
			name:       "non-root field with disabled domain falls back to panic stub",
			field:      &domainField{Object: makeObject("Other", false), Field: makeFieldWithPos("User", makeObject("Other", false), "/abs/graph/schema/other/x.graphqls")},
			wantPrefix: `panic(fmt.Errorf("not implemented:`,
		},
	}

	p := mustNew(t, WithEnabledDomains("todos"))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
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

// TestImplement_StashesPrevImplForMigratedDomain captures the contract that
// makes first-time domain migration work without losing user code: when a
// field's domain becomes enabled, Implement returns "" (so the root method is
// stripped) AND stashes prevImpl in migratedImpls keyed by Object.Field, so
// that GenerateCode can rehydrate the body inside the new domain package
// (where the AST rewriter sees an empty / non-existent dir).
func TestImplement_StashesPrevImplForMigratedDomain(t *testing.T) {
	t.Parallel()
	p := mustNew(t, WithEnabledDomains("todos"))

	mutationObj := makeObject("Mutation", true)
	field := makeFieldWithPos("CreateTodo", mutationObj, todoSchema)

	got := p.Implement(`return &model.Todo{ID: "1"}, nil`, field)
	if got != "" {
		t.Fatalf("Implement should return empty for migrated domain, got %q", got)
	}
	stashed := p.migratedImpls[migratedImplKey("Mutation", "CreateTodo")]
	if stashed != `return &model.Todo{ID: "1"}, nil` {
		t.Errorf("migratedImpls miss: got %q", stashed)
	}

	// Empty prevImpl must NOT poison the cache (panic stubs would overwrite a
	// legit prior generation if we stashed them).
	field2 := makeFieldWithPos("Todos", makeObject("Query", true), todoSchema)
	_ = p.Implement("", field2)
	if _, ok := p.migratedImpls[migratedImplKey("Query", "Todos")]; ok {
		t.Errorf("empty prevImpl must not be stashed")
	}
}

func TestPanicStub_Format(t *testing.T) {
	t.Parallel()
	mutationObj := makeObject("Mutation", true)
	field := makeFieldWithPos("CreateTodo", mutationObj, todoSchema)
	field.Name = "createTodo"

	got := panicStub(field)
	want := `panic(fmt.Errorf("not implemented: CreateTodo - createTodo"))`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
