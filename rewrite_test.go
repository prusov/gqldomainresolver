package domainresolver

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFile is a test helper that writes content to a file inside dir.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("writeFile: %v", err)
	}
}

// TestASTRewriter_GetMethodBody_PointerReceiver extracts body from a pointer-receiver method.
func TestASTRewriter_GetMethodBody_PointerReceiver(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "todo.go", `package todos

type TodoResolver struct{}

func (r *TodoResolver) Something() (string, error) {
	return "hello", nil
}
`)

	rw, err := newASTRewriter(dir)
	if err != nil {
		t.Fatalf("newASTRewriter: %v", err)
	}

	got := rw.getMethodBody("TodoResolver", "Something")
	want := `return "hello", nil`
	if got != want {
		t.Errorf("getMethodBody = %q, want %q", got, want)
	}
}

// TestASTRewriter_GetMethodBody_ValueReceiver extracts body from a value-receiver method.
func TestASTRewriter_GetMethodBody_ValueReceiver(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "todo.go", `package todos

type TodoResolver struct{}

func (r TodoResolver) Something() string {
	return "value recv"
}
`)

	rw, err := newASTRewriter(dir)
	if err != nil {
		t.Fatalf("newASTRewriter: %v", err)
	}

	got := rw.getMethodBody("TodoResolver", "Something")
	want := `return "value recv"`
	if got != want {
		t.Errorf("getMethodBody = %q, want %q", got, want)
	}
}

// TestASTRewriter_GetMethodBody_NotFound returns empty string for an unknown method.
func TestASTRewriter_GetMethodBody_NotFound(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "todo.go", `package todos

type TodoResolver struct{}

func (r *TodoResolver) User() string { return "user" }
`)

	rw, err := newASTRewriter(dir)
	if err != nil {
		t.Fatalf("newASTRewriter: %v", err)
	}

	got := rw.getMethodBody("TodoResolver", "Something")
	if got != "" {
		t.Errorf("expected empty string for missing method, got %q", got)
	}
}

// TestASTRewriter_GetMethodBody_WrongType returns empty if receiver type doesn't match.
func TestASTRewriter_GetMethodBody_WrongType(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "todo.go", `package todos

type UserResolver struct{}

func (r *UserResolver) Something() string { return "user" }
`)

	rw, err := newASTRewriter(dir)
	if err != nil {
		t.Fatalf("newASTRewriter: %v", err)
	}

	got := rw.getMethodBody("TodoResolver", "Something")
	if got != "" {
		t.Errorf("expected empty string for wrong receiver type, got %q", got)
	}
}

// TestNewASTRewriter_EmptyDir returns error for an already-empty directory.
func TestNewASTRewriter_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	rw, err := newASTRewriter(dir)
	if err == nil {
		t.Error("expected error for empty dir, got nil")
	}
	if rw != nil {
		t.Error("expected nil rewriter for empty dir")
	}
}

// TestNewASTRewriter_NonExistentDir returns error for a missing directory.
func TestNewASTRewriter_NonExistentDir(t *testing.T) {
	rw, err := newASTRewriter("/nonexistent/path")
	if err == nil {
		t.Error("expected error for non-existent dir, got nil")
	}
	if rw != nil {
		t.Error("expected nil rewriter for non-existent dir")
	}
}
