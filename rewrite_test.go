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
	writeFile(t, dir, "todo.resolvers.go", `package todos

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
	writeFile(t, dir, "todo.resolvers.go", `package todos

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
	writeFile(t, dir, "todo.resolvers.go", `package todos

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
	writeFile(t, dir, "todo.resolvers.go", `package todos

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

// TestASTRewriter_ExistingImports_Plain captures import paths from the file
// matching outFile. Regression for the bug where third-party imports referenced
// only inside copied method bodies were dropped on regeneration.
func TestASTRewriter_ExistingImports_Plain(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "todo.resolvers.go", `package todos

import (
	"context"

	"example.com/foo/service/todo"
)

type TodoResolver struct{}

func (r *TodoResolver) Something(ctx context.Context) string {
	return todo.DoSomething()
}
`)

	rw, err := newASTRewriter(dir)
	if err != nil {
		t.Fatalf("newASTRewriter: %v", err)
	}

	got := rw.existingImports(filepath.Join(dir, "todo.resolvers.go"))
	want := map[string]string{
		"context":                      "",
		"example.com/foo/service/todo": "",
	}
	if len(got) != len(want) {
		t.Fatalf("existingImports len = %d, want %d (%v)", len(got), len(want), got)
	}
	for _, imp := range got {
		alias, ok := want[imp.ImportPath]
		if !ok {
			t.Errorf("unexpected import %q", imp.ImportPath)
			continue
		}
		if imp.Alias != alias {
			t.Errorf("import %q alias = %q, want %q", imp.ImportPath, imp.Alias, alias)
		}
	}
}

// TestASTRewriter_ExistingImports_Aliased captures named, blank and dot imports.
func TestASTRewriter_ExistingImports_Aliased(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "todo.resolvers.go", `package todos

import (
	myalias "example.com/foo"
	_ "example.com/blank"
	. "example.com/dot"
)

var _ = myalias.X
var _ = Y
`)

	rw, err := newASTRewriter(dir)
	if err != nil {
		t.Fatalf("newASTRewriter: %v", err)
	}

	got := rw.existingImports(filepath.Join(dir, "todo.resolvers.go"))
	want := map[string]string{
		"example.com/foo":   "myalias",
		"example.com/blank": "_",
		"example.com/dot":   ".",
	}
	if len(got) != len(want) {
		t.Fatalf("existingImports len = %d, want %d (%v)", len(got), len(want), got)
	}
	for _, imp := range got {
		alias, ok := want[imp.ImportPath]
		if !ok {
			t.Errorf("unexpected import %q", imp.ImportPath)
			continue
		}
		if imp.Alias != alias {
			t.Errorf("import %q alias = %q, want %q", imp.ImportPath, imp.Alias, alias)
		}
	}
}

// TestASTRewriter_ExistingImports_PerFile only returns imports from the
// requested file, not from siblings in the same dir.
func TestASTRewriter_ExistingImports_PerFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "todo.resolvers.go", `package todos

import "example.com/todo"

var _ = todo.X
`)
	writeFile(t, dir, "user.resolvers.go", `package todos

import "example.com/user"

var _ = user.Y
`)

	rw, err := newASTRewriter(dir)
	if err != nil {
		t.Fatalf("newASTRewriter: %v", err)
	}

	got := rw.existingImports(filepath.Join(dir, "todo.resolvers.go"))
	if len(got) != 1 || got[0].ImportPath != "example.com/todo" {
		t.Errorf("existingImports(todo.resolvers.go) = %v, want [example.com/todo]", got)
	}
}

// TestASTRewriter_ExistingImports_MissingFile returns nil when the requested
// file isn't part of the parsed set.
func TestASTRewriter_ExistingImports_MissingFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "todo.resolvers.go", `package todos

import "example.com/todo"

var _ = todo.X
`)

	rw, err := newASTRewriter(dir)
	if err != nil {
		t.Fatalf("newASTRewriter: %v", err)
	}

	got := rw.existingImports(filepath.Join(dir, "missing.resolvers.go"))
	if got != nil {
		t.Errorf("existingImports(missing.resolvers.go) = %v, want nil", got)
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
