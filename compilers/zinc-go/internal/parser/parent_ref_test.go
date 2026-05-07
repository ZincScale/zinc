package parser

import (
	"testing"
)

// Tests for ParentRef — the parent-list shape that retains generic
// type arguments. zinc syntax `class Foo : Bar, Container<T>` used to
// have its `<T>` consumed and discarded by the parser; targets that
// need to propagate the generic args through inheritance (Crystal's
// `include Container(T)`) had no way to recover them. The change
// switched ClassDecl.Parents and DataClassDecl.Parents from []string
// to []ParentRef{Name, TypeArgs}.

func TestParentRef_ClassNoGenericArgs(t *testing.T) {
	prog := parseProg(t, `
class Foo : Bar {
    void hi() {}
}`)
	cls := findClass(t, prog, "Foo")
	if len(cls.Parents) != 1 {
		t.Fatalf("expected 1 parent, got %d", len(cls.Parents))
	}
	if cls.Parents[0].Name != "Bar" {
		t.Errorf("Parents[0].Name = %q, want %q", cls.Parents[0].Name, "Bar")
	}
	if cls.Parents[0].TypeArgs != nil {
		t.Errorf("Parents[0].TypeArgs = %v, want nil", cls.Parents[0].TypeArgs)
	}
}

func TestParentRef_ClassWithGenericArgs(t *testing.T) {
	prog := parseProg(t, `
class Foo<T> : Container<T> {
    void hi() {}
}`)
	cls := findClass(t, prog, "Foo")
	if len(cls.Parents) != 1 {
		t.Fatalf("expected 1 parent, got %d", len(cls.Parents))
	}
	p := cls.Parents[0]
	if p.Name != "Container" {
		t.Errorf("Parents[0].Name = %q, want %q", p.Name, "Container")
	}
	if len(p.TypeArgs) != 1 {
		t.Fatalf("Parents[0].TypeArgs len = %d, want 1", len(p.TypeArgs))
	}
	if simple, ok := p.TypeArgs[0].(*SimpleType); !ok || simple.Name != "T" {
		t.Errorf("Parents[0].TypeArgs[0] = %v, want SimpleType{T}", p.TypeArgs[0])
	}
}

func TestParentRef_ClassMultipleParentsMixedGenerics(t *testing.T) {
	prog := parseProg(t, `
class Foo<K, V> : Bar, Container<K, V>, core.Describable {
    void hi() {}
}`)
	cls := findClass(t, prog, "Foo")
	if len(cls.Parents) != 3 {
		t.Fatalf("expected 3 parents, got %d", len(cls.Parents))
	}
	// 1: Bar (no generics)
	if cls.Parents[0].Name != "Bar" || cls.Parents[0].TypeArgs != nil {
		t.Errorf("Parents[0] = %+v, want {Bar, nil}", cls.Parents[0])
	}
	// 2: Container<K, V>
	if cls.Parents[1].Name != "Container" {
		t.Errorf("Parents[1].Name = %q", cls.Parents[1].Name)
	}
	if len(cls.Parents[1].TypeArgs) != 2 {
		t.Errorf("Parents[1].TypeArgs len = %d, want 2", len(cls.Parents[1].TypeArgs))
	}
	// 3: core.Describable (dotted, no generics)
	if cls.Parents[2].Name != "core.Describable" || cls.Parents[2].TypeArgs != nil {
		t.Errorf("Parents[2] = %+v, want {core.Describable, nil}", cls.Parents[2])
	}
}

func TestParentRef_DataClassWithGenericArgs(t *testing.T) {
	prog := parseProg(t, `
data Pair<A, B>(A first, B second) : Comparable<Pair<A, B>>
`)
	d := findDataClass(t, prog, "Pair")
	if len(d.Parents) != 1 {
		t.Fatalf("expected 1 parent, got %d", len(d.Parents))
	}
	p := d.Parents[0]
	if p.Name != "Comparable" {
		t.Errorf("Parents[0].Name = %q", p.Name)
	}
	if len(p.TypeArgs) != 1 {
		t.Fatalf("expected 1 type arg, got %d", len(p.TypeArgs))
	}
	// Nested generic: Pair<A, B>
	if g, ok := p.TypeArgs[0].(*GenericType); !ok {
		t.Errorf("Parents[0].TypeArgs[0] = %T, want *GenericType", p.TypeArgs[0])
	} else if g.Name != "Pair" || len(g.TypeArgs) != 2 {
		t.Errorf("nested GenericType = %+v, want Pair<A, B>", g)
	}
}

// findClass locates a top-level ClassDecl by name. Helper for the
// ParentRef tests; mirrors the shape of findFn in tuple_type_test.go.
func findClass(t *testing.T, prog *Program, name string) *ClassDecl {
	t.Helper()
	for _, d := range prog.Decls {
		if c, ok := d.(*ClassDecl); ok && c.Name == name {
			return c
		}
	}
	t.Fatalf("class %q not found in program", name)
	return nil
}

func findDataClass(t *testing.T, prog *Program, name string) *DataClassDecl {
	t.Helper()
	for _, d := range prog.Decls {
		if c, ok := d.(*DataClassDecl); ok && c.Name == name {
			return c
		}
	}
	t.Fatalf("data class %q not found in program", name)
	return nil
}
