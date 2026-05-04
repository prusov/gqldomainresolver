package domainresolver

import (
	"testing"
)

func TestExtractDomain(t *testing.T) {
	tests := []struct {
		name       string
		schemaPath string
		want       domain
	}{
		{
			name:       "todos domain",
			schemaPath: "/abs/path/graph/schema/todos/todo.graphqls",
			want:       domain{Raw: "todos", Pkg: "todos"},
		},
		{
			name:       "users domain",
			schemaPath: "/abs/path/graph/schema/users/user.graphqls",
			want:       domain{Raw: "users", Pkg: "users"},
		},
		{
			name:       "root schema — parent is schema",
			schemaPath: "/abs/path/graph/schema/schema.graphqls",
			want:       domain{},
		},
		{
			name:       "common directives — valid domain name",
			schemaPath: "/abs/path/graph/schema/common/directives.graphqls",
			want:       domain{Raw: "common", Pkg: "common"},
		},
		{
			name:       "directory with dash — strip-only lowercase",
			schemaPath: "/abs/path/graph/schema/business-process/x.graphqls",
			want:       domain{Raw: "business-process", Pkg: "businessprocess"},
		},
		{
			name:       "directory with underscore — strip-only lowercase",
			schemaPath: "/abs/path/graph/schema/order_flow/x.graphqls",
			want:       domain{Raw: "order_flow", Pkg: "orderflow"},
		},
		{
			name:       "mixed case directory normalises to lowercase",
			schemaPath: "/abs/path/graph/schema/OrderFlow/x.graphqls",
			want:       domain{Raw: "OrderFlow", Pkg: "orderflow"},
		},
		{
			name:       "reserved keyword: import",
			schemaPath: "/abs/path/graph/schema/import/x.graphqls",
			want:       domain{Raw: "import", Pkg: "gqlimport"},
		},
		{
			name:       "reserved keyword: type",
			schemaPath: "/abs/path/graph/schema/type/x.graphqls",
			want:       domain{Raw: "type", Pkg: "gqltype"},
		},
		{
			name:       "reserved keyword: func",
			schemaPath: "/abs/path/graph/schema/func/x.graphqls",
			want:       domain{Raw: "func", Pkg: "gqlfunc"},
		},
		{
			name:       "leading digit gets keyword prefix",
			schemaPath: "/abs/path/graph/schema/2fa/x.graphqls",
			want:       domain{Raw: "2fa", Pkg: "gql2fa"},
		},
		{
			name:       "only filename, no parent dir",
			schemaPath: "schema.graphqls",
			want:       domain{},
		},
		{
			name:       "directory of only separators normalises to empty",
			schemaPath: "/a/b/-_-/x.graphqls",
			want:       domain{},
		},
		{
			name:       "nested schema path with multiple segments",
			schemaPath: "/a/b/c/orders/order.graphqls",
			want:       domain{Raw: "orders", Pkg: "orders"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractDomain(tt.schemaPath, DefaultKeywordPrefix)
			if got != tt.want {
				t.Errorf("extractDomain(%q) = %+v, want %+v", tt.schemaPath, got, tt.want)
			}
		})
	}
}

func TestNormalizeDomain(t *testing.T) {
	tests := []struct {
		input  string
		prefix string
		want   string
	}{
		{"todos", "gql", "todos"},
		{"business-process", "gql", "businessprocess"},
		{"order_flow", "gql", "orderflow"},
		{"OrderFlow", "gql", "orderflow"},
		{"import", "gql", "gqlimport"},
		{"schema", "gql", "gqlschema"},
		{"2fa", "gql", "gql2fa"},
		{"2fa", "x", "x2fa"},
		{"", "gql", ""},
		{"-_-", "gql", ""},
		{"break", "domain", "domainbreak"},
	}

	for _, tt := range tests {
		t.Run(tt.input+"_"+tt.prefix, func(t *testing.T) {
			got := normalizeDomain(tt.input, tt.prefix)
			if got != tt.want {
				t.Errorf("normalizeDomain(%q, %q) = %q, want %q", tt.input, tt.prefix, got, tt.want)
			}
		})
	}
}

func TestExtractDomain_CustomPrefix(t *testing.T) {
	got := extractDomain("/x/schema/import/x.graphqls", "domain")
	want := domain{Raw: "import", Pkg: "domainimport"}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}
