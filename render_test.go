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
