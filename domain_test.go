package domainresolver

import (
	"testing"
)

func TestExtractDomain(t *testing.T) {
	tests := []struct {
		name       string
		schemaPath string
		want       string
	}{
		{
			name:       "todos domain",
			schemaPath: "/abs/path/graph/schema/todos/todo.graphqls",
			want:       "todos",
		},
		{
			name:       "users domain",
			schemaPath: "/abs/path/graph/schema/users/user.graphqls",
			want:       "users",
		},
		{
			name:       "root schema — parent is schema",
			schemaPath: "/abs/path/graph/schema/schema.graphqls",
			want:       "",
		},
		{
			name:       "common directives — valid domain name, no resolvers so file won't appear, but extractDomain returns common",
			schemaPath: "/abs/path/graph/schema/common/directives.graphqls",
			want:       "common",
		},
		{
			name:       "directory with dash — invalid Go identifier",
			schemaPath: "/abs/path/graph/schema/business-process/x.graphqls",
			want:       "",
		},
		{
			name:       "reserved keyword: import",
			schemaPath: "/abs/path/graph/schema/import/x.graphqls",
			want:       "",
		},
		{
			name:       "reserved keyword: type",
			schemaPath: "/abs/path/graph/schema/type/x.graphqls",
			want:       "",
		},
		{
			name:       "reserved keyword: func",
			schemaPath: "/abs/path/graph/schema/func/x.graphqls",
			want:       "",
		},
		{
			name:       "only filename, no parent dir",
			schemaPath: "schema.graphqls",
			want:       "",
		},
		{
			name:       "nested schema path with multiple segments",
			schemaPath: "/a/b/c/orders/order.graphqls",
			want:       "orders",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractDomain(tt.schemaPath)
			if got != tt.want {
				t.Errorf("extractDomain(%q) = %q, want %q", tt.schemaPath, got, tt.want)
			}
		})
	}
}

func TestIsValidDomain(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"valid: todos", "todos", true},
		{"valid: users", "users", true},
		{"valid: orders", "orders", true},
		{"invalid: empty", "", false},
		{"invalid: schema", "schema", false},
		{"invalid: dash", "my-domain", false},
		{"invalid: keyword break", "break", false},
		{"invalid: keyword default", "default", false},
		{"invalid: keyword go", "go", false},
		{"invalid: keyword map", "map", false},
		{"invalid: keyword var", "var", false},
		{"invalid: keyword select", "select", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidDomain(tt.input)
			if got != tt.want {
				t.Errorf("isValidDomain(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
