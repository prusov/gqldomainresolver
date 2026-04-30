package domainresolver

import (
	"testing"

	"github.com/99designs/gqlgen/codegen"
	gqlast "github.com/vektah/gqlparser/v2/ast"
)

// makeObject creates a minimal codegen.Object for testing.
func makeObject(name string, root bool) *codegen.Object {
	return &codegen.Object{
		Definition: &gqlast.Definition{Name: name},
		Root:       root,
	}
}

// makeFieldWithPos creates a codegen.Field with a schema file position set.
func makeFieldWithPos(goName string, obj *codegen.Object, schemaPath string, args ...*codegen.FieldArgument) *codegen.Field {
	return &codegen.Field{
		FieldDefinition: &gqlast.FieldDefinition{
			Name: goName,
			Position: &gqlast.Position{
				Src: &gqlast.Source{Name: schemaPath},
			},
		},
		GoFieldName: goName,
		Object:      obj,
		Args:        args,
	}
}

// makeObjectWithPos creates a codegen.Object with a schema file position set.
func makeObjectWithPos(name string, root bool, schemaPath string) *codegen.Object {
	return &codegen.Object{
		Definition: &gqlast.Definition{
			Name: name,
			Position: &gqlast.Position{
				Src: &gqlast.Source{Name: schemaPath},
			},
		},
		Root: root,
	}
}

const (
	todoSchema = "/abs/graph/schema/todos/todo.graphqls"
	userSchema = "/abs/graph/schema/users/user.graphqls"
	todoType   = "Todo"
	userType   = "User"
)

func TestGroupBySchemaFile_SingleDomain(t *testing.T) {
	mutObj := makeObject("Mutation", true)
	todoObj := makeObjectWithPos("Todo", false, todoSchema)

	fields := []*domainField{
		{Object: mutObj, Field: makeFieldWithPos("CreateTodo", mutObj, todoSchema)},
		{Object: todoObj, Field: makeFieldWithPos("Something", todoObj, todoSchema)},
	}
	objects := []*codegen.Object{todoObj}

	groups := groupBySchemaFile(fields, objects)

	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	g, ok := groups["todo"]
	if !ok {
		t.Fatal("expected group 'todo'")
	}
	if len(g.fields) != 2 {
		t.Errorf("expected 2 fields, got %d", len(g.fields))
	}
	if len(g.objects) != 1 {
		t.Errorf("expected 1 object, got %d", len(g.objects))
	}
}

func TestGroupBySchemaFile_TwoDomains(t *testing.T) {
	mutObj := makeObject("Mutation", true)
	todoObj := makeObjectWithPos("Todo", false, todoSchema)
	userObj := makeObjectWithPos("User", false, userSchema)

	fields := []*domainField{
		{Object: mutObj, Field: makeFieldWithPos("CreateTodo", mutObj, todoSchema)},
		{Object: mutObj, Field: makeFieldWithPos("CreateUser", mutObj, userSchema)},
		{Object: todoObj, Field: makeFieldWithPos("User", todoObj, todoSchema)},
	}
	objects := []*codegen.Object{todoObj, userObj}

	groups := groupBySchemaFile(fields, objects)

	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d: %v", len(groups), groupKeys(groups))
	}

	todoGroup := groups["todo"]
	if todoGroup == nil {
		t.Fatal("missing 'todo' group")
	}
	if len(todoGroup.fields) != 2 {
		t.Errorf("todo group: expected 2 fields, got %d", len(todoGroup.fields))
	}
	if len(todoGroup.objects) != 1 || todoGroup.objects[0].Name != todoType {
		t.Errorf("todo group: expected [Todo] objects, got %v", objectNames(todoGroup.objects))
	}

	userGroup := groups["user"]
	if userGroup == nil {
		t.Fatal("missing 'user' group")
	}
	if len(userGroup.fields) != 1 {
		t.Errorf("user group: expected 1 field, got %d", len(userGroup.fields))
	}
	if len(userGroup.objects) != 1 || userGroup.objects[0].Name != userType {
		t.Errorf("user group: expected [User] objects, got %v", objectNames(userGroup.objects))
	}
}

// TestGroupBySchemaFile_DeduplicatesObjects verifies the same object is not added twice.
func TestGroupBySchemaFile_DeduplicatesObjects(t *testing.T) {
	todoObj := makeObjectWithPos("Todo", false, todoSchema)

	fields := []*domainField{
		{Object: todoObj, Field: makeFieldWithPos("User", todoObj, todoSchema)},
		{Object: todoObj, Field: makeFieldWithPos("Something", todoObj, todoSchema)},
	}
	// Pass the same object twice to simulate multiple fields on same type.
	objects := []*codegen.Object{todoObj, todoObj}

	groups := groupBySchemaFile(fields, objects)

	g := groups["todo"]
	if g == nil {
		t.Fatal("missing 'todo' group")
	}
	if len(g.objects) != 1 {
		t.Errorf("expected 1 deduplicated object, got %d", len(g.objects))
	}
}

// TestGroupBySchemaFile_Empty returns empty map for no input.
func TestGroupBySchemaFile_Empty(t *testing.T) {
	groups := groupBySchemaFile(nil, nil)
	if len(groups) != 0 {
		t.Errorf("expected empty map, got %d groups", len(groups))
	}
}

// objWithResolverField builds an Object with a single resolver field so that
// HasResolvers() reports true. Used in collectRootCtors tests.
func objWithResolverField(name string, root bool, schemaPath string) *codegen.Object {
	o := makeObjectWithPos(name, root, schemaPath)
	o.Fields = []*codegen.Field{{
		FieldDefinition: &gqlast.FieldDefinition{
			Name:     "f",
			Position: &gqlast.Position{Src: &gqlast.Source{Name: schemaPath}},
		},
		IsResolver: true,
	}}

	return o
}

// TestCollectRootCtors_DisabledDomainGetsRootWrapper verifies that an object
// in a domain dir whose domain is NOT in the allowlist gets a root-package
// ctor + wrapper (so existing root resolvers keep compiling during gradual
// migration).
func TestCollectRootCtors_DisabledDomainGetsRootWrapper(t *testing.T) {
	p := New(WithEnabledDomains("todos"))

	objs := []*codegen.Object{
		objWithResolverField("Todo", false, todoSchema), // todos enabled → skip
		objWithResolverField("User", false, userSchema), // users disabled → wrap
		objWithResolverField("Query", true, todoSchema), // root → skip
		makeObjectWithPos("Subtask", false, userSchema), // no resolvers → skip
	}

	got := p.collectRootCtors(objs)

	if len(got) != 1 {
		t.Fatalf("expected 1 rootCtor, got %d: %+v", len(got), got)
	}
	if got[0].TypeName != userType || got[0].WrapperLc != "userResolver" {
		t.Errorf("got %+v, want {User userResolver}", got[0])
	}
}

// TestCollectRootCtors_NoAllowlistEverythingWraps verifies the empty-allowlist
// case: every non-root object with resolvers becomes a root wrapper. This is
// the entry point for projects just adopting the plugin — no domain has been
// migrated yet, so the plugin must behave like default gqlgen would.
func TestCollectRootCtors_NoAllowlistEverythingWraps(t *testing.T) {
	p := New()

	objs := []*codegen.Object{
		objWithResolverField("Todo", false, todoSchema),
		objWithResolverField("User", false, userSchema),
	}

	got := p.collectRootCtors(objs)

	if len(got) != 2 {
		t.Fatalf("expected 2 rootCtors, got %d: %+v", len(got), got)
	}
	// Sorted alphabetically: Todo, User.
	if got[0].TypeName != "Todo" || got[1].TypeName != "User" {
		t.Errorf("expected [Todo, User], got [%s, %s]", got[0].TypeName, got[1].TypeName)
	}
}

// TestCollectRootCtors_AllEnabledNoRoots — symmetric to the disabled case.
// Sanity check that the original behaviour (all domains migrated → no root
// wrappers) is preserved.
func TestCollectRootCtors_AllEnabledNoRoots(t *testing.T) {
	p := New(WithEnabledDomains("todos", "users"))

	objs := []*codegen.Object{
		objWithResolverField("Todo", false, todoSchema),
		objWithResolverField("User", false, userSchema),
	}

	got := p.collectRootCtors(objs)

	if len(got) != 0 {
		t.Errorf("expected no rootCtors when all domains enabled, got %+v", got)
	}
}

func TestHasRootField(t *testing.T) {
	mutObj := makeObject("Mutation", true)
	queryObj := makeObject("Query", true)
	subObj := makeObject("Subscription", true)
	todoObj := makeObject("Todo", false)

	fields := []*domainField{
		{Object: mutObj, Field: makeFieldWithPos("CreateTodo", mutObj, todoSchema)},
		{Object: todoObj, Field: makeFieldWithPos("User", todoObj, todoSchema)},
	}

	if !hasRootField(fields, "Mutation") {
		t.Error("expected hasRootField Mutation = true")
	}
	if hasRootField(fields, "Query") {
		t.Error("expected hasRootField Query = false")
	}
	if hasRootField(fields, "Subscription") {
		t.Error("expected hasRootField Subscription = false")
	}
	// non-root field with same name shouldn't count.
	if hasRootField([]*domainField{{Object: todoObj, Field: makeFieldWithPos("Mutation", todoObj, todoSchema)}}, "Mutation") {
		t.Error("non-root field must not register as root")
	}

	all := []*domainField{
		{Object: mutObj, Field: makeFieldWithPos("M", mutObj, todoSchema)},
		{Object: queryObj, Field: makeFieldWithPos("Q", queryObj, todoSchema)},
		{Object: subObj, Field: makeFieldWithPos("S", subObj, todoSchema)},
	}
	for _, name := range []string{"Mutation", "Query", "Subscription"} {
		if !hasRootField(all, name) {
			t.Errorf("expected hasRootField %s = true", name)
		}
	}
}

func TestBuildDomainFile(t *testing.T) {
	mutObj := makeObject("Mutation", true)
	queryObj := makeObject("Query", true)
	subObj := makeObject("Subscription", true)
	todoObj := makeObjectWithPos("Todo", false, todoSchema)

	fg := &fileGroup{
		fields: []*domainField{
			{Object: mutObj, Field: makeFieldWithPos("CreateTodo", mutObj, todoSchema)},
			{Object: queryObj, Field: makeFieldWithPos("Todos", queryObj, todoSchema)},
			{Object: subObj, Field: makeFieldWithPos("TodoChanged", subObj, todoSchema)},
			{Object: todoObj, Field: makeFieldWithPos("User", todoObj, todoSchema)},
		},
		objects: []*codegen.Object{todoObj},
	}

	build := buildDomainFile(fg)

	wantOrder := []struct {
		Object string
		Field  string
	}{
		{"Mutation", "CreateTodo"},
		{"Query", "Todos"},
		{"Subscription", "TodoChanged"},
		{"Todo", "User"},
	}
	if len(build.Methods) != len(wantOrder) {
		t.Fatalf("Methods len = %d, want %d", len(build.Methods), len(wantOrder))
	}
	for i, w := range wantOrder {
		got := build.Methods[i]
		if got.Object.Name != w.Object || got.Field.GoFieldName != w.Field {
			t.Errorf("Methods[%d] = %s.%s, want %s.%s", i, got.Object.Name, got.Field.GoFieldName, w.Object, w.Field)
		}
	}
	if len(build.Objects) != 1 || build.Objects[0].Name != "Todo" {
		t.Errorf("Objects = %v", build.Objects)
	}
}

// helpers for readable error messages

func groupKeys(m map[string]*fileGroup) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func objectNames(objs []*codegen.Object) []string {
	names := make([]string, len(objs))
	for i, o := range objs {
		names[i] = o.Name
	}
	return names
}
