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

	// classes tracks all class names declared in this Program. Used
	// by emitCall to discriminate `Foo(args)` (constructor → Foo.new(args))
	// from `someFn(args)` (regular call). Populated in a pre-pass over
	// prog.Decls before body emission.
	classes map[string]bool

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
		sealedVariants: make(map[string]sealedVariantInfo),
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

	// Trailing `main` invocation — both for explicit and synthesized.
	bodyGen.writeln("main")

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
	default:
		// SKETCH: sealed, enums, data classes, interfaces all land
		// incrementally. Today's class slice covers plain `class Foo`
		// with init + fields + methods + single-parent inheritance.
		g.compileError(0, "codegen_cr: %T not implemented yet", decl)
	}
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
// is auto-generated from the variant's params.
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
	if len(c.Parents) > 0 {
		header += " < " + c.Parents[0]
	}
	g.writeln(header)
	g.indent++

	// Field declarations: private fields as @-vars, pub as getters.
	// Default values move into initialize since Crystal doesn't allow
	// `@x : Int32 = 0` at the class-body level.
	for _, f := range c.Fields {
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
		g.emitCtor(c)
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
func (g *Generator) emitCtor(c *parser.ClassDecl) {
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
	if c.Ctor.Body != nil {
		for _, s := range c.Ctor.Body.Stmts {
			g.emitStmt(s)
		}
	}
	g.indent--
	g.writeln("end")
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
	if len(m.Params) == 0 {
		g.writeln("def %s : %s", m.Name, ret)
	} else {
		g.writeln("def %s(%s) : %s", m.Name, strings.Join(params, ", "), ret)
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
	header := fmt.Sprintf("def %s(%s) : %s", fn.Name, strings.Join(params, ", "), ret)
	if len(fn.Params) == 0 {
		header = fmt.Sprintf("def %s : %s", fn.Name, ret)
	}
	g.writeln(header)
	g.indent++
	if fn.Body != nil {
		for _, s := range fn.Body.Stmts {
			g.emitStmt(s)
		}
	}
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
		return name.Name // assume user-defined type, pass through
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
		if stmt.Value == nil {
			g.writeln("return")
		} else {
			g.writeln("return %s", g.emitExpr(stmt.Value))
		}
	case *parser.ExprStmt:
		g.writeln("%s", g.emitExpr(stmt.Expr))
	case *parser.VarStmt:
		// Local var. Crystal infers the type from the RHS; emit
		// `<name> = <expr>` for type-inferred or `<name> : <type> = <expr>`
		// when the user wrote an explicit type. SKETCH: matches phase0
		// hello.cr's `x : Int32 = 42` shape when the type is given.
		if stmt.Type != nil {
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
	case *parser.WhileStmt:
		// `while (cond) { body }` → `while cond; body; end`.
		// SKETCH: the §1.4 plan note about `while (true)` lowering to
		// Crystal's `loop do ... end` idiom (matching zinc-go's
		// while(true)→for{} rewrite) lands when we have a BoolLit
		// detection here. For now, plain `while`.
		g.writeln("while %s", g.emitExpr(stmt.Cond))
		g.indent++
		for _, s := range stmt.Body.Stmts {
			g.emitStmt(s)
		}
		g.indent--
		g.writeln("end")
	default:
		g.compileError(0, "codegen_cr: unsupported stmt %T", stmt)
	}
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
	g.writeln("case %s", subject)

	var sealedParent string
	for _, mc := range m.Cases {
		// Pattern is typically a CallExpr like `Circle(r)` — Callee is
		// the variant name, Args are the binding names.
		call, ok := mc.Pattern.(*parser.CallExpr)
		if !ok {
			g.compileError(0, "codegen_cr: match: pattern %T not supported yet", mc.Pattern)
			continue
		}
		ident, ok := call.Callee.(*parser.Ident)
		if !ok {
			g.compileError(0, "codegen_cr: match: pattern callee %T not supported", call.Callee)
			continue
		}
		info, ok := g.sealedVariants[ident.Name]
		if !ok {
			g.compileError(0, "codegen_cr: match: %q is not a sealed-class variant", ident.Name)
			continue
		}
		sealedParent = info.Parent

		g.writeln("in %s::%s", info.Parent, ident.Name)
		g.indent++
		// Emit field bindings: `r = s.radius` for each arg in the pattern.
		for i, a := range call.Args {
			if i >= len(info.Fields) {
				g.compileError(0, "codegen_cr: match pattern %s has more bindings than the variant has fields", ident.Name)
				break
			}
			bindIdent, ok := a.(*parser.Ident)
			if !ok {
				g.compileError(0, "codegen_cr: match: pattern binding %T not supported", a)
				continue
			}
			g.writeln("%s = %s.%s", bindIdent.Name, subject, info.Fields[i])
		}
		// Emit the case body.
		if mc.Body != nil {
			for _, s := range mc.Body.Stmts {
				g.emitStmt(s)
			}
		}
		g.indent--
	}

	// Crystal's case-in over an abstract base requires the runtime
	// fallback arm — phase0 confirmed.
	if sealedParent != "" {
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
	switch expr := e.(type) {
	case *parser.Ident:
		// Implicit self: bare field name inside a method/ctor body
		// lowers to `@field`. Outside class scope, plain ident.
		if g.currentClassFields != nil && g.currentClassFields[expr.Name] {
			return "@" + expr.Name
		}
		return expr.Name
	case *parser.ThisExpr:
		// `this` alone (e.g. returned, passed) becomes `self` in Crystal.
		return "self"
	case *parser.SelectorExpr:
		// `this.x` → `@x`. Other selectors → `obj.field` 1:1.
		if _, isThis := expr.Object.(*parser.ThisExpr); isThis {
			return "@" + expr.Field
		}
		if id, ok := expr.Object.(*parser.Ident); ok && id.Name == "this" {
			return "@" + expr.Field
		}
		return fmt.Sprintf("%s.%s", g.emitExpr(expr.Object), expr.Field)
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
	default:
		g.compileError(0, "codegen_cr: unsupported expr %T", expr)
		return fmt.Sprintf("/* TODO %T */", expr)
	}
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
	// Method call with zero args: `c.inc()` — Crystal lets you drop the
	// parens entirely (`c.inc`), which is the more idiomatic shape.
	// Keep parens for any-args case to preserve clarity.
	if sel, ok := c.Callee.(*parser.SelectorExpr); ok && len(c.Args) == 0 {
		return g.emitExpr(sel)
	}
	args := make([]string, 0, len(c.Args))
	for _, a := range c.Args {
		args = append(args, g.emitExpr(a))
	}
	return fmt.Sprintf("%s(%s)", g.emitExpr(c.Callee), strings.Join(args, ", "))
}
