// Package codegen_cr generates Crystal source code from a Zinc AST.
//
// Mirrors zinc-go/internal/codegen_go/ in shape. The Generator struct
// owns emit state (output buffer, indent level, in-flight class
// context, requireSet for `require` lines, etc.); a single
// GenerateFiles call walks the AST and produces one .cr per .zn input.
//
// Status: SKETCH. Smallest meaningful slice — top-level statements,
// function declarations with primitive params/returns, the `print`
// builtin, string interpolation. Enough to lower phase0/zn/hello.zn
// to phase0/cr/hello.cr byte-equivalent. Everything else is TODO.
package codegen_cr

import (
	"fmt"
	"strings"

	"zinc-go/parser"
)

// Generator emits Crystal source from a parsed zinc Program.
//
// Field-by-field intent (see PLAN §6.4 for the full anticipated set,
// most of which lands as we grow past the hello-world slice):
//
//   - buf, indent: output buffer and current indent depth.
//   - className: target binary name; drives `def main` wrap and the
//     trailing `main` invocation. Comes from zinc.toml [package].name.
//   - requireSet: shards that need a top-of-file `require "<x>"`.
//     Populated as codegen encounters constructs that demand them
//     (e.g. `concurrent { }` → `require "wait_group"`).
//   - inConcurrentBlock + concurrencyOwnerDepth: the §1.4 spawn-owner
//     tracker. Validation of bare `spawn` (compile error if outside
//     an owner) and lowering decision (wg.spawn vs Isolated) hang off
//     this. Not used yet at the hello-world slice but reserved.
//   - compileErrors: buffer for fatal codegen errors. The driver
//     surfaces these and exits non-zero — same shape as zinc-go's
//     compileError pattern.
type Generator struct {
	buf       strings.Builder
	indent    int
	className string

	// Crystal-target specifics
	requireSet              map[string]bool
	inConcurrentBlock       bool
	concurrencyOwnerDepth   int
	currentOwnerKind        string // "concurrent" / "parallel" / "task" / ""

	// currentClassFields is the field name set of the class currently
	// being emitted. Drives the implicit-self lowering: bare `x = 1`
	// inside a method body becomes `@x = 1` when `x` is in this set.
	// Cleared when class emit finishes. nil outside any class scope.
	currentClassFields map[string]bool

	// currentFnIsThrower is true while emitting the body of a function
	// whose declared return type ends in `error` (bare or trailing in
	// a tuple). Drives `return ErrorExpr` → `raise ErrorExpr` and
	// `return v, null` → `return v` rewrites. Saved/restored across
	// nested function emits.
	currentFnIsThrower bool

	// classes tracks all class names declared in this Program. Used
	// by emitCall to discriminate `Foo(args)` (constructor → Foo.new(args))
	// from `someFn(args)` (regular call). Populated in a pre-pass over
	// prog.Decls before body emission.
	classes map[string]bool

	// interfaces tracks names declared as `interface Foo` in this
	// Program. Used in emitClassDecl to choose `include` vs `<` for
	// each entry in Parents — Crystal modules (which interfaces
	// lower to) require `include`, real classes use `<`.
	interfaces map[string]bool

	// enumMembers maps a variant name (e.g. "Red") to the enum it
	// belongs to (e.g. "Color"). zinc lets users write bare `Red`;
	// Crystal requires the qualified form `Color::Red` everywhere
	// (variable assignments, match patterns, comparisons).
	enumMembers map[string]string

	// sealedVariants maps a sealed-variant name (e.g. "Circle") to
	// its parent sealed class name (e.g. "Shape") and field-name list.
	// Drives match lowering: when a `case Circle(r)` pattern appears,
	// codegen knows the case-in arm needs `Shape::Circle` (qualified)
	// and that `r` binds to the first field (from the DataClassDecl's
	// Params).
	sealedVariants map[string]sealedVariantInfo

	// needsZincFmt is set when emit produces any string interpolation
	// expression. Drives whether we prepend the zinc_fmt helper at the
	// top of the file. The helper exists to fix phase0/MISMATCH §4 —
	// Crystal's Float64#to_s prints "12.0" where Go's %v prints "12".
	// Wrapping every interpolated expression in zinc_fmt(...) trims
	// the trailing ".0" from integer-valued floats, matching zinc-go's
	// expected/<name>.txt outputs byte-for-byte.
	needsZincFmt bool

	// Per-file errors. Non-empty means the build fails; the driver
	// prints these and exits non-zero.
	compileErrors []string
}

type sealedVariantInfo struct {
	Parent string   // sealed class name (e.g. "Shape")
	Fields []string // field names in declaration order
}

// New returns a fresh Generator with all maps initialized.
func New() *Generator {
	return &Generator{
		requireSet:     make(map[string]bool),
		classes:        make(map[string]bool),
		interfaces:     make(map[string]bool),
		sealedVariants: make(map[string]sealedVariantInfo),
		enumMembers:    make(map[string]string),
	}
}

// CompileErrors returns any fatal errors recorded during codegen.
// Non-empty result must cause the driver to fail the build.
func (g *Generator) CompileErrors() []string { return g.compileErrors }

// SetClassName sets the Crystal target binary's basename. Used both
// for the `def main` / trailing-`main` shape and (eventually) for
// any naming that derives from the binary's identity. Conventionally
// matches the project name from zinc.toml.
func (g *Generator) SetClassName(name string) { g.className = name }

// OutputFile is the (filename, content) pair the driver writes to disk.
// Mirrors zinc-go's same struct. zinc-crystal currently emits a single
// file per .zn input — multi-file projects work because each .zn
// becomes its own .cr (no merging needed at this stage).
type OutputFile struct {
	Name    string
	Content string
}

// GenerateFiles walks the parsed Program and returns the .cr files
// to write to disk. SKETCH: emits one src/<className>.cr that
// contains every decl + a `def main` wrapping any top-level stmts.
//
// The hello-world target is reached when this lowers a Program with
// one FnDecl `main` (zero params, void return, body of one print
// call) into the exact byte sequence of phase0/cr/hello.cr (modulo
// the leading hand-written comment).
func (g *Generator) GenerateFiles(prog *parser.Program) []OutputFile {
	g.buf.Reset()
	g.indent = 0

	// Pre-pass: collect class names so emitCall can rewrite Foo(...)
	// to Foo.new(...). Has to happen before body emission since a
	// constructor call to Bar may appear in a class declared earlier
	// in the file. Sealed-class variants register both as constructable
	// classes and as match-pattern targets.
	for _, d := range prog.Decls {
		switch decl := d.(type) {
		case *parser.ClassDecl:
			g.classes[decl.Name] = true
			if decl.IsSealed {
				for _, v := range decl.Variants {
					g.classes[v.Name] = true
					fields := make([]string, 0, len(v.Params))
					for _, p := range v.Params {
						fields = append(fields, p.Name)
					}
					g.sealedVariants[v.Name] = sealedVariantInfo{
						Parent: decl.Name,
						Fields: fields,
					}
				}
			}
		case *parser.DataClassDecl:
			g.classes[decl.Name] = true
		case *parser.InterfaceDecl:
			g.interfaces[decl.Name] = true
		case *parser.EnumDecl:
			for _, v := range decl.Variants {
				g.enumMembers[v] = decl.Name
			}
		}
	}

	// Walk decls into a body buffer so we can emit `require` lines at
	// the top once requireSet is finalized. Same trick zinc-go uses
	// (separate body buffer, then prepend imports).
	bodyGen := *g
	bodyGen.buf.Reset()
	for _, d := range prog.Decls {
		bodyGen.emitDecl(d)
		bodyGen.writeln("")
	}
	// Top-level statements wrap into `def main; ...; end` + trailing
	// `main` invocation, only when the program has no explicit `main`
	// FnDecl. SKETCH: hello.zn declares `void main() { ... }`, so this
	// branch doesn't fire today — but it's the same shape as zinc-go.
	hasExplicitMain := false
	for _, d := range prog.Decls {
		if fn, ok := d.(*parser.FnDecl); ok && fn.Name == "main" {
			hasExplicitMain = true
			break
		}
	}
	if len(prog.Stmts) > 0 && !hasExplicitMain {
		bodyGen.writeln("def main : Nil")
		bodyGen.indent++
		for _, s := range prog.Stmts {
			bodyGen.emitStmt(s)
		}
		bodyGen.indent--
		bodyGen.writeln("end")
		bodyGen.writeln("")
	}

	// Trailing `main` invocation — only when this file actually has a
	// main entry point (explicit `void main()` decl or top-level stmts
	// that we wrapped). Helper / module files don't need it.
	if hasExplicitMain || (len(prog.Stmts) > 0 && !hasExplicitMain) {
		bodyGen.writeln("main")
	}

	// Propagate state changes from the body pass back to the outer
	// Generator (Crystal-side requires accumulate during emit, errors
	// likewise).
	for k := range bodyGen.requireSet {
		g.requireSet[k] = true
	}
	g.compileErrors = append(g.compileErrors, bodyGen.compileErrors...)
	if bodyGen.needsZincFmt {
		g.needsZincFmt = true
	}

	// Assemble final file: requires + zinc_fmt helper (if used) + body.
	for k := range g.requireSet {
		g.writeln(`require "%s"`, k)
	}
	if len(g.requireSet) > 0 {
		g.writeln("")
	}
	if g.needsZincFmt {
		g.emitZincFmtHelper()
		g.writeln("")
	}
	g.buf.WriteString(bodyGen.buf.String())

	name := g.className
	if name == "" {
		name = "main"
	}
	return []OutputFile{{Name: name + ".cr", Content: g.buf.String()}}
}

// --- Emit helpers ---------------------------------------------------------

// writeln writes a single line with current indent + a trailing newline.
// Empty format produces a bare newline (used as a separator).
func (g *Generator) writeln(format string, args ...any) {
	if format != "" {
		for i := 0; i < g.indent; i++ {
			g.buf.WriteString("  ")
		}
		fmt.Fprintf(&g.buf, format, args...)
	}
	g.buf.WriteByte('\n')
}

// compileError records a fatal codegen error. Format mirrors zinc-go's
// `<file>:<line>: <message>` shape — for now the line is whatever the
// AST node carries (zero when the node is a UnaryExpr or other
// line-less type).
func (g *Generator) compileError(line int, format string, args ...any) {
	g.compileErrors = append(g.compileErrors,
		fmt.Sprintf("line %d: %s", line, fmt.Sprintf(format, args...)))
}

// --- Declaration dispatch ------------------------------------------------

func (g *Generator) emitDecl(d parser.TopLevelDecl) {
	switch decl := d.(type) {
	case *parser.FnDecl:
		g.emitFnDecl(decl)
	case *parser.ClassDecl:
		g.emitClassDecl(decl)
	case *parser.DataClassDecl:
		g.emitTopLevelDataClass(decl)
	case *parser.EnumDecl:
		g.emitEnumDecl(decl)
	case *parser.ConstDecl:
		g.emitConstDecl(decl)
	case *parser.InterfaceDecl:
		g.emitInterfaceDecl(decl)
	case *parser.TypeAliasDecl:
		// `type Foo = Bar` → Crystal's `alias Foo = Bar`. Both have
		// the same semantics — Foo and Bar are interchangeable in
		// type position.
		g.writeln("alias %s = %s", decl.Name, g.crType(decl.Type))
	default:
		g.compileError(0, "codegen_cr: %T not implemented yet", decl)
	}
}

// emitTopLevelDataClass lowers `data Foo(int x, String y)` declared at
// the top level (not as a sealed variant). Same shape as a sealed
// variant minus the parent class — plain class + getters + initialize +
// to_s in the zinc-go printable format.
func (g *Generator) emitTopLevelDataClass(d *parser.DataClassDecl) {
	header := "class " + d.Name
	if len(d.Parents) > 0 {
		header += " < " + d.Parents[0]
	}
	g.writeln(header)
	g.indent++
	for _, p := range d.Params {
		g.writeln("getter %s : %s", p.Name, g.crType(p.Type))
	}
	if len(d.Params) > 0 {
		params := make([]string, 0, len(d.Params))
		for _, p := range d.Params {
			params = append(params, fmt.Sprintf("@%s : %s", p.Name, g.crType(p.Type)))
		}
		g.writeln("")
		g.writeln("def initialize(%s)", strings.Join(params, ", "))
		g.writeln("end")
		g.needsZincFmt = true
		g.writeln("")
		g.writeln("def to_s(io : IO) : Nil")
		g.indent++
		g.writeln(`io << "%s("`, d.Name)
		for i, p := range d.Params {
			if i > 0 {
				g.writeln(`io << ", "`)
			}
			g.writeln(`io << "%s=" << zinc_fmt(@%s)`, p.Name, p.Name)
		}
		g.writeln(`io << ")"`)
		g.indent--
		g.writeln("end")
	}
	for _, m := range d.Methods {
		g.writeln("")
		synth := &parser.ClassDecl{Name: d.Name}
		for _, p := range d.Params {
			synth.Fields = append(synth.Fields, &parser.FieldDecl{Name: p.Name, Type: p.Type})
		}
		g.emitMethod(synth, m)
	}
	g.indent--
	g.writeln("end")
	g.classes[d.Name] = true
}

// emitEnumDecl lowers `enum Color { Red, Green, Blue }` to Crystal's
// `enum Color; Red; Green; Blue; end`. zinc enums today are
// value-less (just discriminants); Crystal enums get integer backing
// automatically. Match patterns over enum members work natively.
func (g *Generator) emitEnumDecl(e *parser.EnumDecl) {
	g.writeln("enum %s", e.Name)
	g.indent++
	for _, v := range e.Variants {
		g.writeln("%s", v)
	}
	g.indent--
	g.writeln("end")
}

// emitConstDecl lowers `[pub] const X[: T] = expr` to Crystal's
// top-level `X = expr`. Crystal constants are by-convention SCREAMING_CASE
// but case isn't enforced; emit the user's exact name. Type annotation
// lands as a comment for now (Crystal infers from the value).
func (g *Generator) emitConstDecl(c *parser.ConstDecl) {
	g.writeln("%s = %s", c.Name, g.emitExpr(c.Value))
}

// emitInterfaceDecl lowers `interface Speaker { void greet() }` to
// Crystal's `module Speaker` with `abstract def` declarations.
// Implementing classes do `include Speaker` (lowered when emitting
// classes whose Parents includes an interface name — TODO).
func (g *Generator) emitInterfaceDecl(i *parser.InterfaceDecl) {
	g.writeln("module %s", i.Name)
	g.indent++
	for _, m := range i.Methods {
		params := make([]string, 0, len(m.Params))
		for _, p := range m.Params {
			params = append(params, fmt.Sprintf("%s : %s", p.Name, g.crType(p.Type)))
		}
		ret := g.crType(m.ReturnType)
		if ret == "" {
			ret = "Nil"
		}
		name := crMethodName(m.Name)
		if len(m.Params) == 0 {
			g.writeln("abstract def %s : %s", name, ret)
		} else {
			g.writeln("abstract def %s(%s) : %s", name, strings.Join(params, ", "), ret)
		}
	}
	g.indent--
	g.writeln("end")
}

// emitClassDecl dispatches on whether the class is sealed. Sealed
// classes get a totally different shape (abstract base + nested
// variant classes); plain classes go through emitPlainClass.
func (g *Generator) emitClassDecl(c *parser.ClassDecl) {
	if c.IsSealed {
		g.emitSealedClass(c)
		return
	}
	g.emitPlainClass(c)
}

// emitSealedClass lowers a sealed class to PLAN §4.3 Option A:
// abstract base + concrete subclasses scoped under the base's
// namespace. Per phase0's confirmation that Crystal's `case in`
// doesn't accept this encoding as exhaustive, every match over a
// sealed subject gets a runtime `in <Base>; raise` arm — emitted
// here as a marker on the class, consumed by emitMatch.
//
// Shape:
//   abstract class Shape
//     # base methods (shared across variants)
//   end
//   class Shape::Circle < Shape
//     getter radius : Float64
//     def initialize(@radius : Float64); end
//   end
//   class Shape::Rect < Shape
//     getter width : Float64
//     getter height : Float64
//     def initialize(@width : Float64, @height : Float64); end
//   end
//
// SKETCH: no `to_s` override yet — Crystal's default prints
// `#<Shape::Circle:0x...>`. Fixing this needs a Float64-formatter
// helper (phase0/MISMATCH §4) which is a separate slice. For now,
// `print(c)` on a variant prints the default repr. Computed values
// from match (`area(c)`, `describe(c)`) work fine.
func (g *Generator) emitSealedClass(c *parser.ClassDecl) {
	g.writeln("abstract class %s", c.Name)
	g.indent++
	for _, m := range c.Methods {
		g.emitMethod(c, m)
	}
	g.indent--
	g.writeln("end")
	g.writeln("")

	// Emit each variant.
	for i, v := range c.Variants {
		if i > 0 {
			g.writeln("")
		}
		g.emitSealedVariant(c, v)
	}
}

// emitSealedVariant emits one variant of a sealed class. Field
// declarations all become `getter` (visible from outside the class
// since match bindings need `s.radius`-style access). Constructor
// is auto-generated from the variant's params, plus a `to_s` override
// that matches zinc-go's `Circle(radius=5)` format byte-for-byte.
//
// The to_s emit goes through zinc_fmt for each field value so
// integer-valued Float64 fields (`Circle(radius=5.0)`) print as
// `Circle(radius=5)` matching Go's %v output.
func (g *Generator) emitSealedVariant(parent *parser.ClassDecl, v *parser.DataClassDecl) {
	g.writeln("class %s::%s < %s", parent.Name, v.Name, parent.Name)
	g.indent++
	for _, p := range v.Params {
		g.writeln("getter %s : %s", p.Name, g.crType(p.Type))
	}
	if len(v.Params) > 0 {
		// Constructor: `def initialize(@a : T, @b : U); end`. The
		// @-prefix syntax is Crystal's shorthand for assigning the
		// argument to an instance variable in one go.
		params := make([]string, 0, len(v.Params))
		for _, p := range v.Params {
			params = append(params, fmt.Sprintf("@%s : %s", p.Name, g.crType(p.Type)))
		}
		g.writeln("")
		g.writeln("def initialize(%s)", strings.Join(params, ", "))
		g.writeln("end")
	}
	// to_s override — `<Variant>(<field>=<value>, ...)` shape.
	if len(v.Params) > 0 {
		g.needsZincFmt = true
		g.writeln("")
		g.writeln("def to_s(io : IO) : Nil")
		g.indent++
		// Build the output as one io << "..." chain. Crystal allows
		// io << "Circle(radius=" << zinc_fmt(@radius) << ")" but
		// requires no commas. We emit pieces line by line for clarity.
		g.writeln(`io << "%s("`, v.Name)
		for i, p := range v.Params {
			if i > 0 {
				g.writeln(`io << ", "`)
			}
			g.writeln(`io << "%s=" << zinc_fmt(@%s)`, p.Name, p.Name)
		}
		g.writeln(`io << ")"`)
		g.indent--
		g.writeln("end")
	}
	for _, m := range v.Methods {
		g.writeln("")
		// Method on a variant — uses the parent class for field
		// bookkeeping. Construct a synthetic ClassDecl for the
		// emitMethod's currentClassFields tracking.
		synth := &parser.ClassDecl{Name: parent.Name + "::" + v.Name}
		for _, p := range v.Params {
			synth.Fields = append(synth.Fields, &parser.FieldDecl{Name: p.Name, Type: p.Type})
		}
		g.emitMethod(synth, m)
	}
	g.indent--
	g.writeln("end")
}

// emitClassDecl lowers `class Foo[ : Bar] { fields, init, methods }`
// to Crystal `class Foo[ < Bar] ... end`.
//
// Field handling:
//   - private (default): `@field : Type`
//   - pub: `getter field : Type` (Crystal's getter macro auto-emits an
//     accessor; PLAN §4.2 — using built-in macros is OK per the
//     "leave macros for now" decision applies to *custom* macros, not
//     stdlib ones like getter/property)
//   - default value: appended to the @field declaration (`@n = 0`)
//     in initialize, not as `= ...` after the type — Crystal classes
//     can't initialize ivars at the declaration site.
//
// Constructor:
//   - `init() { body }` → `def initialize; body; end`. zinc's `this.x = e`
//     becomes Crystal's `@x = e` (the implicit-self lowering).
//
// Methods:
//   - `pub void inc() { n = n + 1 }` → `def inc : Nil; @n = @n + 1; end`.
//     Bare field references (`n` inside a method body) lower to `@n`.
//
// SKETCH limitations:
//   - No interfaces / multiple parents (`class Foo : Bar, Qux` — TODO).
//   - No generics (`class Box<T>` — TODO).
//   - No overloaded constructors (zinc's Ctors[] beyond primary — TODO).
//   - No const / readonly / init field modifiers — TODO.
//   - No annotations.
//   - `super(...)` calls in ctor body — TODO.
func (g *Generator) emitPlainClass(c *parser.ClassDecl) {
	header := "class " + c.Name
	autoException := false
	// Walk Parents — first non-interface entry becomes the `< Parent`
	// inheritance, all interface entries get emitted as `include` lines
	// inside the class body. zinc allows `class Foo : Bar, IBaz, IQux`
	// with multiple interfaces; Crystal requires single inheritance plus
	// any number of module includes, so this split works.
	var classParent string
	var includeMixins []string
	for _, p := range c.Parents {
		if g.interfaces[p] {
			includeMixins = append(includeMixins, p)
		} else if classParent == "" {
			classParent = p
		}
	}
	if classParent != "" {
		header += " < " + classParent
	} else if isErrorClassName(c.Name) {
		// Convention: any class named *Error or *Exception auto-extends
		// Crystal's Exception. Zinc users write `class ParseError { ... }`
		// and zinc-crystal makes it raisable. Without this, `raise
		// ParseError.new(...)` is a Crystal compile error because plain
		// classes can't be raised.
		header += " < Exception"
		autoException = true
	}
	g.writeln(header)
	g.indent++

	// Mixin interfaces via `include`. Has to come first in the body
	// before fields/methods so the include is in scope.
	for _, iface := range includeMixins {
		g.writeln("include %s", iface)
	}
	if len(includeMixins) > 0 && len(c.Fields) > 0 {
		g.writeln("")
	}

	// Field declarations: private fields as @-vars, pub as getters.
	// Default values move into initialize since Crystal doesn't allow
	// `@x : Int32 = 0` at the class-body level.
	//
	// Auto-Exception subclasses skip emitting a `message` field —
	// Crystal's Exception base class already provides @message : String?,
	// and re-declaring it as String here would be a type-redeclaration
	// error. The user's `pub String message` in zinc is the same field;
	// we just route through the parent class.
	for _, f := range c.Fields {
		if autoException && f.Name == "message" {
			continue
		}
		if f.IsPub {
			g.writeln("getter %s : %s", f.Name, g.crType(f.Type))
		} else {
			g.writeln("@%s : %s", f.Name, g.crType(f.Type))
		}
	}
	if len(c.Fields) > 0 && (c.Ctor != nil || len(c.Methods) > 0) {
		g.writeln("")
	}

	// Constructor.
	if c.Ctor != nil {
		g.emitCtor(c, autoException)
	}

	// Methods.
	for i, m := range c.Methods {
		if i > 0 || c.Ctor != nil {
			g.writeln("")
		}
		g.emitMethod(c, m)
	}

	g.indent--
	g.writeln("end")
}

// emitCtor emits `def initialize(...) ... end`. We build a class-body
// scope (currentClassFields) so `this.x = e` and bare `x = e` both
// lower to `@x = e`.
//
// autoException == true means this class extends Crystal's Exception
// via our naming convention. In that mode, any `this.message = X`
// statement in the ctor body is rewritten to `super(X)` so we set
// the parent class's @message instead of re-declaring our own.
func (g *Generator) emitCtor(c *parser.ClassDecl, autoException bool) {
	prevFields := g.currentClassFields
	g.currentClassFields = make(map[string]bool, len(c.Fields))
	for _, f := range c.Fields {
		g.currentClassFields[f.Name] = true
	}
	defer func() { g.currentClassFields = prevFields }()

	params := make([]string, 0, len(c.Ctor.Params))
	for _, p := range c.Ctor.Params {
		params = append(params, fmt.Sprintf("%s : %s", p.Name, g.crType(p.Type)))
	}
	if len(c.Ctor.Params) == 0 {
		g.writeln("def initialize")
	} else {
		g.writeln("def initialize(%s)", strings.Join(params, ", "))
	}
	g.indent++

	// super(args) — zinc's parser strips the super(...) call out of
	// the ctor body and parks the args on the CtorDecl. We have to
	// re-emit it at the top of initialize. Without this, ParseError
	// extending BaseError would never run BaseError's init (which
	// sets the message via super → Exception).
	if c.Ctor.SuperCalled {
		args := make([]string, 0, len(c.Ctor.SuperArgs))
		for _, a := range c.Ctor.SuperArgs {
			args = append(args, g.emitExpr(a))
		}
		g.writeln("super(%s)", strings.Join(args, ", "))
	}

	// Auto-init for stdlib types like sync.Mutex / sync.RWMutex /
	// sync.WaitGroup. zinc-go does the same thing in its codegen
	// (sync_field_init.zn was the regression test). Without this, the
	// field stays nil and the first method call segfaults.
	for _, f := range c.Fields {
		if simple, ok := f.Type.(*parser.SimpleType); ok {
			if mapped, ok := stdlibTypeRewrite[simple.Name]; ok {
				g.writeln("@%s = %s.new", f.Name, mapped)
			}
		}
	}

	if c.Ctor.Body != nil {
		for _, s := range c.Ctor.Body.Stmts {
			if autoException && isMessageAssign(s) {
				assign := s.(*parser.AssignStmt)
				g.writeln("super(%s)", g.emitExpr(assign.Value))
				continue
			}
			g.emitStmt(s)
		}
	}
	g.indent--
	g.writeln("end")
}

// isMessageAssign matches `this.message = X` AST shape. Used in the
// auto-Exception ctor lowering to rewrite to `super(X)`.
func isMessageAssign(s parser.Stmt) bool {
	a, ok := s.(*parser.AssignStmt)
	if !ok {
		return false
	}
	sel, ok := a.Target.(*parser.SelectorExpr)
	if !ok || sel.Field != "message" {
		return false
	}
	if _, isThis := sel.Object.(*parser.ThisExpr); isThis {
		return true
	}
	if id, ok := sel.Object.(*parser.Ident); ok && id.Name == "this" {
		return true
	}
	return false
}

// emitMethod emits a `def name(...) : Ret ... end`. Drops `pub`
// (Crystal methods are public by default; private would be `private def`).
func (g *Generator) emitMethod(c *parser.ClassDecl, m *parser.MethodDecl) {
	prevFields := g.currentClassFields
	g.currentClassFields = make(map[string]bool, len(c.Fields))
	for _, f := range c.Fields {
		g.currentClassFields[f.Name] = true
	}
	defer func() { g.currentClassFields = prevFields }()

	params := make([]string, 0, len(m.Params))
	for _, p := range m.Params {
		params = append(params, fmt.Sprintf("%s : %s", p.Name, g.crType(p.Type)))
	}
	ret := g.crType(m.ReturnType)
	if ret == "" {
		ret = "Nil"
	}
	methodName := crMethodName(m.Name)
	if len(m.Params) == 0 {
		g.writeln("def %s : %s", methodName, ret)
	} else {
		g.writeln("def %s(%s) : %s", methodName, strings.Join(params, ", "), ret)
	}
	g.indent++
	if m.Body != nil {
		for _, s := range m.Body.Stmts {
			g.emitStmt(s)
		}
	}
	g.indent--
	g.writeln("end")
}

// isTrueLit returns true iff the expression is the bool literal `true`.
// Used by the `while (true)` → `loop do` rewrite.
func isTrueLit(e parser.Expr) bool {
	if b, ok := e.(*parser.BoolLit); ok {
		return b.Value
	}
	return false
}

// isErrorClassName tells the class-decl emitter whether a class is a
// candidate to inherit from Crystal's Exception. Convention-based —
// names ending in "Error" or "Exception" are treated as raisable.
// Anything else stays a plain class.
//
// SKETCH: cross-class inheritance complicates this. If `class
// ParseError : BaseError` and BaseError already extends Exception,
// ParseError gets it transitively and we shouldn't add the explicit
// `< Exception`. The current code only adds it when Parents is empty,
// which means user-declared inheritance chains work as written. Phase 1
// can revisit if users hit a case the convention misses.
func isErrorClassName(name string) bool {
	return strings.HasSuffix(name, "Error") || strings.HasSuffix(name, "Exception")
}

// isThrowerType reports whether a TypeExpr is a thrower return shape:
// bare `error`, or a TupleType whose last element is `error`. Used to
// decide whether to drop the error slot from the Crystal return type
// signature (Crystal uses raise/rescue, not error-as-value).
func isThrowerType(t parser.TypeExpr) bool {
	if t == nil {
		return false
	}
	if s, ok := t.(*parser.SimpleType); ok && s.Name == "error" {
		return true
	}
	if tup, ok := t.(*parser.TupleType); ok && len(tup.Elements) > 0 {
		if last, ok := tup.Elements[len(tup.Elements)-1].(*parser.SimpleType); ok && last.Name == "error" {
			return true
		}
	}
	return false
}

// throwerValueType strips the trailing `error` slot from a thrower
// signature, returning the Crystal type for the "value" portion.
//
//   error               → Nil
//   (Int, error)        → Int32
//   (Int, String, error) → Tuple(Int32, String)
//
// SKETCH: multi-value throwers lower to a Tuple. zinc-go uses tuple
// returns natively; Crystal supports the same shape with Tuple(...).
func (g *Generator) throwerValueType(t parser.TypeExpr) string {
	if s, ok := t.(*parser.SimpleType); ok && s.Name == "error" {
		return "Nil"
	}
	if tup, ok := t.(*parser.TupleType); ok && len(tup.Elements) > 1 {
		// Drop trailing error.
		valueTypes := tup.Elements[:len(tup.Elements)-1]
		if len(valueTypes) == 1 {
			return g.crType(valueTypes[0])
		}
		parts := make([]string, len(valueTypes))
		for i, vt := range valueTypes {
			parts[i] = g.crType(vt)
		}
		return "Tuple(" + strings.Join(parts, ", ") + ")"
	}
	return "Nil"
}

// emitFnDecl emits a Crystal `def <name>(<params>) : <ret>` block.
//
// SKETCH: handles the simplest shape — primitive-only param types,
// primitive return type or `void`. Generic params, default values,
// `pub` visibility, thrower-error returns, and lambda-typed params
// all TODO. The hello.zn fn `String greet(String name) { ... }` and
// `void main() { ... }` are the two shapes covered today.
func (g *Generator) emitFnDecl(fn *parser.FnDecl) {
	params := make([]string, 0, len(fn.Params))
	for _, p := range fn.Params {
		params = append(params, fmt.Sprintf("%s : %s", p.Name, g.crType(p.Type)))
	}
	ret := g.crType(fn.ReturnType)
	if ret == "" {
		ret = "Nil" // void → Nil
	}
	// Thrower return type rewrite: `(Int, error)` → `Int32`,
	// bare `error` → `Nil`. Crystal uses raise/rescue rather than
	// returning an error slot.
	isThrower := isThrowerType(fn.ReturnType)
	if isThrower {
		ret = g.throwerValueType(fn.ReturnType)
	}
	header := fmt.Sprintf("def %s(%s) : %s", fn.Name, strings.Join(params, ", "), ret)
	if len(fn.Params) == 0 {
		header = fmt.Sprintf("def %s : %s", fn.Name, ret)
	}
	g.writeln(header)
	g.indent++

	prevThrower := g.currentFnIsThrower
	g.currentFnIsThrower = isThrower
	if fn.Body != nil {
		for _, s := range fn.Body.Stmts {
			g.emitStmt(s)
		}
	}
	g.currentFnIsThrower = prevThrower

	g.indent--
	g.writeln("end")
}

// crType lowers a zinc TypeExpr to its Crystal-side type string,
// per PLAN §4.1. SKETCH covers only primitive cases needed for
// hello-world; the full table lands incrementally.
func (g *Generator) crType(t parser.TypeExpr) string {
	if t == nil {
		return ""
	}
	if name, ok := t.(*parser.SimpleType); ok {
		switch name.Name {
		case "void":
			return "Nil"
		case "int":
			return "Int32"
		case "long":
			return "Int64"
		case "byte":
			return "UInt8"
		case "double":
			return "Float64"
		case "float":
			return "Float32"
		case "boolean":
			return "Bool"
		case "String":
			return "String"
		case "any":
			// Per PLAN §1.4 + per-target diff list: `any` doesn't have a
			// natural Crystal lowering. Reject at validate time. For now
			// emit a placeholder + record the error so the driver fails.
			g.compileError(0, "type 'any' is not allowed in zinc-crystal — use a concrete type")
			return "Object" // unreachable in practice; build fails first
		}
		// Stdlib type rewrites: zinc users write `sync.Mutex` (Go-style),
		// Crystal-target maps to `Sync::Mutex`. Same shape for RWLock,
		// WaitGroup, etc. Same convention for any future stdlib import
		// that has a different naming idiom in Crystal.
		if mapped, ok := stdlibTypeRewrite[name.Name]; ok {
			return mapped
		}
		return name.Name // assume user-defined type, pass through
	}
	if gt, ok := t.(*parser.GenericType); ok {
		switch gt.Name {
		case "List":
			if len(gt.TypeArgs) == 1 {
				return "Array(" + g.crType(gt.TypeArgs[0]) + ")"
			}
		case "Map":
			if len(gt.TypeArgs) == 2 {
				return "Hash(" + g.crType(gt.TypeArgs[0]) + ", " + g.crType(gt.TypeArgs[1]) + ")"
			}
		case "Set":
			if len(gt.TypeArgs) == 1 {
				return "Set(" + g.crType(gt.TypeArgs[0]) + ")"
			}
		case "Channel", "Chan":
			if len(gt.TypeArgs) == 1 {
				return "Channel(" + g.crType(gt.TypeArgs[0]) + ")"
			}
		}
		// User-defined generic — pass through (e.g. `Box<T>` → `Box(T)`)
		args := make([]string, 0, len(gt.TypeArgs))
		for _, a := range gt.TypeArgs {
			args = append(args, g.crType(a))
		}
		return fmt.Sprintf("%s(%s)", gt.Name, strings.Join(args, ", "))
	}
	if opt, ok := t.(*parser.OptionalType); ok {
		// `T?` → Crystal's `T?` (nilable union). Crystal's
		// nullable shorthand. Plan §4.1 lowering.
		return g.crType(opt.Inner) + "?"
	}
	if tup, ok := t.(*parser.TupleType); ok {
		parts := make([]string, len(tup.Elements))
		for i, e := range tup.Elements {
			parts[i] = g.crType(e)
		}
		return "Tuple(" + strings.Join(parts, ", ") + ")"
	}
	if arr, ok := t.(*parser.ArrayType); ok {
		// Sized array `int[]` → `Array(Int32)`. zinc's typed ArrayType
		// (without explicit size) is a regular Crystal Array. Sized
		// fixed-length arrays would use StaticArray(T, N) — TODO when
		// SizedArrayExpr is the input.
		return "Array(" + g.crType(arr.ElementType) + ")"
	}
	if fn, ok := t.(*parser.FuncTypeExpr); ok {
		// `Fn<(A, B), C>` → Crystal `Proc(A, B, C)` (return type
		// trails the param types). Crystal procs have the same
		// shape — first-class function values.
		parts := make([]string, 0, len(fn.Params)+1)
		for _, p := range fn.Params {
			parts = append(parts, g.crType(p))
		}
		ret := g.crType(fn.ReturnType)
		if ret == "" {
			ret = "Nil"
		}
		parts = append(parts, ret)
		return "Proc(" + strings.Join(parts, ", ") + ")"
	}
	g.compileError(0, "codegen_cr: unsupported type %T", t)
	return "Object"
}

// --- Statement dispatch --------------------------------------------------

// emitStmt lowers one zinc statement. SKETCH: covers ReturnStmt and
// ExprStmt — everything print()-shaped routes through emitExpr's
// CallExpr arm. var/assign/if/while/for/match all TODO.
func (g *Generator) emitStmt(s parser.Stmt) {
	switch stmt := s.(type) {
	case *parser.ReturnStmt:
		g.emitReturn(stmt)
	case *parser.ExprStmt:
		if stmt.OrHandler != nil {
			g.emitOrHandlerStmt(stmt.Expr, stmt.OrHandler, "")
		} else {
			g.writeln("%s", g.emitExpr(stmt.Expr))
		}
	case *parser.VarStmt:
		// Local var. Crystal infers the type from the RHS; emit
		// `<name> = <expr>` for type-inferred or `<name> : <type> = <expr>`
		// when the user wrote an explicit type. With an or { } handler,
		// wrap the RHS in a begin/rescue block so a thrown exception
		// runs the handler body. The `err` binding inside the handler
		// becomes a Crystal `rescue err : Exception` capture.
		if stmt.OrHandler != nil {
			g.emitOrHandlerStmt(stmt.Value, stmt.OrHandler, stmt.Name)
		} else if stmt.Type != nil {
			g.writeln("%s : %s = %s", stmt.Name, g.crType(stmt.Type), g.emitExpr(stmt.Value))
		} else {
			g.writeln("%s = %s", stmt.Name, g.emitExpr(stmt.Value))
		}
	case *parser.AssignStmt:
		// `x = expr` — Crystal accepts the same shape. SKETCH: only
		// covers Ident targets today (`x = 1`). Field/index assignments
		// (`obj.f = 1`, `arr[0] = 1`) land when classes/collections do.
		g.writeln("%s = %s", g.emitExpr(stmt.Target), g.emitExpr(stmt.Value))
	case *parser.IfStmt:
		// Crystal: `if cond ... [elsif ...] [else ...] end`. zinc's AST
		// nests `else if` as a chain of IfStmts in ElseStmt; we walk
		// that chain and emit `elsif` arms instead of nested `if/end`s.
		g.writeln("if %s", g.emitExpr(stmt.Cond))
		g.indent++
		for _, s := range stmt.Then.Stmts {
			g.emitStmt(s)
		}
		g.indent--
		// Walk the else-if chain.
		cur := stmt.ElseStmt
		for cur != nil {
			if elif, ok := cur.(*parser.IfStmt); ok {
				g.writeln("elsif %s", g.emitExpr(elif.Cond))
				g.indent++
				for _, s := range elif.Then.Stmts {
					g.emitStmt(s)
				}
				g.indent--
				cur = elif.ElseStmt
				continue
			}
			if block, ok := cur.(*parser.BlockStmt); ok {
				g.writeln("else")
				g.indent++
				for _, s := range block.Stmts {
					g.emitStmt(s)
				}
				g.indent--
			}
			break
		}
		g.writeln("end")
	case *parser.MatchStmt:
		g.emitMatch(stmt)
	case *parser.ParallelForStmt:
		g.emitParallelFor(stmt)
	case *parser.AssertStmt:
		// `assert (cond)` → `raise "assertion failed" unless cond`.
		// `assert (cond, msg)` → `raise msg unless cond`.
		msg := `"assertion failed"`
		if stmt.Message != nil {
			msg = g.emitExpr(stmt.Message)
		}
		g.writeln("raise %s unless %s", msg, g.emitExpr(stmt.Cond))
	case *parser.ForStmt:
		g.emitFor(stmt)
	case *parser.WhileStmt:
		// `while (true) { body }` lowers to `loop do ... end` (the
		// idiomatic Crystal infinite-loop form, matching zinc-go's
		// while(true)→for{} rewrite). All other `while` keep the
		// natural Crystal shape.
		if isTrueLit(stmt.Cond) {
			g.writeln("loop do")
		} else {
			g.writeln("while %s", g.emitExpr(stmt.Cond))
		}
		g.indent++
		for _, s := range stmt.Body.Stmts {
			g.emitStmt(s)
		}
		g.indent--
		g.writeln("end")
	case *parser.BreakStmt:
		g.writeln("break")
	case *parser.ContinueStmt:
		g.writeln("next")
	case *parser.WithStmt:
		g.emitWith(stmt)
	case *parser.TupleVarStmt:
		// `(a, b) = expr` → `a, b = expr` (Crystal native tuple
		// destructuring on the LHS). Names are already in stmt.Names.
		g.writeln("%s = %s", strings.Join(stmt.Names, ", "), g.emitExpr(stmt.Value))
	default:
		g.compileError(0, "codegen_cr: unsupported stmt %T", stmt)
	}
}

// emitReturn lowers a return statement, with thrower-shape rewrites
// active when currentFnIsThrower is set.
//
// Inside a thrower:
//   - `return ErrorExpr` (a single value that's an error class instance)
//     → `raise ErrorExpr`
//   - `return v, null` (multi-value with implicit no-error)
//     → `return v` (drop the trailing nil for the error slot)
//   - `return v1, v2, null`
//     → `return {v1, v2}` (Tuple shape — values combined, error dropped)
//   - `return null` (bare for an `error`-typed thrower) → just `return`
//
// SKETCH: distinguishing "this single-value return is an error" vs
// "this single-value return is the success value" needs type info we
// don't fully have. Heuristic: if the return value is a CallExpr whose
// callee is a known class name and the class extends Exception (or
// has Error in its hierarchy), treat it as raise. Else value. For
// phase0/error_explicit.zn this is enough.
func (g *Generator) emitReturn(r *parser.ReturnStmt) {
	if r.Value == nil {
		g.writeln("return")
		return
	}
	if g.currentFnIsThrower {
		// TupleLit return — split error handling.
		if tup, ok := r.Value.(*parser.TupleLit); ok && len(tup.Elements) > 0 {
			last := tup.Elements[len(tup.Elements)-1]
			if _, isNull := last.(*parser.NullLit); isNull {
				// Drop the trailing null (error slot).
				values := tup.Elements[:len(tup.Elements)-1]
				if len(values) == 0 {
					g.writeln("return")
					return
				}
				if len(values) == 1 {
					g.writeln("return %s", g.emitExpr(values[0]))
					return
				}
				parts := make([]string, len(values))
				for i, v := range values {
					parts[i] = g.emitExpr(v)
				}
				g.writeln("return {%s}", strings.Join(parts, ", "))
				return
			}
		}
		// Bare null in a thrower — no-op return.
		if _, isNull := r.Value.(*parser.NullLit); isNull {
			g.writeln("return")
			return
		}
		// Single-value return where the value looks like an error
		// constructor — heuristic: CallExpr whose callee Ident name
		// ends in "Error" or is a known class name we've registered.
		if call, ok := r.Value.(*parser.CallExpr); ok {
			if ident, ok := call.Callee.(*parser.Ident); ok {
				name := ident.Name
				if strings.HasSuffix(name, "Error") || strings.HasSuffix(name, "Exception") || g.classes[name] {
					// Render the call WITHOUT the `Foo.new` constructor
					// rewrite — we want `raise Foo.new(msg)` shape.
					args := make([]string, 0, len(call.Args))
					for _, a := range call.Args {
						args = append(args, g.emitExpr(a))
					}
					g.writeln("raise %s.new(%s)", name, strings.Join(args, ", "))
					return
				}
			}
		}
	}
	g.writeln("return %s", g.emitExpr(r.Value))
}

// emitOrHandlerStmt lowers a call-site `or { }` to a begin/rescue
// block. `varName` is non-empty for the VarStmt case (`var ok = ...`),
// empty for the ExprStmt case (`proc.start() or { ... }`).
//
// Result shape:
//
//   <name> = begin     <-- only when varName != ""
//     <expr>
//   rescue err : Exception   <-- err is implicit zinc binding
//     <handler body>
//   end
//
// For the `or match err { case Type -> ... }` form, we'd emit
// multiple `rescue err : Type` arms. SKETCH: simple form only
// today; or-match lands when phase0/error_explicit needs it.
func (g *Generator) emitOrHandlerStmt(value parser.Expr, h *parser.OrHandler, varName string) {
	prefix := ""
	if varName != "" {
		prefix = varName + " = "
	}
	g.writeln("%sbegin", prefix)
	g.indent++
	g.writeln("%s", g.emitExpr(value))
	g.indent--
	if h.MatchCases != nil {
		// or match err { case Foo -> ...; case _ -> ... }
		matchVar := h.MatchVar
		if matchVar == "" {
			matchVar = "err"
		}
		for _, mc := range h.MatchCases {
			if mc.Type == "" {
				g.writeln("rescue %s : Exception", matchVar)
			} else {
				g.writeln("rescue %s : %s", matchVar, mc.Type)
			}
			g.indent++
			if mc.Body != nil {
				for _, s := range mc.Body.Stmts {
					g.emitStmt(s)
				}
			}
			g.indent--
		}
	} else {
		g.writeln("rescue err : Exception")
		g.indent++
		if h.Body != nil {
			for _, s := range h.Body.Stmts {
				g.emitStmt(s)
			}
		}
		g.indent--
	}
	g.writeln("end")
}

// emitWith handles both `lock (mu) { }` (zinc parser uses WithStmt
// with Resources[0].Name == "_lock" as the marker) and `using/with`
// resource-managed blocks.
//
//   lock (mu) { body }   → mu.synchronize do; body; end
//   with (var f = open()) { body }  → wraps body in a begin/ensure
//                                     that calls f.close
//
// SKETCH: only the lock form is wired today. Full with/using lowering
// (begin/ensure with auto-Close) lands when a real example needs it.
func (g *Generator) emitWith(w *parser.WithStmt) {
	if len(w.Resources) == 1 && w.Resources[0].Name == "_lock" {
		mu := g.emitExpr(w.Resources[0].Value)
		g.writeln("%s.synchronize do", mu)
		g.indent++
		if w.Body != nil {
			for _, s := range w.Body.Stmts {
				g.emitStmt(s)
			}
		}
		g.indent--
		g.writeln("end")
		return
	}
	// `using (var r = open()) { body }` — declare r, run body, close r
	// even on exception. Crystal's idiom is begin/ensure.
	for _, r := range w.Resources {
		g.writeln("%s = %s", r.Name, g.emitExpr(r.Value))
	}
	g.writeln("begin")
	g.indent++
	if w.Body != nil {
		for _, s := range w.Body.Stmts {
			g.emitStmt(s)
		}
	}
	g.indent--
	g.writeln("ensure")
	g.indent++
	for _, r := range w.Resources {
		g.writeln("%s.close", r.Name)
	}
	g.indent--
	g.writeln("end")
}

// emitFor lowers zinc for loops to Crystal:
//
//   for (x in xs) { body }              → xs.each do |x| ... end
//   for (i in 0..n) { body }            → (0...n).each do |i| ... end   (exclusive)
//   for (i in 0..=n) { body }           → (0..n).each do |i| ... end    (inclusive)
//   for (k, v in m) { body }            → m.each do |k, v| ... end
//   for (init; cond; post) { body }     → init; while cond; body; post; end
//
// Crystal range note: zinc's `..` is exclusive (matches Go); Crystal's
// `..` is inclusive. So we always emit `...` for zinc's `..`. This is
// PLAN §11.11 cashed out.
//
// SKETCH: C-style for stays as a fallback. Range-style is the common
// case across the example corpus.
func (g *Generator) emitFor(f *parser.ForStmt) {
	if !f.IsRange {
		// C-style: lower as while + post-step. Wraps in a `begin/end`
		// scope so the init var is local. SKETCH — uncommon path.
		if f.Init != nil {
			g.emitStmt(f.Init)
		}
		g.writeln("while %s", g.emitExpr(f.Cond))
		g.indent++
		for _, s := range f.Body.Stmts {
			g.emitStmt(s)
		}
		if f.Post != nil {
			g.emitStmt(f.Post)
		}
		g.indent--
		g.writeln("end")
		return
	}

	// Range-style. Two sub-cases: range expression vs collection.
	rangeExpr := g.emitExpr(f.Range)
	if rng, ok := f.Range.(*parser.RangeExpr); ok {
		op := "..."
		if rng.Inclusive {
			op = ".."
		}
		rangeExpr = fmt.Sprintf("(%s%s%s)", g.emitExpr(rng.Start), op, g.emitExpr(rng.End))
	}

	loopVars := f.Item
	if f.IndexVar != "" {
		// `for (i, item) in xs` — Crystal's each_with_index passes
		// (item, idx). Note the order swap: zinc says (i, item),
		// Crystal yields (item, idx). We rebind for clarity.
		g.writeln("%s.each_with_index do |%s, %s|", rangeExpr, f.Item, f.IndexVar)
	} else {
		g.writeln("%s.each do |%s|", rangeExpr, loopVars)
	}
	g.indent++
	for _, s := range f.Body.Stmts {
		g.emitStmt(s)
	}
	g.indent--
	g.writeln("end")
}

// emitCapacity lowers a capacity-typed constructor.
//
//   Channel<T>(N)  → Channel(T).new(N)
//   List<T>(N)     → Array(T).new(N)            (preallocated array)
//   Map<K,V>(N)    → Hash(K, V).new(initial_capacity: N)  (TODO)
//
// SKETCH: currently only Channel is the common case; List/Map with
// capacity are rare in zinc and Crystal's defaults are good enough.
func (g *Generator) emitCapacity(c *parser.CapacityExpr) string {
	if c.CollectionType == nil {
		return "/* TODO: bare capacity */"
	}
	name := c.CollectionType.Name
	switch name {
	case "Channel", "Chan":
		if len(c.CollectionType.TypeArgs) == 1 {
			return fmt.Sprintf("Channel(%s).new(%s)",
				g.crType(c.CollectionType.TypeArgs[0]), g.emitExpr(c.Capacity))
		}
		// Untyped Channel(N) — Crystal needs a type. Default to
		// Channel(Nil) which matches zinc-go's chan interface{}
		// semantics roughly. Phase 1 should warn here.
		return fmt.Sprintf("Channel(Nil).new(%s)", g.emitExpr(c.Capacity))
	case "List":
		if len(c.CollectionType.TypeArgs) == 1 {
			return fmt.Sprintf("Array(%s).new(%s)",
				g.crType(c.CollectionType.TypeArgs[0]), g.emitExpr(c.Capacity))
		}
	case "Map":
		if len(c.CollectionType.TypeArgs) == 2 {
			// Crystal's Hash#new accepts `initial_capacity:` named arg.
			return fmt.Sprintf("Hash(%s, %s).new(initial_capacity: %s)",
				g.crType(c.CollectionType.TypeArgs[0]),
				g.crType(c.CollectionType.TypeArgs[1]),
				g.emitExpr(c.Capacity))
		}
	case "Set":
		if len(c.CollectionType.TypeArgs) == 1 {
			// Set has no capacity ctor; ignore the capacity hint.
			return fmt.Sprintf("Set(%s).new",
				g.crType(c.CollectionType.TypeArgs[0]))
		}
	}
	g.compileError(0, "codegen_cr: capacity ctor for %s not supported", name)
	return ""
}

// emitSpawn implements the §1.4 no-fire-and-forget rule. spawn { }
// outside an owner scope is a compile error. Inside a parallel-for
// (or future concurrent { } / task { } when those land), it lowers
// to `wg.spawn do ... end`.
//
// Owner-scope tracking lives in concurrencyOwnerDepth +
// currentOwnerKind. emitParallelFor sets them on entry, restores
// on exit.
func (g *Generator) emitSpawn(s *parser.SpawnExpr) string {
	if g.concurrencyOwnerDepth == 0 {
		g.compileError(0, "spawn { } must be inside an owner scope (parallel for / concurrent / task); "+
			"bare spawn has no owner — see PLAN §1.4 no-fire-and-forget rule")
		return "/* spawn-outside-owner */"
	}

	// Currently only the parallel-for owner emits spawn-as-expression
	// inline. Future concurrent { } / task { } slices land their own
	// emit shapes here.
	prefix := "wg.spawn"
	if g.currentOwnerKind == "task" {
		prefix = "Fiber::ExecutionContext::Isolated.new(\"task\")"
	}

	// Build the body inline using a sub-generator to capture indented
	// statements. We could also do raw emit in-buffer, but a substring
	// is cleaner and reuses emitStmt verbatim.
	sub := *g
	sub.buf.Reset()
	sub.indent = g.indent + 1
	if s.Body != nil {
		for _, st := range s.Body.Stmts {
			sub.emitStmt(st)
		}
	}
	g.compileErrors = append(g.compileErrors, sub.compileErrors[len(g.compileErrors):]...)

	// Inline spawn-as-expression form: `<prefix> do; <body>; end`.
	// Crystal accepts trailing `do ... end` blocks on method calls
	// uniformly, so this composes when spawn is nested inside another
	// expression (rare but valid).
	body := sub.buf.String()
	return fmt.Sprintf("%s do\n%s%send", prefix, body, indentStr(g.indent))
}

// emitParallelFor lowers `parallel for (x in xs) { body }` to:
//
//   WaitGroup.wait do |wg|
//     xs.each do |x|
//       wg.spawn do
//         body
//       end
//     end
//   end
//
// Inside the body, concurrencyOwnerDepth=1 and currentOwnerKind="parallel"
// so any zinc-source `spawn { }` inside (rare but legal) lowers to
// wg.spawn. Outside the body, spawn would trip the §1.4 validator.
//
// `or { handler }` on the parallel-for is TODO — needs the same
// shape as `or { }` lowering, which lands in its own slice.
func (g *Generator) emitParallelFor(p *parser.ParallelForStmt) {
	g.requireSet["wait_group"] = true

	rangeExpr := g.emitExpr(p.Range)
	if rng, ok := p.Range.(*parser.RangeExpr); ok {
		op := "..."
		if rng.Inclusive {
			op = ".."
		}
		rangeExpr = fmt.Sprintf("(%s%s%s)", g.emitExpr(rng.Start), op, g.emitExpr(rng.End))
	}

	g.writeln("WaitGroup.wait do |wg|")
	g.indent++
	g.writeln("%s.each do |%s|", rangeExpr, p.Item)
	g.indent++
	g.writeln("wg.spawn do")
	g.indent++

	prevDepth := g.concurrencyOwnerDepth
	prevKind := g.currentOwnerKind
	g.concurrencyOwnerDepth++
	g.currentOwnerKind = "parallel"

	if p.Body != nil {
		for _, s := range p.Body.Stmts {
			g.emitStmt(s)
		}
	}

	g.concurrencyOwnerDepth = prevDepth
	g.currentOwnerKind = prevKind

	g.indent--
	g.writeln("end")
	g.indent--
	g.writeln("end")
	g.indent--
	g.writeln("end")
}

// indentStr returns n levels of two-space indent.
func indentStr(n int) string {
	return strings.Repeat("  ", n)
}

// emitMatch lowers `match (s) { case Pat(bindings) { ... } }` to a
// Crystal `case s; in ParentName::Pat; <bindings>; <body>; end` form.
//
// For sealed-class subjects, every case-in arm gets type-narrowing
// (Crystal sees `in Shape::Circle` and treats `s` as `Shape::Circle`
// in that arm, so `s.radius` works). zinc patterns of the form
// `Circle(r)` translate to two parts:
//   1. The type test:        in Shape::Circle
//   2. Binding to fields:    r = s.radius
// We emit the binding statements as the first lines inside the arm.
//
// Per phase0/MISMATCH §3 + PLAN §11.6: an extra trailing arm
//   in <Base>; raise "unreachable: bare abstract <Base>"
// is required because Crystal's exhaustiveness checker reports the
// abstract base class as a "missing type" even when every concrete
// subclass is covered. The arm is unreachable at runtime (you can't
// instantiate an abstract class) but mandatory for the type-checker.
//
// SKETCH: only sealed-variant patterns are handled. Wildcard `_`,
// literal patterns (`case 0`), and tuple destructuring (`case (a, b)`)
// all TODO. Match expressions (vs match statements) also TODO —
// they need an outer `result = case ...` shape.
func (g *Generator) emitMatch(m *parser.MatchStmt) {
	subject := g.emitExpr(m.Subject)

	// Detect whether this match has a wildcard `_` arm. Crystal's
	// `case in` (exhaustive) doesn't allow `else`; the wildcard
	// requires the non-exhaustive `case when` form. So we pick the
	// keyword based on what the cases contain. Sealed-class matches
	// without wildcards stay on `case in` to keep the runtime
	// fallback arm we emit.
	hasWildcard := false
	for _, mc := range m.Cases {
		if mc.Pattern == nil {
			hasWildcard = true
			break
		}
	}
	useWhen := hasWildcard
	g.writeln("case %s", subject)

	var sealedParent string
	for _, mc := range m.Cases {
		// Wildcard `_` (Pattern is nil) → Crystal `else` arm. Only
		// reachable when useWhen=true (we already detected the case
		// requires the non-exhaustive form).
		if mc.Pattern == nil {
			g.writeln("else")
			g.indent++
			if mc.Body != nil {
				for _, s := range mc.Body.Stmts {
					g.emitStmt(s)
				}
			}
			g.indent--
			continue
		}
		// Pattern is typically a CallExpr like `Circle(r)` for sealed
		// destructuring, or `String(s)` for type-match. Plain idents
		// and literals are also valid for non-sealed match.
		if call, ok := mc.Pattern.(*parser.CallExpr); ok {
			ident, ok := call.Callee.(*parser.Ident)
			if !ok {
				g.compileError(0, "codegen_cr: match: pattern callee %T not supported", call.Callee)
				continue
			}
			// Sealed variant pattern: bind to fields.
			if info, ok := g.sealedVariants[ident.Name]; ok {
				sealedParent = info.Parent
				kw := "in"
				if useWhen {
					kw = "when"
				}
				g.writeln("%s %s::%s", kw, info.Parent, ident.Name)
				g.indent++
				for i, a := range call.Args {
					if i >= len(info.Fields) {
						g.compileError(0, "codegen_cr: match pattern %s has more bindings than fields", ident.Name)
						break
					}
					bindIdent, ok := a.(*parser.Ident)
					if !ok {
						g.compileError(0, "codegen_cr: match: pattern binding %T not supported", a)
						continue
					}
					g.writeln("%s = %s.%s", bindIdent.Name, subject, info.Fields[i])
				}
				if mc.Body != nil {
					for _, s := range mc.Body.Stmts {
						g.emitStmt(s)
					}
				}
				g.indent--
				continue
			}
			// Type-match pattern: `case T(x)` — narrow subject to T,
			// bind x to subject. Crystal's `case in` does the type
			// narrowing automatically; we add the binding statement.
			typeName := ident.Name
			if mapped, ok := primitiveTypeMap[typeName]; ok {
				typeName = mapped
			}
			kw := "in"
			if useWhen {
				kw = "when"
			}
			g.writeln("%s %s", kw, typeName)
			g.indent++
			if len(call.Args) == 1 {
				if bindIdent, ok := call.Args[0].(*parser.Ident); ok {
					g.writeln("%s = %s", bindIdent.Name, subject)
				}
			}
			if mc.Body != nil {
				for _, s := range mc.Body.Stmts {
					g.emitStmt(s)
				}
			}
			g.indent--
			continue
		}
		// Literal / ident pattern (enum variant, integer literal, etc.).
		kw := "in"
		if useWhen {
			kw = "when"
		}
		g.writeln("%s %s", kw, g.emitExpr(mc.Pattern))
		g.indent++
		if mc.Body != nil {
			for _, s := range mc.Body.Stmts {
				g.emitStmt(s)
			}
		}
		g.indent--
	}

	// Crystal's case-in over an abstract base requires the runtime
	// fallback arm — phase0 confirmed. Only needed for `case in`
	// (sealed without wildcard); when useWhen is set the user's
	// `else` arm already covers the unreachable case.
	if sealedParent != "" && !useWhen {
		g.writeln("in %s", sealedParent)
		g.indent++
		g.writeln(`raise "unreachable: bare abstract %s"`, sealedParent)
		g.indent--
	}
	g.writeln("end")
}

// --- Expression dispatch -------------------------------------------------

// emitExpr returns the Crystal source for one expression.
//
// SKETCH: literals, ident, string interpolation, the `print` builtin
// (lowers to `puts`), and ordinary call expressions. Everything else
// marked TODO and routes through compileError so we know what the
// next slice has to grow.
func (g *Generator) emitExpr(e parser.Expr) string {
	if e == nil {
		// Nil expr usually means a missing-value slot the caller should
		// have skipped (e.g. nullable field with no default). Emit the
		// nil literal so we never produce malformed Crystal; callers
		// that want to skip should check for nil before invoking us.
		return "nil"
	}
	switch expr := e.(type) {
	case *parser.Ident:
		// Implicit self: bare field name inside a method/ctor body
		// lowers to `@field`. Outside class scope, plain ident.
		if g.currentClassFields != nil && g.currentClassFields[expr.Name] {
			return "@" + expr.Name
		}
		// Enum member qualification: bare `Red` → `Color::Red`.
		// zinc lets users write the variant name unqualified; Crystal
		// always requires the enum prefix.
		if enumName, ok := g.enumMembers[expr.Name]; ok {
			return enumName + "::" + expr.Name
		}
		return expr.Name
	case *parser.ThisExpr:
		// `this` alone (e.g. returned, passed) becomes `self` in Crystal.
		return "self"
	case *parser.SelectorExpr:
		// `this.x` → `@x`. Other selectors → `obj.field` with method-name
		// lowercasing applied (zinc-go users write `obj.Foo` Go-style;
		// Crystal needs `obj.foo` since uppercase is a constant ref).
		if _, isThis := expr.Object.(*parser.ThisExpr); isThis {
			return "@" + expr.Field
		}
		if id, ok := expr.Object.(*parser.Ident); ok && id.Name == "this" {
			return "@" + expr.Field
		}
		return fmt.Sprintf("%s.%s", g.emitExpr(expr.Object), crMethodName(expr.Field))
	case *parser.IntLit:
		return expr.Value
	case *parser.FloatLit:
		return expr.Value
	case *parser.StringLit:
		return fmt.Sprintf("%q", expr.Value)
	case *parser.BoolLit:
		if expr.Value {
			return "true"
		}
		return "false"
	case *parser.NullLit:
		return "nil"
	case *parser.StringInterpLit:
		return g.emitStringInterp(expr)
	case *parser.BinaryExpr:
		return g.emitBinary(expr)
	case *parser.UnaryExpr:
		// SKETCH: `-x` and `!x` map directly. `&x` is rejected per
		// the per-target diff list — Crystal has no address-of operator.
		if expr.Op == "&" {
			g.compileError(0, "& (address-of) is not allowed in zinc-crystal")
			return ""
		}
		return fmt.Sprintf("%s%s", expr.Op, g.emitExpr(expr.Operand))
	case *parser.CallExpr:
		return g.emitCall(expr)
	case *parser.ListLit:
		return g.emitListLit(expr)
	case *parser.MapLit:
		return g.emitMapLit(expr)
	case *parser.RangeExpr:
		// Bare range expression (outside a for loop, e.g. saved to a
		// var). Same translation rule as in emitFor.
		op := "..."
		if expr.Inclusive {
			op = ".."
		}
		return fmt.Sprintf("(%s%s%s)", g.emitExpr(expr.Start), op, g.emitExpr(expr.End))
	case *parser.IndexExpr:
		return fmt.Sprintf("%s[%s]", g.emitExpr(expr.Object), g.emitExpr(expr.Index))
	case *parser.SliceExpr:
		// `arr[low:high]` → `arr[low...high]`. zinc semantics match Go
		// (exclusive upper), Crystal's `..` is inclusive so we use `...`.
		// Open-ended slices: `arr[:high]` → `arr[..high-1]` (TODO: edge
		// cases); `arr[low:]` → `arr[low..-1]` (TODO).
		obj := g.emitExpr(expr.Object)
		low := "0"
		if expr.Low != nil {
			low = g.emitExpr(expr.Low)
		}
		if expr.High == nil {
			return fmt.Sprintf("%s[%s..-1]", obj, low)
		}
		return fmt.Sprintf("%s[%s...%s]", obj, low, g.emitExpr(expr.High))
	case *parser.SafeNavExpr:
		// `obj?.field` → `obj.try(&.field)`. For `obj?.method(args)` use
		// `obj.try(&.method(args))` or block form. SKETCH: only the
		// field-access form today; method form lands when needed.
		if expr.Call == nil {
			return fmt.Sprintf("%s.try(&.%s)", g.emitExpr(expr.Object), crMethodName(expr.Field))
		}
		args := make([]string, 0, len(expr.Call.Args))
		for _, a := range expr.Call.Args {
			args = append(args, g.emitExpr(a))
		}
		return fmt.Sprintf("%s.try(&.%s(%s))", g.emitExpr(expr.Object), crMethodName(expr.Field), strings.Join(args, ", "))
	case *parser.CapacityExpr:
		return g.emitCapacity(expr)
	case *parser.SpawnExpr:
		return g.emitSpawn(expr)
	case *parser.SuperCallExpr:
		// `super(args)` inside ctor → Crystal's `super(args)` form
		// (1:1). Used when zinc explicitly chains to a parent's init.
		args := make([]string, 0, len(expr.Args))
		for _, a := range expr.Args {
			args = append(args, g.emitExpr(a))
		}
		return fmt.Sprintf("super(%s)", strings.Join(args, ", "))
	case *parser.SpreadExpr:
		// `arr...` at a call site → Crystal's `*arr` splat.
		return "*" + g.emitExpr(expr.Expr)
	case *parser.SizedArrayExpr:
		// `int[10]` → `Array(Int32).new(10, 0)`. Crystal initializes
		// new arrays explicitly with a default value; the zero value
		// for Int32/Int64/Float* is 0, for String "" — we'd need
		// type-aware default selection. SKETCH: use 0 for numeric,
		// rely on Crystal's compile-time check for others.
		zero := "0"
		switch expr.ElementType {
		case "String":
			zero = `""`
		case "boolean", "Bool":
			zero = "false"
		}
		ct := expr.ElementType
		if mapped, ok := primitiveTypeMap[ct]; ok {
			ct = mapped
		}
		return fmt.Sprintf("Array(%s).new(%s, %s)", ct, g.emitExpr(expr.Size), zero)
	case *parser.IfExpr:
		// `if cond: a else: b` → Crystal ternary form
		// `(if cond; a; else; b; end)`. zinc users may have chained
		// elses (else if cond: c else: d) which we walk recursively.
		return g.emitIfExpr(expr)
	case *parser.LambdaExpr:
		return g.emitLambda(expr)
	case *parser.MatchExpr:
		return g.emitMatchExpr(expr)
	case *parser.TypeAssertExpr:
		// `x as T` → `x.as(T)` (Crystal cast — raises on failure).
		// `x is T` → `x.is_a?(T)`.
		typeName := expr.TypeName
		if mapped, ok := primitiveTypeMap[typeName]; ok {
			typeName = mapped
		}
		if expr.IsCheck {
			return fmt.Sprintf("%s.is_a?(%s)", g.emitExpr(expr.Object), typeName)
		}
		return fmt.Sprintf("%s.as(%s)", g.emitExpr(expr.Object), typeName)
	case *parser.RawStringLit:
		// Backtick raw string. Crystal's `%q( ... )` is the closest
		// equivalent; for simple cases the basic double-quoted string
		// with escaping works too. SKETCH: emit %q for safety.
		return fmt.Sprintf("%%q(%s)", expr.Value)
	case *parser.TupleLit:
		parts := make([]string, len(expr.Elements))
		for i, e := range expr.Elements {
			parts[i] = g.emitExpr(e)
		}
		return "{" + strings.Join(parts, ", ") + "}"
	default:
		g.compileError(0, "codegen_cr: unsupported expr %T", expr)
		return fmt.Sprintf("/* TODO %T */", expr)
	}
}

// crystalMethodRewrite maps zinc method names that mean something to
// Crystal's idiomatic equivalents. Applied at every method-call
// emission site (emitCall) for SelectorExpr callees. Per the user's
// "leverage Crystal's collection methods" principle: Channel.recv →
// receive, isEmpty → empty?, contains → includes?, filter → select,
// etc. The user can still call methods literally named "recv" on a
// user-defined class — those would shadow the rewrite, which the
// rewrite map shouldn't catch — but for the common stdlib-like
// targets, this gives idiomatic Crystal output.
//
// SKETCH: shadow detection (rename only when receiver is a known
// stdlib type, not a user class) lands when zinc-crystal grows a
// type tracker. For now, any method named "recv" is rewritten.
// primitiveTypeMap is the same lookup as crType but for use when the
// type appears as a string (e.g. CallExpr.TypeArgs is []string, not
// []TypeExpr). Keep keys in sync with crType's switch.
var primitiveTypeMap = map[string]string{
	"void":    "Nil",
	"int":     "Int32",
	"long":    "Int64",
	"byte":    "UInt8",
	"double":  "Float64",
	"float":   "Float32",
	"boolean": "Bool",
}

// stdlibTypeRewrite maps Go-style stdlib type names to Crystal stdlib
// equivalents. Most zinc projects write `sync.Mutex` because that's
// the Go convention they're used to; zinc-crystal lowers them to the
// Crystal-side shape.
//
// Also drives the auto-init logic in emitCtor — if a class has a
// field of one of these types and no explicit init for it, we emit
// `@field = TypeName.new` at the top of initialize. Crystal classes
// can't initialize ivars at the declaration site, so this avoids
// every project re-typing the same boilerplate.
var stdlibTypeRewrite = map[string]string{
	"sync.Mutex":     "Sync::Mutex",
	"sync.RWMutex":   "Sync::RWLock",
	"sync.WaitGroup": "WaitGroup",
}

// crMethodName returns the Crystal-side method name for a zinc method.
// Two transforms applied in order:
//   1. crystalMethodRewrite hit (recv → receive, isEmpty → empty?, etc.)
//   2. Lowercase first letter if uppercase. Crystal requires methods
//      to start with a lowercase letter (or _); uppercase identifiers
//      are constants/types. zinc-go users follow Go's "capitalize for
//      exported" convention, so `pub String Error()` is common —
//      becomes `def error : String` in Crystal.
//
// The mirror of zinc-go's exportName(). Used at both decl sites
// (emitMethod) and call sites (emitCall on SelectorExpr).
//
// Edge cases:
//   - Names that lowercase to a Crystal reserved word would conflict;
//     not handled today (Phase 1 enumeration). Most zinc method names
//     don't.
//   - All-uppercase acronyms (HTTP, URL) lowercase only the first
//     char (HTTP → hTTP), which looks bad but is technically valid.
//     Phase 1 can apply smarter casing if a real example hits it.
func crMethodName(name string) string {
	if r, ok := crystalMethodRewrite[name]; ok {
		return r
	}
	if name == "" {
		return name
	}
	first := name[0]
	if first >= 'A' && first <= 'Z' {
		return string(first+32) + name[1:]
	}
	return name
}

var crystalMethodRewrite = map[string]string{
	"recv":     "receive",
	"isEmpty":  "empty?",
	"contains": "includes?",
	"filter":   "select",
	"length":   "size",
	"indexOf":  "index",
	// Sync::RWLock idioms — Go-style RLock/RUnlock map to Crystal's
	// imperative read/write helpers. Block forms (`rw.read { }`,
	// `rw.write { }`) are also supported in Crystal but require a
	// control-flow rewrite, not just a method rename.
	"RLock":   "lock_read",
	"RUnlock": "unlock_read",
	"Lock":    "lock_write",
	"Unlock":  "unlock_write",
}

// emitLambda lowers `(x: Int) => x + 1` to Crystal's proc form
// `->(x : Int32) { x + 1 }`. Both single-expr and block-body forms
// are supported. Crystal procs are first-class values that can be
// stored, passed, called, etc. — same role as zinc lambdas.
//
// SKETCH: when the lambda is passed as the trailing arg of a method
// call, Crystal idiom is `arr.each { |x| ... }` (block) rather than
// a proc. We emit the proc form universally for now; converting to
// block form when called as `arr.each(lambda)` would need pattern
// detection and a wrapper. Phase 1 follow-up.
func (g *Generator) emitLambda(l *parser.LambdaExpr) string {
	params := make([]string, 0, len(l.Params))
	for _, p := range l.Params {
		if p.Type != nil {
			params = append(params, fmt.Sprintf("%s : %s", p.Name, g.crType(p.Type)))
		} else {
			// Untyped params — Crystal procs need types, so fall back
			// to a block-style emit when types aren't known.
			params = append(params, p.Name)
		}
	}
	paramStr := ""
	if len(params) > 0 {
		paramStr = "(" + strings.Join(params, ", ") + ")"
	}
	if l.Expr != nil {
		return fmt.Sprintf("->%s { %s }", paramStr, g.emitExpr(l.Expr))
	}
	if l.Body == nil {
		return fmt.Sprintf("->%s { }", paramStr)
	}
	// Block body: emit as multi-statement proc. The body sub-emit
	// captures stmts to a sub-buffer.
	sub := *g
	sub.buf.Reset()
	sub.indent = 0
	for _, s := range l.Body.Stmts {
		sub.emitStmt(s)
	}
	body := strings.TrimSpace(sub.buf.String())
	body = strings.ReplaceAll(body, "\n", "; ")
	return fmt.Sprintf("->%s { %s }", paramStr, body)
}

// emitMatchExpr lowers a match expression — match in value position
// like `var x = match (s) { case Foo -> 1, case Bar -> 2 }`. Crystal's
// `case in` is itself an expression, so we wrap directly.
//
// SKETCH: same restrictions as emitMatch — sealed-variant + literal
// patterns supported, no destructuring of arbitrary patterns yet.
func (g *Generator) emitMatchExpr(m *parser.MatchExpr) string {
	var sb strings.Builder
	sb.WriteString("(case ")
	sb.WriteString(g.emitExpr(m.Subject))
	for _, mc := range m.Cases {
		if mc.Pattern == nil {
			sb.WriteString("; else; ")
			sb.WriteString(g.emitExpr(mc.Value))
			continue
		}
		// Sealed-variant pattern.
		if call, ok := mc.Pattern.(*parser.CallExpr); ok {
			if ident, ok := call.Callee.(*parser.Ident); ok {
				if info, ok := g.sealedVariants[ident.Name]; ok {
					sb.WriteString("; in ")
					sb.WriteString(info.Parent)
					sb.WriteString("::")
					sb.WriteString(ident.Name)
					sb.WriteString("; ")
					// Inline bindings + value as a block expression.
					subject := g.emitExpr(m.Subject)
					for i, a := range call.Args {
						if i >= len(info.Fields) {
							break
						}
						if bid, ok := a.(*parser.Ident); ok {
							sb.WriteString(bid.Name)
							sb.WriteString(" = ")
							sb.WriteString(subject)
							sb.WriteString(".")
							sb.WriteString(info.Fields[i])
							sb.WriteString("; ")
						}
					}
					sb.WriteString(g.emitExpr(mc.Value))
					continue
				}
			}
		}
		// Literal pattern.
		sb.WriteString("; in ")
		sb.WriteString(g.emitExpr(mc.Pattern))
		sb.WriteString("; ")
		sb.WriteString(g.emitExpr(mc.Value))
	}
	sb.WriteString("; end)")
	return sb.String()
}

// emitIfExpr lowers a ternary-like if-expression. Crystal's `if/else`
// is itself an expression so we wrap in parens for clarity at use site:
// `var x = if cond: 1 else: 2` becomes `x = (if cond; 1; else; 2; end)`.
// Chained else-ifs walk through expr.Else if it's another IfExpr.
func (g *Generator) emitIfExpr(e *parser.IfExpr) string {
	var sb strings.Builder
	sb.WriteString("(if ")
	sb.WriteString(g.emitExpr(e.Cond))
	sb.WriteString("; ")
	sb.WriteString(g.emitExpr(e.Then))
	cur := e.Else
	for cur != nil {
		if elif, ok := cur.(*parser.IfExpr); ok {
			sb.WriteString("; elsif ")
			sb.WriteString(g.emitExpr(elif.Cond))
			sb.WriteString("; ")
			sb.WriteString(g.emitExpr(elif.Then))
			cur = elif.Else
			continue
		}
		sb.WriteString("; else; ")
		sb.WriteString(g.emitExpr(cur))
		break
	}
	sb.WriteString("; end)")
	return sb.String()
}

// emitListLit lowers a list literal. zinc `["a","b"]` → Crystal `["a","b"]`
// (1:1 syntax). When ExplicitType is set (`List<int> xs = [1,2]`), we
// emit the typed array form `[1, 2] of Int32` so Crystal infers the
// element type rather than `Array(Int32 | other)`.
//
// Empty lists are special: Crystal rejects bare `[]` because it can't
// infer the element type. With ExplicitType we can emit `[] of T`;
// without it we fall back to a generic-Object array (less ideal but
// at least compiles).
func (g *Generator) emitListLit(l *parser.ListLit) string {
	if len(l.Elements) == 0 {
		if l.ExplicitType != nil {
			if gt, ok := l.ExplicitType.(*parser.GenericType); ok && len(gt.TypeArgs) == 1 {
				return "[] of " + g.crType(gt.TypeArgs[0])
			}
			if at, ok := l.ExplicitType.(*parser.ArrayType); ok {
				return "[] of " + g.crType(at.ElementType)
			}
		}
		// No type hint — fall back to Object. Crystal will complain
		// at use site if this conflicts, but at least the empty
		// literal compiles.
		return "[] of Object"
	}
	parts := make([]string, 0, len(l.Elements))
	for _, e := range l.Elements {
		parts = append(parts, g.emitExpr(e))
	}
	body := "[" + strings.Join(parts, ", ") + "]"
	if l.ExplicitType != nil {
		if gt, ok := l.ExplicitType.(*parser.GenericType); ok && len(gt.TypeArgs) == 1 {
			return body + " of " + g.crType(gt.TypeArgs[0])
		}
	}
	return body
}

// emitMapLit lowers a map literal. zinc `{"a": 1, "b": 2}` → Crystal
// `{"a" => 1, "b" => 2}` (Crystal uses `=>` for hash literals, not `:`).
// Empty map needs a type annotation in Crystal — without an explicit
// type or any keys to infer from, we emit `Hash(String, Int32).new`
// for zinc's bare `{}` ... but zinc rarely emits a bare `{}`, so the
// SKETCH leaves that case as a TODO.
func (g *Generator) emitMapLit(m *parser.MapLit) string {
	if len(m.Keys) == 0 {
		// Empty map: prefer the explicit-type form when zinc gave one,
		// otherwise emit Hash(String, String).new which is the most
		// common-case default and lets Crystal's type inference take
		// over once the map is populated.
		if m.ExplicitType != nil {
			if gt, ok := m.ExplicitType.(*parser.GenericType); ok && len(gt.TypeArgs) == 2 {
				return "{} of " + g.crType(gt.TypeArgs[0]) + " => " + g.crType(gt.TypeArgs[1])
			}
		}
		return "Hash(String, String).new"
	}
	parts := make([]string, 0, len(m.Keys))
	for i := range m.Keys {
		parts = append(parts, fmt.Sprintf("%s => %s", g.emitExpr(m.Keys[i]), g.emitExpr(m.Values[i])))
	}
	body := "{" + strings.Join(parts, ", ") + "}"
	if m.ExplicitType != nil {
		if gt, ok := m.ExplicitType.(*parser.GenericType); ok && len(gt.TypeArgs) == 2 {
			return body + " of " + g.crType(gt.TypeArgs[0]) + " => " + g.crType(gt.TypeArgs[1])
		}
	}
	return body
}

// emitBinary lowers a binary expression. Most zinc operators map 1:1
// to Crystal (+, -, *, /, %, ==, !=, <, <=, >, >=, &&, ||). The few
// that don't:
//   - `and` / `or` keywords → Crystal also accepts `&&` / `||`. zinc
//     uses the symbolic form, so this is a passthrough.
//   - bitwise &, |, ^ → 1:1.
//   - `is` / `is not` → Crystal `is_a?` / `!is_a?` — TODO (lands with
//     match's type guards).
//   - `in` / `not in` → Crystal `.includes?` — TODO.
func (g *Generator) emitBinary(b *parser.BinaryExpr) string {
	left := g.emitExpr(b.Left)
	right := g.emitExpr(b.Right)
	switch b.Op {
	case "is":
		return fmt.Sprintf("%s.is_a?(%s)", left, right)
	case "is not":
		return fmt.Sprintf("!%s.is_a?(%s)", left, right)
	case "in":
		return fmt.Sprintf("%s.includes?(%s)", right, left)
	case "not in":
		return fmt.Sprintf("!%s.includes?(%s)", right, left)
	}
	return fmt.Sprintf("%s %s %s", left, b.Op, right)
}

// emitStringInterp lowers `"hello, ${name}!"` → `"hello, #{name}!"`.
//
// Every interpolated expression gets wrapped in `zinc_fmt(...)` so
// Float64 values without fractional parts print as "12" (matching
// Go's %v) instead of Crystal's default "12.0". The helper is a
// no-op for non-Float values — it just calls to_s — so wrapping
// every interpolation is uniform and cheap. See phase0/MISMATCH §4.
func (g *Generator) emitStringInterp(s *parser.StringInterpLit) string {
	g.needsZincFmt = true
	var sb strings.Builder
	sb.WriteByte('"')
	for _, p := range s.Parts {
		switch part := p.(type) {
		case *parser.StringLit:
			sb.WriteString(part.Value)
		default:
			sb.WriteString("#{zinc_fmt(")
			sb.WriteString(g.emitExpr(part))
			sb.WriteString(")}")
		}
	}
	sb.WriteByte('"')
	return sb.String()
}

// emitZincFmtHelper writes the zinc_fmt top-level helper. Crystal
// allows top-level defs without a module wrapper; using a free
// function keeps the call sites short (`zinc_fmt(x)` vs
// `Zinc.fmt(x)`) and avoids polluting the symbol table with our
// own module name.
//
// The helper is intentionally permissive: anything that responds to
// to_s passes through unchanged. Only Float values get the
// integer-valued trim. Phase-1 follow-up: move this into a
// `zinc-runtime` shard so projects with multiple .zn files don't
// each ship a copy.
func (g *Generator) emitZincFmtHelper() {
	g.writeln("# Bridge for zinc-go output parity: integer-valued Float64s")
	g.writeln("# print as \"12\" not \"12.0\". phase0/MISMATCH §4.")
	g.writeln("def zinc_fmt(v) : String")
	g.indent++
	g.writeln("if v.is_a?(Float)")
	g.indent++
	g.writeln("return v.to_i.to_s if v == v.to_i")
	g.indent--
	g.writeln("end")
	g.writeln("v.to_s")
	g.indent--
	g.writeln("end")
}

// emitCall lowers a call expression. The two cases at this slice:
//   - bare `print(x)` → Crystal `puts <expr>`
//   - everything else → `<callee>(<args>)`
//
// Both Crystal's `puts` and `print` exist as built-ins; we pick `puts`
// because zinc's `print` semantics include a trailing newline (matches
// Go's fmt.Println).
func (g *Generator) emitCall(c *parser.CallExpr) string {
	if ident, ok := c.Callee.(*parser.Ident); ok && ident.Name == "print" {
		args := make([]string, 0, len(c.Args))
		for _, a := range c.Args {
			args = append(args, g.emitExpr(a))
		}
		// puts with multiple args: comma-joined; with one, just the value.
		// hello.zn only ever passes one, so this is safe.
		return "puts " + strings.Join(args, ", ")
	}
	// Channel<T>(N) — typed channel constructor. Parses as CallExpr
	// with TypeArgs=["T"]. Lower to Crystal's Channel(T).new(N).
	// Untyped Channel(N) is rejected per the per-target diff list
	// (Crystal needs a concrete element type).
	if ident, ok := c.Callee.(*parser.Ident); ok && (ident.Name == "Channel" || ident.Name == "Chan") {
		args := make([]string, 0, len(c.Args))
		for _, a := range c.Args {
			args = append(args, g.emitExpr(a))
		}
		typeArg := "Nil"
		if len(c.TypeArgs) == 1 {
			typeArg = c.TypeArgs[0]
			// Apply primitive-type renames since TypeArgs is []string,
			// not TypeExpr — bypass crType.
			if mapped := primitiveTypeMap[typeArg]; mapped != "" {
				typeArg = mapped
			}
		}
		return fmt.Sprintf("Channel(%s).new(%s)", typeArg, strings.Join(args, ", "))
	}
	// Constructor call: `Foo(args)` → `Foo.new(args)` when Foo is a
	// known class. Crystal classes are constructed via `.new`; bare
	// `Foo(args)` is a syntax error (looks like a function call).
	// Sealed-class variants need their parent prefix:
	// `Circle(5.0)` → `Shape::Circle.new(5.0)` because Circle is
	// declared as a nested class under Shape.
	if ident, ok := c.Callee.(*parser.Ident); ok && g.classes[ident.Name] {
		args := make([]string, 0, len(c.Args))
		for _, a := range c.Args {
			args = append(args, g.emitExpr(a))
		}
		typeName := ident.Name
		if v, isVariant := g.sealedVariants[ident.Name]; isVariant {
			typeName = v.Parent + "::" + ident.Name
		}
		return fmt.Sprintf("%s.new(%s)", typeName, strings.Join(args, ", "))
	}
	// Method calls — apply crMethodName (rewrite map + auto-lowercase)
	// and the drop-parens-on-zero-args idiom.
	if sel, ok := c.Callee.(*parser.SelectorExpr); ok {
		field := crMethodName(sel.Field)
		obj := g.emitExpr(sel.Object)
		if len(c.Args) == 0 {
			return fmt.Sprintf("%s.%s", obj, field)
		}
		args := make([]string, 0, len(c.Args))
		for _, a := range c.Args {
			args = append(args, g.emitExpr(a))
		}
		return fmt.Sprintf("%s.%s(%s)", obj, field, strings.Join(args, ", "))
	}
	args := make([]string, 0, len(c.Args))
	for _, a := range c.Args {
		args = append(args, g.emitExpr(a))
	}
	return fmt.Sprintf("%s(%s)", g.emitExpr(c.Callee), strings.Join(args, ", "))
}
