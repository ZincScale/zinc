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

	// Per-file errors. Non-empty means the build fails; the driver
	// prints these and exits non-zero.
	compileErrors []string
}

// New returns a fresh Generator with all maps initialized.
func New() *Generator {
	return &Generator{
		requireSet: make(map[string]bool),
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

	// Assemble final file: requires + blank line + body.
	for k := range g.requireSet {
		g.writeln(`require "%s"`, k)
	}
	if len(g.requireSet) > 0 {
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
	default:
		// SKETCH: classes, sealed, enums, data classes, etc. land later.
		// Today's slice is hello-world only, so anything else surfaces
		// as a TODO that hasn't been wired.
		g.compileError(0, "codegen_cr: %T not implemented yet", decl)
	}
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
	default:
		g.compileError(0, "codegen_cr: unsupported stmt %T", stmt)
	}
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
		return expr.Name
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
	case *parser.CallExpr:
		return g.emitCall(expr)
	default:
		g.compileError(0, "codegen_cr: unsupported expr %T", expr)
		return fmt.Sprintf("/* TODO %T */", expr)
	}
}

// emitStringInterp lowers `"hello, ${name}!"` → `"hello, #{name}!"`.
// SKETCH — straight token-by-token map; no escape edge cases yet.
func (g *Generator) emitStringInterp(s *parser.StringInterpLit) string {
	var sb strings.Builder
	sb.WriteByte('"')
	for _, p := range s.Parts {
		switch part := p.(type) {
		case *parser.StringLit:
			sb.WriteString(part.Value)
		default:
			// Any non-StringLit part is an interpolated expression.
			sb.WriteString("#{")
			sb.WriteString(g.emitExpr(part))
			sb.WriteByte('}')
		}
	}
	sb.WriteByte('"')
	return sb.String()
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
	args := make([]string, 0, len(c.Args))
	for _, a := range c.Args {
		args = append(args, g.emitExpr(a))
	}
	return fmt.Sprintf("%s(%s)", g.emitExpr(c.Callee), strings.Join(args, ", "))
}
