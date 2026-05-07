package codegen_go

import (
	"testing"

	"zinc-go/internal/parser"
	"zinc-go/internal/typechecker"
)

// boundWithAliases builds a minimal BoundProgram exposing the given
// type aliases, suitable for unit-testing alias-driven codegen helpers
// without spinning up the full Bind pipeline.
func boundWithAliases(aliases map[string]parser.TypeExpr) *typechecker.BoundProgram {
	return &typechecker.BoundProgram{TypeAliases: aliases}
}

// Pure-function tests for the explicit-error helpers introduced in
// the redesign. They have no Generator state, so the tests stay
// fast and don't need any of the cross-package plumbing.

func TestReturnTypeDeclaresError(t *testing.T) {
	intT := &parser.SimpleType{Name: "int"}
	stringT := &parser.SimpleType{Name: "string"}
	errorT := &parser.SimpleType{Name: "error"}

	cases := []struct {
		name string
		t    parser.TypeExpr
		want bool
	}{
		{"nil → false (void)", nil, false},
		{"bare int → false", intT, false},
		{"bare error → true", errorT, true},
		{"tuple (int, string) → false", &parser.TupleType{Elements: []parser.TypeExpr{intT, stringT}}, false},
		{"tuple (int, error) → true", &parser.TupleType{Elements: []parser.TypeExpr{intT, errorT}}, true},
		{"tuple (int, string, error) → true", &parser.TupleType{Elements: []parser.TypeExpr{intT, stringT, errorT}}, true},
		{"tuple (error, int) → false (only tail counts)", &parser.TupleType{Elements: []parser.TypeExpr{errorT, intT}}, false},
		{"empty tuple → false", &parser.TupleType{Elements: nil}, false},
		{"generic List<int> → false", &parser.GenericType{Name: "List", TypeArgs: []parser.TypeExpr{intT}}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := returnTypeDeclaresError(c.t); got != c.want {
				t.Fatalf("returnTypeDeclaresError(%s) = %v, want %v", c.name, got, c.want)
			}
		})
	}
}

func TestIsZincErrorType(t *testing.T) {
	cases := []struct {
		name string
		t    parser.TypeExpr
		want bool
	}{
		{"nil", nil, false},
		{"SimpleType{error}", &parser.SimpleType{Name: "error"}, true},
		{"SimpleType{Error}", &parser.SimpleType{Name: "Error"}, false},
		{"SimpleType{int}", &parser.SimpleType{Name: "int"}, false},
		{"GenericType named error", &parser.GenericType{Name: "error"}, false},
		{"TupleType containing error", &parser.TupleType{Elements: []parser.TypeExpr{&parser.SimpleType{Name: "error"}}}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isZincErrorType(c.t); got != c.want {
				t.Fatalf("isZincErrorType(%s) = %v, want %v", c.name, got, c.want)
			}
		})
	}
}

func TestThrowerValueTypes(t *testing.T) {
	intT := &parser.SimpleType{Name: "int"}
	stringT := &parser.SimpleType{Name: "string"}
	errorT := &parser.SimpleType{Name: "error"}

	t.Run("non-thrower returns nil", func(t *testing.T) {
		if got := throwerValueTypes(intT); got != nil {
			t.Fatalf("expected nil, got %v", got)
		}
	})
	t.Run("bare error → empty slice (void thrower)", func(t *testing.T) {
		got := throwerValueTypes(errorT)
		if len(got) != 0 {
			t.Fatalf("expected empty slice, got %v", got)
		}
	})
	t.Run("(int, error) → [int]", func(t *testing.T) {
		got := throwerValueTypes(&parser.TupleType{Elements: []parser.TypeExpr{intT, errorT}})
		if len(got) != 1 || got[0] != intT {
			t.Fatalf("expected [int], got %v", got)
		}
	})
	t.Run("(int, string, error) → [int, string]", func(t *testing.T) {
		got := throwerValueTypes(&parser.TupleType{Elements: []parser.TypeExpr{intT, stringT, errorT}})
		if len(got) != 2 || got[0] != intT || got[1] != stringT {
			t.Fatalf("expected [int, string], got %v", got)
		}
	})
	t.Run("nil input → nil", func(t *testing.T) {
		if got := throwerValueTypes(nil); got != nil {
			t.Fatalf("expected nil, got %v", got)
		}
	})
}

func TestResolveFuncTypeExpr(t *testing.T) {
	intT := &parser.SimpleType{Name: "int"}
	errorT := &parser.SimpleType{Name: "error"}
	directFn := &parser.FuncTypeExpr{
		Params:     []parser.TypeExpr{intT},
		ReturnType: &parser.TupleType{Elements: []parser.TypeExpr{intT, errorT}},
	}

	t.Run("nil → nil", func(t *testing.T) {
		g := &Generator{}
		if got := g.resolveFuncTypeExpr(nil); got != nil {
			t.Fatalf("expected nil, got %v", got)
		}
	})
	t.Run("direct FuncTypeExpr returns itself", func(t *testing.T) {
		g := &Generator{}
		if got := g.resolveFuncTypeExpr(directFn); got != directFn {
			t.Fatalf("expected identity, got %v", got)
		}
	})
	t.Run("local alias is peeled", func(t *testing.T) {
		g := &Generator{
			bound: boundWithAliases(map[string]parser.TypeExpr{"Factory": directFn}),
		}
		got := g.resolveFuncTypeExpr(&parser.SimpleType{Name: "Factory"})
		if got != directFn {
			t.Fatalf("expected directFn through alias, got %v", got)
		}
	})
	t.Run("subpackage alias is peeled", func(t *testing.T) {
		g := &Generator{
			subpkgTypeAliases: map[string]map[string]parser.TypeExpr{
				"lib": {"Factory": directFn},
			},
		}
		got := g.resolveFuncTypeExpr(&parser.SimpleType{Name: "Factory"})
		if got != directFn {
			t.Fatalf("expected directFn through subpkg alias, got %v", got)
		}
	})
	t.Run("alias chain (alias → alias → fn)", func(t *testing.T) {
		g := &Generator{
			bound: boundWithAliases(map[string]parser.TypeExpr{
				"Outer": &parser.SimpleType{Name: "Inner"},
				"Inner": directFn,
			}),
		}
		got := g.resolveFuncTypeExpr(&parser.SimpleType{Name: "Outer"})
		if got != directFn {
			t.Fatalf("expected directFn through chained aliases, got %v", got)
		}
	})
	t.Run("non-function alias returns nil", func(t *testing.T) {
		g := &Generator{
			bound: boundWithAliases(map[string]parser.TypeExpr{"Name": &parser.SimpleType{Name: "string"}}),
		}
		if got := g.resolveFuncTypeExpr(&parser.SimpleType{Name: "Name"}); got != nil {
			t.Fatalf("expected nil for non-fn alias, got %v", got)
		}
	})
	t.Run("unknown SimpleType returns nil", func(t *testing.T) {
		g := &Generator{}
		if got := g.resolveFuncTypeExpr(&parser.SimpleType{Name: "Unknown"}); got != nil {
			t.Fatalf("expected nil, got %v", got)
		}
	})
}
