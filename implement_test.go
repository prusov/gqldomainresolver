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

// makeField creates a minimal codegen.Field for testing.
func makeField(goName string, obj *codegen.Object, args ...*codegen.FieldArgument) *codegen.Field {
	return &codegen.Field{
		FieldDefinition: &gqlast.FieldDefinition{Name: goName},
		GoFieldName:     goName,
		Object:          obj,
		Args:            args,
	}
}

// makeArg creates a minimal FieldArgument for testing.
func makeArg(varName string) *codegen.FieldArgument {
	return &codegen.FieldArgument{
		ArgumentDefinition: &gqlast.ArgumentDefinition{Name: varName},
		VarName:            varName,
	}
}

func TestDelegationArgs_MutationNoArgs(t *testing.T) {
	obj := makeObject("Mutation", true)
	field := makeField("Hello", obj)

	got := delegationArgs(field)
	want := "ctx"
	if got != want {
		t.Errorf("delegationArgs = %q, want %q", got, want)
	}
}

func TestDelegationArgs_MutationWithArgs(t *testing.T) {
	obj := makeObject("Mutation", true)
	field := makeField("CreateTodo", obj, makeArg("input"))

	got := delegationArgs(field)
	want := "ctx, input"
	if got != want {
		t.Errorf("delegationArgs = %q, want %q", got, want)
	}
}

func TestDelegationArgs_MutationMultipleArgs(t *testing.T) {
	obj := makeObject("Mutation", true)
	field := makeField("CreateUser", obj, makeArg("name"), makeArg("email"))

	got := delegationArgs(field)
	want := "ctx, name, email"
	if got != want {
		t.Errorf("delegationArgs = %q, want %q", got, want)
	}
}

func TestDelegationArgs_FieldResolverAddsObj(t *testing.T) {
	obj := makeObject("Todo", false)
	field := makeField("User", obj) // no extra args

	got := delegationArgs(field)
	want := "ctx, obj"
	if got != want {
		t.Errorf("delegationArgs = %q, want %q", got, want)
	}
}

func TestDelegationArgs_FieldResolverWithArgs(t *testing.T) {
	obj := makeObject("Todo", false)
	field := makeField("Something", obj, makeArg("filter"))

	got := delegationArgs(field)
	want := "ctx, obj, filter"
	if got != want {
		t.Errorf("delegationArgs = %q, want %q", got, want)
	}
}

func TestBuildDelegation_Mutation(t *testing.T) {
	obj := makeObject("Mutation", true)
	field := makeField("CreateTodo", obj, makeArg("input"))

	got := buildDelegation("todos", field)
	want := "return todos.MutationCreateTodo(ctx, input)"
	if got != want {
		t.Errorf("buildDelegation = %q, want %q", got, want)
	}
}

func TestBuildDelegation_Query(t *testing.T) {
	obj := makeObject("Query", true)
	field := makeField("Todos", obj)

	got := buildDelegation("todos", field)
	want := "return todos.QueryTodos(ctx)"
	if got != want {
		t.Errorf("buildDelegation = %q, want %q", got, want)
	}
}

func TestBuildDelegation_FieldResolver(t *testing.T) {
	obj := makeObject("Todo", false)
	field := makeField("Something", obj)

	got := buildDelegation("todos", field)
	want := "return (&todos.TodoResolver{}).Something(ctx, obj)"
	if got != want {
		t.Errorf("buildDelegation = %q, want %q", got, want)
	}
}

func TestBuildDelegation_FieldResolverDifferentDomain(t *testing.T) {
	obj := makeObject("User", false)
	field := makeField("Something", obj)

	got := buildDelegation("users", field)
	want := "return (&users.UserResolver{}).Something(ctx, obj)"
	if got != want {
		t.Errorf("buildDelegation = %q, want %q", got, want)
	}
}

func TestBuildDelegation_QueryWithArgs(t *testing.T) {
	obj := makeObject("Query", true)
	field := makeField("TodosByUser", obj, makeArg("userID"))

	got := buildDelegation("todos", field)
	want := "return todos.QueryTodosByUser(ctx, userID)"
	if got != want {
		t.Errorf("buildDelegation = %q, want %q", got, want)
	}
}
