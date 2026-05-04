package gqldomainresolver

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestStripDomainPrefix(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			got := stripDomainPrefix(tt.domain, tt.typeName)
			if got != tt.want {
				t.Errorf("stripDomainPrefix(%q, %q) = %q, want %q", tt.domain, tt.typeName, got, tt.want)
			}
		})
	}
}

func TestObjectResolverName(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			got := objectResolverName(tt.domain, tt.obj)
			if got != tt.want {
				t.Errorf("objectResolverName(%q, %q) = %q, want %q", tt.domain, tt.obj, got, tt.want)
			}
		})
	}
}

func TestDomainStructPrefix(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			got := domainStructPrefix(tt.domain)
			if got != tt.want {
				t.Errorf("domainStructPrefix(%q) = %q, want %q", tt.domain, got, tt.want)
			}
		})
	}
}

func TestReceiverFor(t *testing.T) {
	t.Parallel()
	mut := makeObject("Mutation", true)
	qry := makeObject("Query", true)
	sub := makeObject("Subscription", true)
	todo := makeObject("Todo", false)
	catalogCategory := makeObject("CatalogCategory", false)

	tests := []struct {
		name   string
		method *domainMethodBuild
		domain string
		want   string
	}{
		{"mutation root", &domainMethodBuild{Object: mut}, "todos", "MixinTodosMutation"},
		{"query root", &domainMethodBuild{Object: qry}, "todos", "MixinTodosQuery"},
		{"subscription root", &domainMethodBuild{Object: sub}, "todos", "MixinTodosSubscription"},
		{"non-root unstripped", &domainMethodBuild{Object: todo}, "todos", "TodoResolver"},
		{"non-root with stripped prefix", &domainMethodBuild{Object: catalogCategory}, "catalog", "CategoryResolver"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			prefix := domainStructPrefix(tt.domain)
			got := receiverFor(tt.method, tt.domain, prefix+"Mutation", prefix+"Query", prefix+"Subscription")
			if got != tt.want {
				t.Errorf("receiverFor = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSortEmbeds(t *testing.T) {
	t.Parallel()
	es := []embed{
		{TypeName: "MixinUsersMutation", Pkg: "users"},
		{TypeName: "MixinTodosMutation", Pkg: "todos"},
		{TypeName: "MixinTodosOther", Pkg: "todos"},
	}
	sortEmbeds(es)

	wantOrder := []string{"todos.MixinTodosMutation", "todos.MixinTodosOther", "users.MixinUsersMutation"}
	for i, w := range wantOrder {
		got := es[i].Pkg + "." + es[i].TypeName
		if got != w {
			t.Errorf("[%d] = %q, want %q", i, got, w)
		}
	}
}

func TestEmbedDomainImports(t *testing.T) {
	t.Parallel()
	es := []embed{
		{Pkg: "todos"},
		{Pkg: "users"},
		{Pkg: "todos"}, // duplicate — deduplicates
	}
	got := embedDomainImports(es, "example.com/foo/graph/resolver")
	want := []string{"example.com/foo/graph/resolver/todos", "example.com/foo/graph/resolver/users"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestEmbedDomainImports_Empty(t *testing.T) {
	t.Parallel()
	got := embedDomainImports(nil, "example.com/x")
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

func TestRestoreImpls_FromAST(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "todo.resolvers.go", `package todos

type TodoResolver struct{}

func (r *TodoResolver) User() (string, error) {
	return "from-ast", nil
}
`)
	rw, err := newASTRewriter(dir)
	if err != nil {
		t.Fatalf("newASTRewriter: %v", err)
	}

	todoObj := makeObject("Todo", false)
	methods := []*domainMethodBuild{
		{Object: todoObj, Field: makeFieldWithPos("User", todoObj, todoSchema), ReceiverType: "TodoResolver"},
	}

	restoreImpls(methods, rw, nil)

	want := `return "from-ast", nil`
	if methods[0].Implementation != want {
		t.Errorf("Implementation = %q, want %q", methods[0].Implementation, want)
	}
}

func TestRestoreImpls_FallbackToMigrated(t *testing.T) {
	t.Parallel()
	mutObj := makeObject("Mutation", true)
	methods := []*domainMethodBuild{
		{Object: mutObj, Field: makeFieldWithPos("CreateTodo", mutObj, todoSchema), ReceiverType: "MixinTodosMutation"},
	}
	migrated := map[string]string{
		migratedImplKey("Mutation", "CreateTodo"): `return &model.Todo{}, nil`,
	}

	// rw == nil simulates first-time generation (no domain pkg on disk yet).
	restoreImpls(methods, nil, migrated)

	want := `return &model.Todo{}, nil`
	if methods[0].Implementation != want {
		t.Errorf("Implementation = %q, want %q", methods[0].Implementation, want)
	}
}

func TestRestoreImpls_BothEmpty(t *testing.T) {
	t.Parallel()
	mutObj := makeObject("Mutation", true)
	methods := []*domainMethodBuild{
		{Object: mutObj, Field: makeFieldWithPos("CreateTodo", mutObj, todoSchema), ReceiverType: "MixinTodosMutation"},
	}
	restoreImpls(methods, nil, nil)
	if methods[0].Implementation != "" {
		t.Errorf("expected empty Implementation, got %q", methods[0].Implementation)
	}
}

// TestASTRewriter_RemainingFuncs returns the source of helper functions whose
// receiver/method names weren't claimed by getMethodBody.
func TestASTRewriter_RemainingFuncs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "todo.resolvers.go", `package todos

type TodoResolver struct{}

func (r *TodoResolver) User() string {
	return "claimed"
}

func helper() string {
	return "stays"
}
`)
	rw, err := newASTRewriter(dir)
	if err != nil {
		t.Fatalf("newASTRewriter: %v", err)
	}

	// claim the User method so only the helper survives.
	_ = rw.getMethodBody("TodoResolver", "User")

	got := rw.remainingFuncs(filepath.Join(dir, "todo.resolvers.go"))
	if got == "" {
		t.Fatal("expected helper source, got empty")
	}
	if !strings.Contains(got, "func helper()") {
		t.Errorf("remaining source missing helper(): %s", got)
	}
	if strings.Contains(got, "func (r *TodoResolver) User") {
		t.Errorf("claimed method should not be in remaining source: %s", got)
	}
}

func TestASTRewriter_RemainingFuncs_MissingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "todo.resolvers.go", `package todos
func helper() {}
`)
	rw, err := newASTRewriter(dir)
	if err != nil {
		t.Fatalf("newASTRewriter: %v", err)
	}

	if got := rw.remainingFuncs(filepath.Join(dir, "missing.go")); got != "" {
		t.Errorf("expected empty for missing file, got %q", got)
	}
}
