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

const todoSchema = "/abs/graph/schema/todos/todo.graphqls"
const userSchema = "/abs/graph/schema/users/user.graphqls"

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
	if len(todoGroup.objects) != 1 || todoGroup.objects[0].Name != "Todo" {
		t.Errorf("todo group: expected [Todo] objects, got %v", objectNames(todoGroup.objects))
	}

	userGroup := groups["user"]
	if userGroup == nil {
		t.Fatal("missing 'user' group")
	}
	if len(userGroup.fields) != 1 {
		t.Errorf("user group: expected 1 field, got %d", len(userGroup.fields))
	}
	if len(userGroup.objects) != 1 || userGroup.objects[0].Name != "User" {
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

	if len(build.MutationMethods) != 1 || build.MutationMethods[0].Field.GoFieldName != "CreateTodo" {
		t.Errorf("MutationMethods = %v", build.MutationMethods)
	}
	if len(build.QueryMethods) != 1 || build.QueryMethods[0].Field.GoFieldName != "Todos" {
		t.Errorf("QueryMethods = %v", build.QueryMethods)
	}
	if len(build.SubscriptionMethods) != 1 || build.SubscriptionMethods[0].Field.GoFieldName != "TodoChanged" {
		t.Errorf("SubscriptionMethods = %v", build.SubscriptionMethods)
	}
	if len(build.ObjectMethods) != 1 || build.ObjectMethods[0].Field.GoFieldName != "User" {
		t.Errorf("ObjectMethods = %v", build.ObjectMethods)
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
