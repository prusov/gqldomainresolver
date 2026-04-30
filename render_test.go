package domainresolver

import "testing"

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
