package domainresolver

import "testing"

func TestStripDomainPrefix(t *testing.T) {
	tests := []struct {
		name     string
		domain   string
		typeName string
		want     string
	}{
		{"strips PascalCase prefix", "catalog", "CatalogCategory", "Category"},
		{"strips multi-segment prefix", "business-process", "BusinessProcessStep", "Step"},
		{"keeps unrelated type", "import", "Entity", "Entity"},
		{"strips Import prefix", "import", "ImportStatus", "Status"},
		{"keeps singular vs plural mismatch", "tasks", "Task", "Task"},
		{"strips when type contains plural prefix", "tasks", "TasksList", "List"},
		{"exact match yields empty", "catalog", "Catalog", ""},
		{"empty domain leaves type unchanged", "", "Catalog", "Catalog"},
		{"empty type leaves it unchanged", "catalog", "", ""},
		{"no split mid-word", "cat", "Catalog", "Catalog"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripDomainPrefix(tt.domain, tt.typeName)
			if got != tt.want {
				t.Errorf("stripDomainPrefix(%q, %q) = %q, want %q", tt.domain, tt.typeName, got, tt.want)
			}
		})
	}
}

func TestObjectResolverName(t *testing.T) {
	tests := []struct {
		domain string
		obj    string
		want   string
	}{
		{"catalog", "CatalogCategory", "CategoryResolver"},
		{"import", "ImportStatus", "StatusResolver"},
		{"import", "Entity", "EntityResolver"},
		{"tasks", "Task", "TaskResolver"},
		{"todos", "Todo", "TodoResolver"},
		{"catalog", "Catalog", "Resolver"},
	}
	for _, tt := range tests {
		t.Run(tt.domain+"/"+tt.obj, func(t *testing.T) {
			got := objectResolverName(tt.domain, tt.obj)
			if got != tt.want {
				t.Errorf("objectResolverName(%q, %q) = %q, want %q", tt.domain, tt.obj, got, tt.want)
			}
		})
	}
}

func TestDomainStructPrefix(t *testing.T) {
	tests := []struct {
		domain string
		want   string
	}{
		{"", ""},
		{"todos", "MixinTodos"},
		{"x", "MixinX"},
		{"users", "MixinUsers"},
		// "Mixin" lead-in keeps the struct name from starting with the
		// package name (revive's package-stutter rule).
		{"todo", "MixinTodo"},
		// Dashes and underscores are word boundaries — each segment is
		// lowercased and capitalized so the struct name reads naturally
		// even when the package name itself is strip-only lowercase.
		{"business-process", "MixinBusinessProcess"},
		{"order_flow", "MixinOrderFlow"},
		{"a-b-c", "MixinABC"},
		{"foo--bar", "MixinFooBar"},
		// Mixed case is folded to lowercase before capitalization so we
		// don't end up with `MixinORderflow` from `OrderFlow`.
		{"ORDERFLOW", "MixinOrderflow"},
	}

	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			got := domainStructPrefix(tt.domain)
			if got != tt.want {
				t.Errorf("domainStructPrefix(%q) = %q, want %q", tt.domain, got, tt.want)
			}
		})
	}
}
