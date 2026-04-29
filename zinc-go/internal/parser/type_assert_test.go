package parser

import (
	"testing"

	"zinc-go/internal/lexer"
)

// Tests for `as` type-cast parsing across qualified and unqualified type
// names. Pre-fix the parser only consumed a single token after `as`, so
// `expr as pkg.Type` failed with "unexpected token . in expression" — and
// none of the existing E2E examples crossed the (`as`/`is`)×(qualified type)
// cell. Tests live here so that gap can't reopen silently.

func parseVarStmt(t *testing.T, src string) *VarStmt {
	t.Helper()
	wrapped := "void main() { " + src + " }"
	l := lexer.New(wrapped)
	tokens := l.Tokenize()
	if len(l.Errors) > 0 {
		t.Fatalf("unexpected lex errors: %v", l.Errors)
	}
	p := New(tokens)
	prog := p.ParseV2()
	if len(p.Errors) > 0 {
		t.Fatalf("unexpected parse errors: %v", p.Errors)
	}
	main := prog.Decls[0].(*FnDecl)
	return main.Body.Stmts[0].(*VarStmt)
}

func TestAsCast_UnqualifiedType(t *testing.T) {
	// Regression guard for the existing simple-name form that already worked.
	v := parseVarStmt(t, "var x = obj as IfStmt")
	ta, ok := v.Value.(*TypeAssertExpr)
	if !ok {
		t.Fatalf("expected TypeAssertExpr, got %T", v.Value)
	}
	if ta.TypeName != "IfStmt" {
		t.Errorf("TypeName: expected IfStmt, got %q", ta.TypeName)
	}
	if ta.IsCheck {
		t.Errorf("IsCheck: expected false (as), got true")
	}
}

func TestAsCast_QualifiedType_PackageAlias(t *testing.T) {
	// The fix: the trailing type after `as` parses IDENT (DOT IDENT)* so
	// qualified names round-trip as a single dotted string.
	v := parseVarStmt(t, "var w = f as os.File")
	ta, ok := v.Value.(*TypeAssertExpr)
	if !ok {
		t.Fatalf("expected TypeAssertExpr, got %T", v.Value)
	}
	if ta.TypeName != "os.File" {
		t.Errorf("TypeName: expected os.File, got %q", ta.TypeName)
	}
}

func TestAsCast_QualifiedType_UserAlias(t *testing.T) {
	// Same surface, longer alias — exercises the path zinc-flow's Avro
	// bridge needs (`hs as hambaAvro.RecordSchema`).
	v := parseVarStmt(t, "var r = hs as hambaAvro.RecordSchema")
	ta, ok := v.Value.(*TypeAssertExpr)
	if !ok {
		t.Fatalf("expected TypeAssertExpr, got %T", v.Value)
	}
	if ta.TypeName != "hambaAvro.RecordSchema" {
		t.Errorf("TypeName: expected hambaAvro.RecordSchema, got %q", ta.TypeName)
	}
}

func TestAsCast_TripleNestedQualifiedName(t *testing.T) {
	// Defensive — Zinc doesn't have multi-level packages today, but the
	// loop accepts any tail of `.IDENT`, which keeps the door open for
	// nested-package syntax without needing another parser tweak later.
	v := parseVarStmt(t, "var x = obj as a.b.c")
	ta := v.Value.(*TypeAssertExpr)
	if ta.TypeName != "a.b.c" {
		t.Errorf("TypeName: expected a.b.c, got %q", ta.TypeName)
	}
}

func TestIsCheck_QualifiedType_Parses(t *testing.T) {
	// `is pkg.Type` already parsed (it goes through the binary-expression
	// path where `pkg.Type` parses as a member-access). The companion bug
	// was in codegen — runtime always returned false for qualified types.
	// This test just locks in the parse; the codegen behavior is verified
	// by the E2E example examples/qualified_type_assert.zn.
	v := parseVarStmt(t, "var b = s is hambaAvro.PrimitiveSchema")
	if v.Value == nil {
		t.Fatal("expected expression value, got nil")
	}
}
