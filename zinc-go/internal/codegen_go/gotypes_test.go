package codegen_go

import (
	"os"
	"path/filepath"
	"testing"
)

// Tests for HasPointerReceiverMethods's AST fallback. The type-checker
// path (loadPkg + types.NewMethodSet) only fires when the dep is fully
// loaded into the build cache; for fresh third-party deps zinc just
// transpiled, we rely on the AST fallback. Pre-fix the AST path didn't
// exist, so qualified types from third-party packages silently failed
// to pointerize — `[]pkg.Foo` instead of `[]*pkg.Foo` — and codegen
// emitted broken Go (e.g. `append([]Foo, *Foo)` after the constructor
// returned a pointer).

// writeFakePkg writes a single .go file at <root>/<pkgPath>/types.go
// with the given source. Returns the root dir to feed into SetDir.
func writeFakePkg(t *testing.T, pkgPath, src string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, pkgPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "types.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return root
}

func TestHasPointerReceiverMethods_ASTFallback_PointerReceiver(t *testing.T) {
	root := writeFakePkg(t, "fakepkg", `package fakepkg

type Watcher struct{ events chan int }

func NewWatcher() *Watcher { return &Watcher{} }
func (w *Watcher) Close() error { return nil }
`)
	r := NewGoTypeResolver()
	r.SetDir(root)
	if !r.HasPointerReceiverMethods("fakepkg", "Watcher") {
		t.Fatalf("expected AST fallback to detect pointer receiver on *Watcher.Close")
	}
}

func TestHasPointerReceiverMethods_ASTFallback_ValueReceiverOnly(t *testing.T) {
	root := writeFakePkg(t, "fakepkg", `package fakepkg

type Point struct{ X, Y int }

func (p Point) Length() int { return p.X + p.Y }
`)
	r := NewGoTypeResolver()
	r.SetDir(root)
	if r.HasPointerReceiverMethods("fakepkg", "Point") {
		t.Fatalf("expected false — Point only has a value receiver")
	}
}

func TestHasPointerReceiverMethods_ASTFallback_NoMethods(t *testing.T) {
	root := writeFakePkg(t, "fakepkg", `package fakepkg

type Bare struct{ Field string }
`)
	r := NewGoTypeResolver()
	r.SetDir(root)
	if r.HasPointerReceiverMethods("fakepkg", "Bare") {
		t.Fatalf("expected false — Bare has no methods at all")
	}
}

func TestHasPointerReceiverMethods_ASTFallback_MixedReceivers(t *testing.T) {
	// Realistic shape: most third-party libs put state-mutating methods on
	// *T and read-only convenience methods on T. As long as one pointer
	// receiver exists, the type's intended-use is *T and we must pointerize.
	root := writeFakePkg(t, "fakepkg", `package fakepkg

type Schema struct{ name string }

func NewSchema(name string) *Schema { return &Schema{name: name} }
func (s Schema) Name() string         { return s.name }   // value
func (s *Schema) SetName(n string)    { s.name = n }      // pointer
`)
	r := NewGoTypeResolver()
	r.SetDir(root)
	if !r.HasPointerReceiverMethods("fakepkg", "Schema") {
		t.Fatalf("expected true — Schema has SetName on *Schema")
	}
}

func TestHasPointerReceiverMethods_ASTFallback_TypeNotFound(t *testing.T) {
	root := writeFakePkg(t, "fakepkg", `package fakepkg

type Other struct{}
func (o *Other) Do() {}
`)
	r := NewGoTypeResolver()
	r.SetDir(root)
	if r.HasPointerReceiverMethods("fakepkg", "Missing") {
		t.Fatalf("expected false — Missing isn't declared")
	}
}

func TestHasPointerReceiverMethods_ASTFallback_PackageNotFound(t *testing.T) {
	r := NewGoTypeResolver()
	r.SetDir(t.TempDir())
	if r.HasPointerReceiverMethods("doesnotexist/atall", "Anything") {
		t.Fatalf("expected false — package can't be loaded")
	}
}

// IsInterface needs the same AST fallback as HasPointerReceiverMethods so
// that zeroValueFor can emit `nil` instead of `pkg.Iface{}` for interface
// return types whose package can't be type-loaded yet.

func TestIsInterface_ASTFallback_Interface(t *testing.T) {
	root := writeFakePkg(t, "fakepkg", `package fakepkg

type Schema interface {
	Type() string
	String() string
}
`)
	r := NewGoTypeResolver()
	r.SetDir(root)
	if !r.IsInterface("fakepkg", "Schema") {
		t.Fatalf("expected AST fallback to detect Schema as an interface")
	}
}

func TestIsInterface_ASTFallback_Struct(t *testing.T) {
	root := writeFakePkg(t, "fakepkg", `package fakepkg

type Record struct{ Name string }
`)
	r := NewGoTypeResolver()
	r.SetDir(root)
	if r.IsInterface("fakepkg", "Record") {
		t.Fatalf("expected false — Record is a struct, not an interface")
	}
}

func TestIsInterface_ASTFallback_TypeNotFound(t *testing.T) {
	root := writeFakePkg(t, "fakepkg", `package fakepkg

type Other struct{}
`)
	r := NewGoTypeResolver()
	r.SetDir(root)
	if r.IsInterface("fakepkg", "Missing") {
		t.Fatalf("expected false — Missing isn't declared")
	}
}

// NeedsPointerArg now returns true only when the Go signature has an
// explicit *T at the given index. The old hand-curated table for
// `any`-typed runtime-pointer params (json/xml/avro Unmarshal, fmt.Scan
// family) is gone — those cases require the user to write `&x` at the
// call site (the FFI-only address-of operator). This test pins the new
// contract: previously-tabled funcs no longer auto-report needs-pointer.
func TestNeedsPointerArg_NoImplicitTable(t *testing.T) {
	r := NewGoTypeResolver()
	cases := []struct {
		pkg, fn string
		idx     int
		why     string
	}{
		{"encoding/json", "Unmarshal", 1, "any-typed runtime-pointer; explicit & required at call site"},
		{"encoding/xml", "Unmarshal", 1, "any-typed runtime-pointer; explicit & required at call site"},
		{"fmt", "Scan", 0, "variadic any, explicit & required at call site"},
		{"fmt", "Sscanf", 2, "variadic any, explicit & required at call site"},
		{"github.com/hamba/avro/v2", "Unmarshal", 2, "any-typed runtime-pointer; explicit & required at call site"},
		{"fmt", "Println", 0, "variadic any, never needed pointer"},
	}
	for _, c := range cases {
		if r.NeedsPointerArg(c.pkg, c.fn, c.idx) {
			t.Errorf("NeedsPointerArg(%q, %q, %d) = true, want false (%s)",
				c.pkg, c.fn, c.idx, c.why)
		}
	}
}
