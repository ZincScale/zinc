package parser

import (
	"testing"

	"zinc-go/internal/lexer"
)

// Tests for the multi-value-return TupleType surface added by the
// error-system redesign (commit f77a9d5). These exercise the parser
// at the public entry point — fewer mocks, more reflective of how
// the parser is actually used.

func parseProg(t *testing.T, src string) *Program {
	t.Helper()
	l := lexer.New(src)
	tokens := l.Tokenize()
	if len(l.Errors) > 0 {
		t.Fatalf("unexpected lex errors: %v", l.Errors)
	}
	p := New(tokens)
	prog := p.ParseV2()
	if len(p.Errors) > 0 {
		t.Fatalf("unexpected parse errors: %v", p.Errors)
	}
	return prog
}

func TestTupleReturnType_TwoValues(t *testing.T) {
	prog := parseProg(t, `
		(Int, String) makeUser() {
			return 42, "alice"
		}
	`)
	fn := prog.Decls[0].(*FnDecl)
	tup, ok := fn.ReturnType.(*TupleType)
	if !ok {
		t.Fatalf("expected TupleType return, got %T", fn.ReturnType)
	}
	if len(tup.Elements) != 2 {
		t.Fatalf("expected 2 tuple elements, got %d", len(tup.Elements))
	}
	if name := tup.Elements[0].(*SimpleType).Name; name != "Int" {
		t.Errorf("element[0]: expected Int, got %s", name)
	}
	if name := tup.Elements[1].(*SimpleType).Name; name != "String" {
		t.Errorf("element[1]: expected String, got %s", name)
	}
}

func TestTupleReturnType_WithErrorTail(t *testing.T) {
	prog := parseProg(t, `
		(Int, String, error) lookup(String key) {
			return 7, "found", null
		}
	`)
	fn := prog.Decls[0].(*FnDecl)
	tup, ok := fn.ReturnType.(*TupleType)
	if !ok {
		t.Fatalf("expected TupleType, got %T", fn.ReturnType)
	}
	if len(tup.Elements) != 3 {
		t.Fatalf("expected 3 tuple elements, got %d", len(tup.Elements))
	}
	if name := tup.Elements[2].(*SimpleType).Name; name != "error" {
		t.Errorf("element[2]: expected error, got %s", name)
	}
}

func TestTupleReturnType_BareError(t *testing.T) {
	prog := parseProg(t, `
		error validate(String s) {
			return null
		}
	`)
	fn := prog.Decls[0].(*FnDecl)
	if _, ok := fn.ReturnType.(*TupleType); ok {
		t.Fatalf("bare error must not parse as TupleType, got TupleType")
	}
	st, ok := fn.ReturnType.(*SimpleType)
	if !ok {
		t.Fatalf("expected SimpleType, got %T", fn.ReturnType)
	}
	if st.Name != "error" {
		t.Errorf("expected name 'error', got %q", st.Name)
	}
}

func TestTupleReturnType_SingletonCollapses(t *testing.T) {
	// `(Int)` with one element should unwrap to plain Int — both
	// `pub Int foo()` and `pub (Int) foo()` produce the same AST.
	prog := parseProg(t, `
		(Int) compute() {
			return 5
		}
	`)
	fn := prog.Decls[0].(*FnDecl)
	if _, ok := fn.ReturnType.(*TupleType); ok {
		t.Fatalf("singleton (Int) must collapse to SimpleType, got TupleType")
	}
	st, ok := fn.ReturnType.(*SimpleType)
	if !ok {
		t.Fatalf("expected SimpleType, got %T", fn.ReturnType)
	}
	if st.Name != "Int" {
		t.Errorf("expected name 'Int', got %q", st.Name)
	}
}

func TestFnTypeWithTupleReturn(t *testing.T) {
	// `Fn<(Int), (String, error)>` — the Fn slot's second arg parses
	// through v2ParseTypeOrTuple, so the return shape is a TupleType.
	prog := parseProg(t, `
		void main() {
			Fn<(Int), (String, error)> f
		}
	`)
	main := prog.Decls[0].(*FnDecl)
	varStmt := main.Body.Stmts[0].(*VarStmt)
	fnT, ok := varStmt.Type.(*FuncTypeExpr)
	if !ok {
		t.Fatalf("expected FuncTypeExpr type, got %T", varStmt.Type)
	}
	if len(fnT.Params) != 1 {
		t.Fatalf("expected 1 param, got %d", len(fnT.Params))
	}
	tup, ok := fnT.ReturnType.(*TupleType)
	if !ok {
		t.Fatalf("expected TupleType return, got %T", fnT.ReturnType)
	}
	if len(tup.Elements) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(tup.Elements))
	}
	if name := tup.Elements[1].(*SimpleType).Name; name != "error" {
		t.Errorf("expected trailing error, got %s", name)
	}
}

func TestMethodTupleReturnType(t *testing.T) {
	// Same surface but on a class method.
	prog := parseProg(t, `
		class Counter {
			Int n
			init(Int n) { this.n = n }

			pub (Int, String, error) describe() {
				return this.n, "count", null
			}
		}
	`)
	cls := prog.Decls[0].(*ClassDecl)
	if len(cls.Methods) != 1 {
		t.Fatalf("expected 1 method, got %d", len(cls.Methods))
	}
	m := cls.Methods[0]
	tup, ok := m.ReturnType.(*TupleType)
	if !ok {
		t.Fatalf("expected TupleType, got %T", m.ReturnType)
	}
	if len(tup.Elements) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(tup.Elements))
	}
}

func TestPubTupleReturnType(t *testing.T) {
	// The `pub` dispatcher had to learn about `(...)` as a fn-decl lead.
	// Without v2SkipBalancedParens, this parsed as "expected
	// function/const/class/data/interface after pub".
	prog := parseProg(t, `
		pub (Int, error) parseNum(String s) {
			return 0, null
		}
	`)
	fn := prog.Decls[0].(*FnDecl)
	if !fn.IsPub {
		t.Errorf("expected IsPub=true")
	}
	if _, ok := fn.ReturnType.(*TupleType); !ok {
		t.Fatalf("expected TupleType, got %T", fn.ReturnType)
	}
}
