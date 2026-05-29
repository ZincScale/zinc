// Copyright 2026 victorybhg
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pyfront

import (
	"fmt"
	"strings"

	"zinc-go/internal/parser"
)

// Names of the emitted Python-runtime shim functions (see runtime.go). The
// front-end lowers Python operations whose semantics diverge from Go's into
// calls to these, which the codegen renders verbatim.
const (
	rtPrint    = "zincpyPrint"
	rtPrintN   = "zincpyPrintN"
	rtFloorDiv = "zincpyFloorDiv"
	rtMod      = "zincpyMod"
	rtDiv      = "zincpyDiv"
	rtReraise  = "__zincpy_cur_exc" // placeholder for bare `raise`, see exceptions.go
)

// floatWrap wraps e in a float() conversion (codegen → float64(e)).
func floatWrap(e parser.Expr) parser.Expr {
	return &parser.CallExpr{Callee: &parser.Ident{Name: "float"}, Args: []parser.Expr{e}}
}

// callIdent builds a call to a bare function name.
func callIdent(name string, args ...parser.Expr) parser.Expr {
	return &parser.CallExpr{Callee: &parser.Ident{Name: name}, Args: args}
}

// sliceCall builds zincpySlice(obj, start, stop, step) for `obj[start:stop:step]`,
// passing a NullLit (→ nil) for any omitted bound.
func sliceCall(obj, start, stop, step parser.Expr) parser.Expr {
	nilify := func(e parser.Expr) parser.Expr {
		if e == nil {
			return &parser.NullLit{}
		}
		return e
	}
	return callIdent("zincpySlice", obj, nilify(start), nilify(stop), nilify(step))
}

// isNegLiteral reports whether e is a negative integer literal (`-k`).
func isNegLiteral(e parser.Expr) bool {
	u, ok := e.(*parser.UnaryExpr)
	if !ok || u.Op != "-" {
		return false
	}
	_, ok = u.Operand.(*parser.IntLit)
	return ok
}

// Parser is a recursive-descent parser that turns the Python token stream
// into the zinc parser.Program AST. It is intentionally a subset: enough to
// drive the existing typechecker + Go codegen for the current spikes.
type Parser struct {
	toks   []Token
	pos    int
	Errors []string

	// scopes tracks names bound in each (function) scope, mapped to their
	// inferred pytype. Presence lets a plain `x = ...` lower to a fresh
	// declaration (VarStmt → `x := ...`) the first time and a reassignment
	// (AssignStmt → `x = ...`) thereafter — Python has no declaration keyword,
	// but Go does. The type drives int→float promotion in mixed arithmetic.
	scopes []map[string]pytype

	// fnRet maps a known function name to its declared return type, so calls
	// participate in numeric promotion (e.g. `intFn() + 1.5`).
	fnRet map[string]pytype

	// fnRetDict maps a function name to its dict key/value types when it
	// returns a dict, so `d = fn()` tracks d as a dict.
	fnRetDict map[string]dictMeta

	// inMethod is true while parsing a class method body, so `self` lowers to
	// the zinc receiver (ThisExpr).
	inMethod bool

	// elemType records the element type of list-valued names, so iteration,
	// indexing, and comprehensions can infer element types.
	elemType map[string]pytype

	// dictVars records which names are ordered-dicts (*zincpyDict) and their
	// key/value types, so reads/writes/iteration route to the right methods.
	dictVars map[string]dictMeta

	// dictExprMeta carries a dict literal's inferred key/value types from the
	// literal site to the assignment that binds it to a name.
	dictExprMeta map[parser.Expr]dictMeta

	// tmpCount names generated temporaries (comprehension accumulators, try
	// result slots).
	tmpCount int

	// currentFnRet is the enclosing function/method's return type, used to
	// declare the result slot when a `return` inside a try must propagate out
	// of the recover closure.
	currentFnRet parser.TypeExpr

	// ffiModBind maps a module's local binding name to its real CPython module
	// name (`import numpy as np` → "np" → "numpy"), so `np.array(...)` lowers
	// to a libpython FFI call. See ffi.go.
	ffiModBind map[string]string

	// ffiImports lists the CPython modules the program imports, in first-seen
	// order. Empty means the program needs no embedded interpreter, so the
	// driver skips the (libpython-linking) cgo runtime file.
	ffiImports []string

	// Class/instance type tracking. classNames is the set of declared classes;
	// classFields/classMethods map a class to its field types / method return
	// types; instanceClass maps a variable to the class it holds (from a
	// constructor call or a typed param); currentClass is the class whose
	// method body is being parsed (so `self.field` resolves). Together these
	// let typeOf see through `obj.field` and `obj.method()` for numeric
	// promotion and downstream inference.
	classNames    map[string]bool
	classFields   map[string]map[string]pytype
	classMethods  map[string]map[string]pytype
	instanceClass map[string]string
	currentClass  string
	// classProps[class][pyName] marks a method decorated @property, so a bare
	// `obj.name` read is lowered to the method call `obj.Name()`.
	classProps map[string]map[string]bool
	// classMethodRet[class][method] is the method's return class name (when it
	// returns an instance), so operator-dunder results chain (a + b + c).
	classMethodRet map[string]map[string]string
	// classConstVal[class][name] holds a class-level constant's literal value,
	// inlined at each `Class.NAME` / `self.NAME` / `instance.NAME` read.
	classConstVal map[string]map[string]parser.Expr
	// classStatics[class][method] marks @staticmethod/@classmethod methods,
	// which are emitted as package functions named Class_method and called via
	// Class.method(...) / obj.method(...).
	classStatics map[string]map[string]bool
	// pendingDecls holds top-level decls (static/class methods lifted to
	// package functions) that parseProgram appends after the class.
	pendingDecls []parser.TopLevelDecl
	// clsAlias is the class name bound to `cls` while parsing a @classmethod
	// body, so `cls(...)` constructs and `cls.X` reads a class member.
	clsAlias string

	// setVars marks variables holding a *zincpySet (from a set literal or
	// set()), so .add()/len()/iteration route to set operations. setExprMeta
	// marks the set-literal constructor IIFEs so an assignment can pick them up.
	setVars     map[string]bool
	setExprMeta map[parser.Expr]bool

	// defParams maps a callable name to its parameter declarations (with
	// defaults), so call sites can resolve keyword args and fill omitted
	// defaults into a plain positional call. Keyed by function name and by
	// class name (for the constructor).
	defParams map[string][]*parser.ParamDecl
}

// Meta carries front-end facts the driver needs that are not expressible in
// the shared AST — currently just the set of CPython modules the program FFIs
// into, which gates emission of the cgo runtime.
type Meta struct {
	FFIModules []string
}

// Parse lexes and parses Python source into a parser.Program plus front-end
// Meta. The returned error slice is non-empty on any lex or parse failure.
func Parse(src string) (*parser.Program, *Meta, []string) {
	lx := New(src)
	toks := lx.Tokenize()
	if len(lx.Errors) > 0 {
		return nil, nil, lx.Errors
	}
	p := &Parser{
		toks:         toks,
		fnRet:        map[string]pytype{},
		fnRetDict:    map[string]dictMeta{},
		elemType:     map[string]pytype{},
		dictVars:     map[string]dictMeta{},
		dictExprMeta:  map[parser.Expr]dictMeta{},
		ffiModBind:    map[string]string{},
		classNames:    map[string]bool{},
		classFields:   map[string]map[string]pytype{},
		classMethods:  map[string]map[string]pytype{},
		instanceClass:  map[string]string{},
		classProps:     map[string]map[string]bool{},
		classMethodRet: map[string]map[string]string{},
		classConstVal:  map[string]map[string]parser.Expr{},
		classStatics:   map[string]map[string]bool{},
		setVars:        map[string]bool{},
		setExprMeta:    map[parser.Expr]bool{},
		defParams:      map[string][]*parser.ParamDecl{},
	}
	p.pushScope()
	prog := p.parseProgram()
	if len(p.Errors) > 0 {
		return nil, nil, p.Errors
	}
	return prog, &Meta{FFIModules: p.ffiImports}, nil
}

// isFFIModule reports whether name is bound to an imported CPython module.
func (p *Parser) isFFIModule(name string) bool {
	_, ok := p.ffiModBind[name]
	return ok
}

// recordFFIImport adds a module to the import list (de-duplicated, order
// preserved).
func (p *Parser) recordFFIImport(mod string) {
	for _, m := range p.ffiImports {
		if m == mod {
			return
		}
	}
	p.ffiImports = append(p.ffiImports, mod)
}

// recordInstanceClass remembers that `name` holds an instance of a class when
// its initializer is a constructor call `Class(...)`, so later `name.field` /
// `name.method()` types resolve.
func (p *Parser) recordInstanceClass(name string, rhs parser.Expr) {
	if call, ok := rhs.(*parser.CallExpr); ok {
		if id, ok := call.Callee.(*parser.Ident); ok && p.classNames[id.Name] {
			p.instanceClass[name] = id.Name
		}
	}
}

// isSetExpr reports whether an expression produces a *zincpySet (a set literal
// IIFE or a set()/set(iter) call).
func (p *Parser) isSetExpr(e parser.Expr) bool {
	if p.setExprMeta[e] {
		return true
	}
	if call, ok := e.(*parser.CallExpr); ok {
		if id, ok := call.Callee.(*parser.Ident); ok {
			return id.Name == "zincpyNewSet" || id.Name == "zincpySetOf"
		}
	}
	return false
}

// recordSetVar marks a variable as holding a set when its initializer makes one.
func (p *Parser) recordSetVar(name string, rhs parser.Expr) {
	if p.isSetExpr(rhs) {
		p.setVars[name] = true
	}
}

// isSetVar reports whether e is a variable known to hold a set.
func (p *Parser) isSetVar(e parser.Expr) bool {
	id, ok := e.(*parser.Ident)
	return ok && p.setVars[id.Name]
}

// recordParamInstance tracks a parameter annotated with a class type as an
// instance of that class.
func (p *Parser) recordParamInstance(pa *parser.ParamDecl) {
	if st, ok := pa.Type.(*parser.SimpleType); ok && p.classNames[st.Name] {
		p.instanceClass[pa.Name] = st.Name
	}
}

// --- scope helpers -----------------------------------------------------------

func (p *Parser) pushScope() { p.scopes = append(p.scopes, map[string]pytype{}) }
func (p *Parser) popScope()  { p.scopes = p.scopes[:len(p.scopes)-1] }

func (p *Parser) declare(name string, t pytype) {
	p.scopes[len(p.scopes)-1][name] = t
}

func (p *Parser) isDeclared(name string) bool {
	_, ok := p.scopes[len(p.scopes)-1][name]
	return ok
}

// lookupType returns the inferred type of name, searching innermost scope
// outward; tUnknown if unbound.
func (p *Parser) lookupType(name string) pytype {
	for i := len(p.scopes) - 1; i >= 0; i-- {
		if t, ok := p.scopes[i][name]; ok {
			return t
		}
	}
	return tUnknown
}

// --- token cursor ------------------------------------------------------------

func (p *Parser) cur() Token  { return p.toks[p.pos] }
func (p *Parser) next() Token { return p.toks[p.pos+1] }

func (p *Parser) advance() Token {
	t := p.toks[p.pos]
	if p.pos < len(p.toks)-1 {
		p.pos++
	}
	return t
}

func (p *Parser) isOp(v string) bool  { t := p.cur(); return t.Kind == TOp && t.Value == v }
func (p *Parser) peekOp(v string) bool {
	if p.pos+1 < len(p.toks) {
		t := p.toks[p.pos+1]
		return t.Kind == TOp && t.Value == v
	}
	return false
}
func (p *Parser) peekKw(v string) bool {
	if p.pos+1 < len(p.toks) {
		t := p.toks[p.pos+1]
		return t.Kind == TName && t.Value == v
	}
	return false
}
func (p *Parser) isKw(v string) bool  { t := p.cur(); return t.Kind == TName && t.Value == v }
func (p *Parser) acceptOp(v string) bool {
	if p.isOp(v) {
		p.advance()
		return true
	}
	return false
}

func (p *Parser) errf(t Token, format string, a ...any) {
	p.Errors = append(p.Errors, fmt.Sprintf("line %d: %s", t.Line, fmt.Sprintf(format, a...)))
}

func (p *Parser) expectOp(v string) {
	if !p.acceptOp(v) {
		p.errf(p.cur(), "expected %q, got %s %q", v, p.cur().Kind, p.cur().Value)
		p.advance()
	}
}

func (p *Parser) expectKind(k TokKind) Token {
	if p.cur().Kind != k {
		p.errf(p.cur(), "expected %s, got %s %q", k, p.cur().Kind, p.cur().Value)
		return p.advance()
	}
	return p.advance()
}

// skipNewlines consumes any run of NEWLINE tokens (blank logical lines).
func (p *Parser) skipNewlines() {
	for p.cur().Kind == TNewline {
		p.advance()
	}
}

// --- program / statements ----------------------------------------------------

func (p *Parser) parseProgram() *parser.Program {
	prog := &parser.Program{}
	for p.cur().Kind != TEOF {
		p.skipNewlines()
		if p.cur().Kind == TEOF {
			break
		}
		// Top-level decorators (e.g. @dataclass) precede a class or def.
		var decorators []string
		if p.isOp("@") {
			decorators = p.parseDecorators()
		}
		if p.isKw("def") {
			if fn := p.parseDef(); fn != nil {
				prog.Decls = append(prog.Decls, fn)
			}
			continue
		}
		if p.isKw("class") {
			prog.Decls = append(prog.Decls, p.parseClass(decorators))
			// static/class methods were lifted to package functions.
			prog.Decls = append(prog.Decls, p.pendingDecls...)
			p.pendingDecls = nil
			continue
		}
		startPos := p.pos
		if s := p.parseStmt(); s != nil {
			prog.Stmts = append(prog.Stmts, s)
		}
		if p.pos == startPos { // no progress — avoid infinite loop on error
			p.advance()
		}
	}
	return prog
}

// parseStmt parses one statement (simple or compound). Returns nil for
// statements with no AST representation (e.g. `pass`).
func (p *Parser) parseStmt() parser.Stmt {
	t := p.cur()
	if t.Kind == TName {
		switch t.Value {
		case "def":
			return p.parseDef()
		case "return":
			return p.parseReturn()
		case "for":
			return p.parseFor()
		case "while":
			return p.parseWhile()
		case "if":
			return p.parseIf()
		case "try":
			return p.parseTry()
		case "raise":
			return p.parseRaise()
		case "import":
			return p.parseImport()
		case "from":
			return p.parseFromImport()
		case "pass":
			p.advance()
			p.endSimple()
			return nil
		case "break":
			p.advance()
			p.endSimple()
			return &parser.BreakStmt{}
		case "continue":
			p.advance()
			p.endSimple()
			return &parser.ContinueStmt{}
		}
	}
	return p.parseSimpleStmt()
}

// endSimple consumes the NEWLINE terminating a simple statement.
func (p *Parser) endSimple() {
	if p.cur().Kind == TNewline {
		p.advance()
	}
}

func (p *Parser) parseSimpleStmt() parser.Stmt {
	line := p.cur().Line
	lhs := p.parseExpr()

	// Multi-target assignment: `a, b = ...`. Collect the target names and
	// route to tuple unpacking.
	if p.isOp(",") {
		return p.parseUnpackAssign(line, lhs)
	}

	// assignment / augmented assignment
	if p.cur().Kind == TOp {
		switch op := p.cur().Value; op {
		case "=", "+=", "-=", "*=", "/=", "%=":
			p.advance()
			rhs := p.parseExpr()
			p.endSimple()
			// d[k] = v — lhs was rewritten to a dict read; convert to Set.
			if op == "=" {
				if obj, key, ok := asDictSetTarget(lhs); ok {
					return &parser.ExprStmt{Line: line, Expr: &parser.CallExpr{
						Callee: &parser.SelectorExpr{Object: obj, Field: "Set"},
						Args:   []parser.Expr{key, rhs},
					}}
				}
			}
			if id, ok := lhs.(*parser.Ident); ok {
				if op == "=" && !p.isDeclared(id.Name) {
					p.declare(id.Name, p.typeOf(rhs))
					p.recordElemType(id.Name, rhs)
					p.recordInstanceClass(id.Name, rhs)
					p.recordSetVar(id.Name, rhs)
					if dm, ok := p.dictMetaOfValue(rhs); ok {
						p.dictVars[id.Name] = dm
					}
					return &parser.VarStmt{Line: line, Name: id.Name, Value: rhs}
				}
				// Type-checker-clean contract: a variable keeps one type. A
				// reassignment that changes it (e.g. int → float) is what mypy
				// rejects and what Go's single-typed vars can't express, so
				// reject it here with a clear message rather than emit broken
				// Go. Only fire when both old and new types are known.
				if old, nw := p.lookupType(id.Name), p.typeOf(rhs); old != tUnknown && nw != tUnknown && old != tDynamic && nw != tDynamic && old != nw {
					p.errf(Token{Line: line}, "variable %q is %s but reassigned to %s; zinc-py requires type-consistent variables (mypy would reject this)", id.Name, old, nw)
				}
			}
			return &parser.AssignStmt{Line: line, Target: lhs, Op: op, Value: rhs}
		}
	}

	p.endSimple()

	// list.append(v): Python mutates in place and returns None; Go's append
	// returns a new slice that must be stored back. Rewrite the statement to
	// `xs = append(xs, v)`.
	if call, ok := lhs.(*parser.CallExpr); ok {
		if sel, ok := call.Callee.(*parser.SelectorExpr); ok && sel.Field == "append" && len(call.Args) == 1 {
			return &parser.AssignStmt{
				Line: line, Target: sel.Object, Op: "=",
				Value: callIdent("append", sel.Object, call.Args[0]),
			}
		}
	}

	// print(...): route to the runtime shim so floats/bools format the
	// Python way (3.0 not 3, True not true) rather than Go's fmt defaults.
	// One arg → zincpyPrint; zero or many → zincpyPrintN (space-separated,
	// matching Python's default sep). Keyword args (sep=/end=) are not yet
	// supported.
	if call, ok := lhs.(*parser.CallExpr); ok {
		if id, ok := call.Callee.(*parser.Ident); ok && id.Name == "print" {
			if len(call.NamedArgs) != 0 {
				p.errf(p.cur(), "print() keyword arguments (sep=/end=) are not yet supported")
			} else if len(call.Args) == 1 {
				id.Name = rtPrint
			} else {
				id.Name = rtPrintN
			}
		}
	}
	return &parser.ExprStmt{Line: line, Expr: lhs}
}

// parseTestList parses a comma-separated expression list. A single expression
// returns as-is; two or more become a TupleLit (Python's bare-tuple syntax,
// e.g. `return a, b` or `(1, 2)`).
func (p *Parser) parseTestList() parser.Expr {
	first := p.parseExpr()
	if !p.isOp(",") {
		return first
	}
	elems := []parser.Expr{first}
	for p.acceptOp(",") {
		if p.cur().Kind == TNewline || p.cur().Kind == TEOF || p.isOp(")") {
			break // trailing comma
		}
		elems = append(elems, p.parseExpr())
	}
	return &parser.TupleLit{Elements: elems}
}

// parseUnpackAssign handles `a, b[, ...] = rhs`. When rhs is a single
// expression (a multi-return call), it lowers to TupleVarStmt → `a, b := f()`.
// Parallel literal assignment (`a, b = 1, 2`) and swap have no clean Go
// codegen path here and are rejected for now.
func (p *Parser) parseUnpackAssign(line int, first parser.Expr) parser.Stmt {
	targets := []parser.Expr{first}
	for p.acceptOp(",") {
		if p.isOp("=") {
			break
		}
		targets = append(targets, p.parseExpr())
	}
	if !p.acceptOp("=") {
		// Bare tuple expression statement (no-op in Python) — represent as one.
		p.endSimple()
		return &parser.ExprStmt{Line: line, Expr: &parser.TupleLit{Elements: targets}}
	}
	rhs := p.parseTestList()
	p.endSimple()

	names := make([]string, 0, len(targets))
	for _, t := range targets {
		id, ok := t.(*parser.Ident)
		if !ok {
			p.errf(Token{Line: line}, "unpacking into non-name targets is not yet supported")
			return &parser.ExprStmt{Line: line, Expr: rhs}
		}
		names = append(names, id.Name)
	}
	// Parallel assignment / swap: `a, b = e1, e2`. The RHS is evaluated fully
	// before binding (Go's multi-assignment matches Python here), so a swap
	// `a, b = b, a` needs no temporary. Track each target's type from its RHS
	// element so later use infers correctly.
	if tup, isTuple := rhs.(*parser.TupleLit); isTuple {
		if len(tup.Elements) != len(names) {
			p.errf(Token{Line: line}, "cannot unpack %d values into %d targets", len(tup.Elements), len(names))
			return &parser.ExprStmt{Line: line, Expr: rhs}
		}
		types := make([]pytype, len(names))
		for i := range names {
			types[i] = p.typeOf(tup.Elements[i]) // capture before any re-declare (swap)
		}
		// `=` when every target already exists (reassignment / swap), `:=`
		// when introducing new ones — same rule as single-var VarStmt vs
		// AssignStmt. Blocks don't open new scopes, so isDeclared on the
		// current (function/module) scope is the right test.
		op := ":="
		allDeclared := true
		for _, n := range names {
			if !p.isDeclared(n) {
				allDeclared = false
				break
			}
		}
		if allDeclared {
			op = "="
		}
		for i, n := range names {
			p.declare(n, types[i])
		}
		return &parser.TupleVarStmt{Line: line, Names: names, Value: rhs, Op: op}
	}
	for _, n := range names {
		p.declare(n, tUnknown)
	}
	return &parser.TupleVarStmt{Line: line, Names: names, Value: rhs}
}

func (p *Parser) parseReturn() parser.Stmt {
	line := p.advance().Line // 'return'
	var val parser.Expr
	if p.cur().Kind != TNewline && p.cur().Kind != TEOF {
		val = p.parseTestList()
	}
	p.endSimple()
	return &parser.ReturnStmt{Line: line, Value: val}
}

// parseDottedName reads a possibly dotted module path (math, os.path) as a
// single string.
func (p *Parser) parseDottedName() string {
	name := p.expectKind(TName).Value
	for p.isOp(".") {
		p.advance()
		name += "." + p.expectKind(TName).Value
	}
	return name
}

// parseImport lowers `import mod [as alias]` to a zincpyPyImport side-effecting
// call and records the binding so `alias.fn(...)` routes through CPython FFI.
// One module per statement (PEP 8 style); dotted modules are not yet routed.
func (p *Parser) parseImport() parser.Stmt {
	p.advance() // 'import'
	mod := p.parseDottedName()
	bind := mod
	if p.isKw("as") {
		p.advance()
		bind = p.expectKind(TName).Value
	}
	tok := p.cur()
	p.endSimple()
	if p.isOp(",") {
		p.errf(tok, "multiple imports per statement are not yet supported; use one import per line")
		return nil
	}
	if strings.Contains(mod, ".") {
		p.errf(tok, "dotted import %q is not yet supported", mod)
		return nil
	}
	p.ffiModBind[bind] = mod
	p.recordFFIImport(mod)
	return &parser.ExprStmt{Expr: callIdent("zincpyPyImport", &parser.StringLit{Value: mod})}
}

// parseFromImport handles `from mod import ...`. Binding individual names
// (`from math import sqrt`) is ambiguous to lower (a bare name may be a value
// or a call), so it is rejected for now in favor of `import mod` + `mod.name`.
func (p *Parser) parseFromImport() parser.Stmt {
	tok := p.advance() // 'from'
	mod := p.parseDottedName()
	if p.isKw("import") {
		p.advance()
	}
	// Collect the imported names (handles `a, b as c` and parenthesized lists).
	p.acceptOp("(")
	var names []string
	for p.cur().Kind == TName || p.isOp("*") {
		if p.acceptOp("*") {
			names = append(names, "*")
		} else {
			names = append(names, p.expectKind(TName).Value)
			if p.isKw("as") { // alias — binding name doesn't matter for our checks
				p.advance()
				p.expectKind(TName)
			}
		}
		if !p.acceptOp(",") {
			break
		}
	}
	p.acceptOp(")")
	p.endSimple()

	// Compile-time-only modules: the import only needs to exist for CPython.
	// We no-op it, but ONLY for names we actually handle natively — anything
	// else gets a clear error here rather than a confusing Go compile failure,
	// so we never silently emit a program that diverges from CPython.
	switch mod {
	case "__future__", "typing": // directives / annotation-only — safe no-op
		return nil
	case "dataclasses":
		for _, n := range names {
			if n != "dataclass" {
				p.errf(tok, "from dataclasses import %q is not supported yet (only 'dataclass')", n)
			}
		}
		return nil
	}
	p.errf(tok, "from-import (`from %s import ...`) is not yet supported; use `import %s` and `%s.name`", mod, mod, mod)
	return nil
}

func (p *Parser) parseDef() *parser.FnDecl {
	line := p.advance().Line // 'def'
	name := p.expectKind(TName).Value
	p.expectOp("(")
	var params []*parser.ParamDecl
	for !p.isOp(")") && p.cur().Kind != TEOF {
		if p.isOp("**") {
			p.errf(p.cur(), "**kwargs is not yet supported")
		}
		variadic := p.acceptOp("*") // *args
		pname := p.expectKind(TName).Value
		var ptype parser.TypeExpr
		if p.acceptOp(":") {
			ptype = p.parseType()
		}
		var def parser.Expr
		if p.acceptOp("=") {
			def = p.parseExpr()
		}
		params = append(params, &parser.ParamDecl{Name: pname, Type: ptype, Default: def, Variadic: variadic})
		if !p.acceptOp(",") {
			break
		}
	}
	p.expectOp(")")
	var ret parser.TypeExpr
	if p.acceptOp("->") {
		ret = p.parseType()
	}
	// Record the signature before the body so recursive calls infer correctly.
	p.fnRet[name] = typeFromExpr(ret)
	p.defParams[name] = params
	if gt, ok := ret.(*parser.GenericType); ok && gt.Name == "PyDict" {
		dm := dictMeta{key: tUnknown, val: tUnknown}
		if len(gt.TypeArgs) == 2 {
			dm = dictMeta{key: typeFromExpr(gt.TypeArgs[0]), val: typeFromExpr(gt.TypeArgs[1])}
		}
		p.fnRetDict[name] = dm
	}
	prevRet := p.currentFnRet
	p.currentFnRet = ret
	p.pushScope()
	for _, pa := range params {
		if pa.Variadic {
			p.declare(pa.Name, tDynamic) // *args is a []any — route ops through dynamic helpers
		} else {
			p.declare(pa.Name, typeFromExpr(pa.Type))
		}
		p.recordParamElem(pa)
		p.recordParamInstance(pa)
	}
	body := p.parseBlock()
	p.popScope()
	p.currentFnRet = prevRet
	ensureTrailingReturn(body, ret)
	return &parser.FnDecl{Line: line, Name: name, Params: params, ReturnType: ret, Body: body}
}

func (p *Parser) parseFor() parser.Stmt {
	line := p.advance().Line // 'for'
	targets := []string{p.expectKind(TName).Value}
	for p.acceptOp(",") {
		if p.isKw("in") {
			break
		}
		targets = append(targets, p.expectKind(TName).Value)
	}
	if !p.isKw("in") {
		p.errf(p.cur(), "expected 'in' in for-statement")
	} else {
		p.advance()
	}
	iter := p.parseExpr()

	// Multi-target `for a, b in iter` (dict.items(), enumerate, zip, list of
	// tuples): iterate via zincpyIter and unpack each element with zincpyGetItem.
	if len(targets) > 1 {
		tmp := fmt.Sprintf("_it%d", p.tmpCount)
		p.tmpCount++
		for _, t := range targets {
			p.declare(t, tDynamic)
		}
		body := p.parseBlock()
		var stmts []parser.Stmt
		for k, t := range targets {
			stmts = append(stmts, &parser.VarStmt{Name: t,
				Value: callIdent("zincpyGetItem", &parser.Ident{Name: tmp}, &parser.IntLit{Value: fmt.Sprintf("%d", k)})})
		}
		stmts = append(stmts, body.Stmts...)
		return &parser.ForStmt{Line: line, IsRange: true, Item: tmp,
			Range: callIdent("zincpyIter", iter),
			Body:  &parser.BlockStmt{Stmts: stmts}}
	}
	item := targets[0]

	rng, isRange := asRange(iter)
	// `for _ in range(n)`: Go can't read `_` as a numeric loop counter, so give
	// the throwaway a fresh name (it's used only in the generated loop header).
	if item == "_" && isRange {
		item = fmt.Sprintf("_i%d", p.tmpCount)
		p.tmpCount++
	}
	// `for k in d` over an ordered-dict iterates keys in insertion order.
	if dm, ok := p.dictMetaOf(iter); ok {
		p.declare(item, dm.key)
		body := p.parseBlock()
		return &parser.ForStmt{Line: line, IsRange: true, Item: item,
			Range: &parser.CallExpr{Callee: &parser.SelectorExpr{Object: iter, Field: "Keys"}},
			Body:  body}
	}
	// `for x in s` over a set → iterate its elements (insertion order).
	if p.isSetVar(iter) {
		p.declare(item, tDynamic)
		body := p.parseBlock()
		return &parser.ForStmt{Line: line, IsRange: true, Item: item,
			Range: &parser.CallExpr{Callee: &parser.SelectorExpr{Object: iter, Field: "Keys"}},
			Body:  body}
	}
	// `for x in v` over a dynamic FFI value → range zincpyIter(v) (list elems,
	// dict keys, or string chars); the loop variable is itself dynamic.
	if p.typeOf(iter) == tDynamic {
		p.declare(item, tDynamic)
		body := p.parseBlock()
		return &parser.ForStmt{Line: line, IsRange: true, Item: item,
			Range: callIdent("zincpyIter", iter), Body: body}
	}
	p.declare(item, p.elemTypeOf(iter)) // range→int, list→element type
	body := p.parseBlock()

	fs := &parser.ForStmt{Line: line, IsRange: true, Item: item, Body: body}
	// range(...) lowers to a numeric range expression (Zinc's `a..b`).
	if isRange {
		fs.Range = rng
	} else {
		fs.Range = iter // iterate a collection directly
	}
	return fs
}

// asRange recognizes a call to range(stop) / range(start, stop) and converts
// it to a Zinc RangeExpr (exclusive upper bound, matching Python semantics).
func asRange(e parser.Expr) (*parser.RangeExpr, bool) {
	call, ok := e.(*parser.CallExpr)
	if !ok {
		return nil, false
	}
	id, ok := call.Callee.(*parser.Ident)
	if !ok || id.Name != "range" {
		return nil, false
	}
	switch len(call.Args) {
	case 1:
		return &parser.RangeExpr{Start: &parser.IntLit{Value: "0"}, End: call.Args[0]}, true
	case 2:
		return &parser.RangeExpr{Start: call.Args[0], End: call.Args[1]}, true
	}
	return nil, false
}

func (p *Parser) parseWhile() parser.Stmt {
	line := p.advance().Line // 'while'
	cond := p.truthyWrap(p.parseExpr())
	body := p.parseBlock()
	return &parser.WhileStmt{Line: line, Cond: cond, Body: body}
}

func (p *Parser) parseIf() parser.Stmt {
	line := p.advance().Line // 'if' or 'elif'
	cond := p.truthyWrap(p.parseExpr())
	then := p.parseBlock()
	stmt := &parser.IfStmt{Line: line, Cond: cond, Then: then}

	p.skipNewlines()
	if p.isKw("elif") {
		stmt.ElseStmt = p.parseIf() // recurse: elif == else-if
	} else if p.isKw("else") {
		p.advance()
		stmt.ElseStmt = p.parseBlock()
	}
	return stmt
}

// parseBlock parses `: NEWLINE INDENT stmts DEDENT`.
func (p *Parser) parseBlock() *parser.BlockStmt {
	p.expectOp(":")
	p.expectKind(TNewline)
	p.expectKind(TIndent)
	block := &parser.BlockStmt{}
	for p.cur().Kind != TDedent && p.cur().Kind != TEOF {
		p.skipNewlines()
		if p.cur().Kind == TDedent || p.cur().Kind == TEOF {
			break
		}
		startPos := p.pos
		if s := p.parseStmt(); s != nil {
			block.Stmts = append(block.Stmts, s)
		}
		if p.pos == startPos {
			p.advance()
		}
	}
	p.expectKind(TDedent)
	return block
}

// --- types -------------------------------------------------------------------

func (p *Parser) parseType() parser.TypeExpr {
	// A string annotation is a forward reference (`other: "Vec"`), required in
	// CPython when the type isn't defined yet — treat the contents as the name.
	if p.cur().Kind == TString {
		return &parser.SimpleType{Name: mapPyType(p.advance().Value)}
	}
	name := p.expectKind(TName).Value
	// Subscripted generics: list[int], dict[str, int], set[int].
	if p.isOp("[") {
		p.advance()
		var args []parser.TypeExpr
		for !p.isOp("]") && p.cur().Kind != TEOF {
			args = append(args, p.parseType())
			if !p.acceptOp(",") {
				break
			}
		}
		p.expectOp("]")
		return genericType(name, args)
	}
	return &parser.SimpleType{Name: mapPyType(name)}
}

// genericType maps a Python subscripted type to its zinc generic node. List →
// []T, Map → map[K]V, Set → map[T]struct{} via codegen's formatType.
func genericType(name string, args []parser.TypeExpr) parser.TypeExpr {
	switch name {
	case "list", "List":
		return &parser.GenericType{Name: "List", TypeArgs: args}
	case "dict", "Dict":
		// Ordered dict — see runtime.go zincpyDict. Codegen renders PyDict as
		// the concrete *zincpyDict; the K/V args are kept so the front-end can
		// track value types for read assertions / loop-var typing.
		return &parser.GenericType{Name: "PyDict", TypeArgs: args}
	case "set", "Set":
		return &parser.GenericType{Name: "Set", TypeArgs: args}
	case "tuple", "Tuple":
		if len(args) == 1 {
			return args[0] // a 1-tuple type is just its element
		}
		return &parser.TupleType{Elements: args}
	}
	return &parser.SimpleType{Name: "Any"} // Optional/Union — not yet modelled
}

// mapPyType maps Python type-hint names to the zinc type names the codegen
// understands. Python's float is IEEE double precision, so it maps to zinc
// `double` (Go float64) — matching how float literals are typed, which a bare
// `float` (treated as 32-bit by the typechecker) would not. str→String;
// int/bool pass through.
func mapPyType(name string) string {
	switch name {
	case "str":
		return "String"
	case "float":
		return "double"
	case "int", "bool":
		return name
	}
	return name
}

// --- expressions (precedence climbing) ---------------------------------------

func (p *Parser) parseExpr() parser.Expr { return p.parseTernary() }

// parseTernary handles Python's conditional expression `X if C else Y`
// (lower precedence than or). It lowers to a Zinc IfExpr, which codegen emits
// as an IIFE with an inferred result type.
func (p *Parser) parseTernary() parser.Expr {
	e := p.parseOr()
	if !p.isKw("if") {
		return e
	}
	p.advance() // 'if'
	cond := p.parseOr()
	if p.isKw("else") {
		p.advance()
	} else {
		p.errf(p.cur(), "conditional expression missing 'else'")
	}
	elseE := p.parseTernary() // right-associative
	return &parser.IfExpr{Cond: p.truthyWrap(cond), Then: e, Else: elseE}
}

// truthyWrap wraps a non-bool expression in zincpyTruthy so it can be used in
// a boolean context (Go requires a bool; Python accepts any value).
func (p *Parser) truthyWrap(e parser.Expr) parser.Expr {
	if p.typeOf(e) == tBool {
		return e
	}
	return callIdent("zincpyTruthy", e)
}

func (p *Parser) parseOr() parser.Expr {
	left := p.parseAnd()
	for p.isKw("or") {
		p.advance()
		right := p.parseAnd()
		left = &parser.BinaryExpr{Left: p.truthyWrap(left), Op: "||", Right: p.truthyWrap(right)}
	}
	return left
}

func (p *Parser) parseAnd() parser.Expr {
	left := p.parseNot()
	for p.isKw("and") {
		p.advance()
		right := p.parseNot()
		left = &parser.BinaryExpr{Left: p.truthyWrap(left), Op: "&&", Right: p.truthyWrap(right)}
	}
	return left
}

func (p *Parser) parseNot() parser.Expr {
	if p.isKw("not") {
		p.advance()
		return &parser.UnaryExpr{Op: "!", Operand: p.truthyWrap(p.parseNot())}
	}
	return p.parseComparison()
}

func (p *Parser) parseComparison() parser.Expr {
	left := p.parseAdditive()
	for {
		if p.cur().Kind == TOp {
			switch p.cur().Value {
			case "==", "!=", "<", ">", "<=", ">=":
				op := p.advance().Value
				left = p.numericBinary(left, op, p.parseAdditive())
				continue
			}
		}
		// membership: `x in c` → zincpyIn(x, c); `x not in c` → !zincpyIn(...).
		if p.isKw("in") {
			p.advance()
			left = callIdent("zincpyIn", left, p.parseAdditive())
			continue
		}
		if p.isKw("not") && p.peekKw("in") {
			p.advance()
			p.advance()
			left = &parser.UnaryExpr{Op: "!", Operand: callIdent("zincpyIn", left, p.parseAdditive())}
			continue
		}
		return left
	}
}

func (p *Parser) parseAdditive() parser.Expr {
	left := p.parseMultiplicative()
	for p.cur().Kind == TOp {
		switch p.cur().Value {
		case "+", "-":
			op := p.advance().Value
			left = p.numericBinary(left, op, p.parseMultiplicative())
		default:
			return left
		}
	}
	return left
}

func (p *Parser) parseMultiplicative() parser.Expr {
	left := p.parseUnary()
	for p.cur().Kind == TOp {
		op := p.cur().Value
		switch op {
		case "*":
			p.advance()
			left = p.numericBinary(left, op, p.parseUnary())
		case "%":
			// Python `%` follows the divisor's sign (Go follows the
			// dividend), so int modulo lowers to a runtime helper. Non-int
			// `%` (rare) falls back to Go's operator.
			p.advance()
			right := p.parseUnary()
			if p.typeOf(left) == tDynamic || p.typeOf(right) == tDynamic {
				left = callIdent("zincpyModDyn", left, right)
			} else if p.typeOf(left) == tInt && p.typeOf(right) == tInt {
				left = callIdent(rtMod, left, right)
			} else {
				left = p.numericBinary(left, "%", right)
			}
		case "/":
			// Python `/` is true division: always float, and 1/0 raises
			// ZeroDivisionError. Lower to zincpyDiv(float(a), float(b)).
			p.advance()
			right := p.parseUnary()
			if p.typeOf(left) == tDynamic || p.typeOf(right) == tDynamic {
				left = callIdent("zincpyTrueDiv", left, right)
			} else {
				left = callIdent(rtDiv, floatWrap(left), floatWrap(right))
			}
		case "//":
			// Python `//` is floor division (toward -inf), which Go's `/`
			// (truncates toward zero) doesn't match for mixed signs. Lower
			// to a runtime helper.
			p.advance()
			right := p.parseUnary()
			if p.typeOf(left) == tDynamic || p.typeOf(right) == tDynamic {
				left = callIdent("zincpyFloorDivDyn", left, right)
			} else {
				left = callIdent(rtFloorDiv, left, right)
			}
		default:
			return left
		}
	}
	return left
}

func (p *Parser) parseUnary() parser.Expr {
	if p.isOp("-") || p.isOp("+") {
		op := p.advance().Value
		return &parser.UnaryExpr{Op: op, Operand: p.parseUnary()}
	}
	return p.parsePostfix()
}

func (p *Parser) parsePostfix() parser.Expr {
	e := p.parseAtom()
	for {
		switch {
		case p.isOp("("):
			e = p.parseCall(e)
		case p.isOp("."):
			p.advance()
			field := p.expectKind(TName).Value
			// FFI module attribute read (`math.pi`) lowers to zincpyPyGet —
			// unless it is immediately called (`math.sqrt(...)`), which
			// parseCall lowers to zincpyPyCall.
			if v, ok := p.classConst(e, field); ok {
				// Class constant (Class.NAME / self.NAME / instance.NAME) —
				// inline the literal value.
				e = v
			} else if id, ok := e.(*parser.Ident); ok && p.isFFIModule(id.Name) && !p.isOp("(") {
				e = callIdent("zincpyPyGet",
					&parser.StringLit{Value: p.ffiModBind[id.Name]},
					&parser.StringLit{Value: field})
			} else if !p.isOp("(") && p.isClassProp(p.exprClass(e), field) {
				// @property read `obj.x` → method call `obj.x()`.
				e = &parser.CallExpr{Callee: &parser.SelectorExpr{Object: e, Field: field}}
			} else {
				e = &parser.SelectorExpr{Object: e, Field: field}
			}
		case p.isOp("["):
			p.advance()
			var start parser.Expr
			if !p.isOp(":") {
				start = p.parseExpr()
			}
			if p.isOp(":") {
				// slice obj[start:stop:step] → zincpySlice (omitted → None/nil)
				p.advance()
				var stop, step parser.Expr
				if !p.isOp(":") && !p.isOp("]") {
					stop = p.parseExpr()
				}
				if p.acceptOp(":") {
					if !p.isOp("]") {
						step = p.parseExpr()
					}
				}
				p.expectOp("]")
				e = sliceCall(e, start, stop, step)
				break
			}
			idx := start
			p.expectOp("]")
			if dm, ok := p.dictMetaOf(e); ok {
				e = dictGetExpr(e, idx, dm.val) // d[k] → any(d.Get(k)).(V)
			} else if p.typeOf(e) == tDynamic || p.typeOf(e) == tStr {
				// FFI value, or a string (Go string indexing yields a byte, not
				// a 1-char string) → runtime dispatch (also handles negatives).
				e = callIdent("zincpyGetItem", e, idx)
			} else if isNegLiteral(idx) {
				// xs[-k] on a native sequence → xs[len(xs)+(-k)] (stays typed).
				e = &parser.IndexExpr{Object: e, Index: &parser.BinaryExpr{
					Left: callIdent("len", e), Op: "+", Right: idx}}
			} else {
				e = &parser.IndexExpr{Object: e, Index: idx}
			}
		default:
			return e
		}
	}
}

// parseListLit parses a Python list literal [a, b, c] or a list comprehension
// [expr for x in it if cond]. A trailing comma is allowed; an empty [] becomes
// an untyped list (codegen → []interface{}).
func (p *Parser) parseListLit() parser.Expr {
	p.expectOp("[")
	if p.isOp("]") {
		p.advance()
		return &parser.ListLit{}
	}
	first := p.parseExpr()
	if p.isKw("for") {
		return p.parseListComprehension(first)
	}
	lit := &parser.ListLit{Elements: []parser.Expr{first}}
	for p.acceptOp(",") {
		if p.isOp("]") {
			break
		}
		lit.Elements = append(lit.Elements, p.parseExpr())
	}
	p.expectOp("]")
	return lit
}

// parseDictLit parses a Python dict literal {k: v, ...} and lowers it to an
// ordered-dict constructor IIFE:
//
//	func() *zincpyDict { _d := zincpyNewDict(); _d.Set(k, v); ...; return _d }()
//
// A bare `{...}` without colons is a set literal, which isn't modelled yet.
func (p *Parser) parseDictLit() parser.Expr {
	p.expectOp("{")
	// `{}` is an empty dict (Python); `set()` makes an empty set.
	if p.isOp("}") {
		p.advance()
		return p.buildDictLit(nil, nil)
	}
	outStart := p.pos // remember the output position for comprehension re-parse
	first := p.parseExpr()
	if !p.isOp(":") {
		if p.isKw("for") {
			return p.parseSetComprehension(outStart) // {expr for x in it}
		}
		// Set literal `{a, b, c}` (no colons).
		elems := []parser.Expr{first}
		for p.acceptOp(",") {
			if p.isOp("}") {
				break
			}
			elems = append(elems, p.parseExpr())
		}
		p.expectOp("}")
		return p.buildSetLit(elems)
	}
	// Dict literal or comprehension `{k: v ...}`.
	p.expectOp(":")
	firstVal := p.parseExpr()
	if p.isKw("for") {
		return p.parseDictComprehension(outStart) // {k: v for x in it}
	}
	keys := []parser.Expr{first}
	vals := []parser.Expr{firstVal}
	for p.acceptOp(",") {
		if p.isOp("}") {
			break
		}
		keys = append(keys, p.parseExpr())
		p.expectOp(":")
		vals = append(vals, p.parseExpr())
	}
	p.expectOp("}")
	return p.buildDictLit(keys, vals)
}

// buildSetLit builds the IIFE constructor for a set literal:
// func() *zincpySet { _s := zincpyNewSet(); _s.Add(e); ...; return _s }()
func (p *Parser) buildSetLit(elems []parser.Expr) parser.Expr {
	s := fmt.Sprintf("_s%d", p.tmpCount)
	p.tmpCount++
	stmts := []parser.Stmt{&parser.VarStmt{Name: s, Value: callIdent("zincpyNewSet")}}
	for _, e := range elems {
		stmts = append(stmts, &parser.ExprStmt{Expr: &parser.CallExpr{
			Callee: &parser.SelectorExpr{Object: &parser.Ident{Name: s}, Field: "Add"},
			Args:   []parser.Expr{e},
		}})
	}
	stmts = append(stmts, &parser.ReturnStmt{Value: &parser.Ident{Name: s}})
	iife := &parser.CallExpr{Callee: &parser.LambdaExpr{
		ReturnType: &parser.SimpleType{Name: "*zincpySet"},
		Body:       &parser.BlockStmt{Stmts: stmts},
	}}
	p.setExprMeta[iife] = true
	return iife
}

func (p *Parser) buildDictLit(keys, vals []parser.Expr) parser.Expr {
	d := fmt.Sprintf("_d%d", p.tmpCount)
	p.tmpCount++
	stmts := []parser.Stmt{
		&parser.VarStmt{Name: d, Value: callIdent("zincpyNewDict")},
	}
	for i := range keys {
		stmts = append(stmts, &parser.ExprStmt{Expr: &parser.CallExpr{
			Callee: &parser.SelectorExpr{Object: &parser.Ident{Name: d}, Field: "Set"},
			Args:   []parser.Expr{keys[i], vals[i]},
		}})
	}
	stmts = append(stmts, &parser.ReturnStmt{Value: &parser.Ident{Name: d}})

	iife := &parser.CallExpr{Callee: &parser.LambdaExpr{
		ReturnType: &parser.SimpleType{Name: "*zincpyDict"},
		Body:       &parser.BlockStmt{Stmts: stmts},
	}}

	meta := dictMeta{key: tUnknown, val: tUnknown}
	if len(keys) > 0 {
		meta = dictMeta{key: p.typeOf(keys[0]), val: p.typeOf(vals[0])}
	}
	p.dictExprMeta[iife] = meta
	return iife
}

// dictMetaOf returns the dict metadata for an expression that is a known
// dict-typed identifier.
func (p *Parser) dictMetaOf(e parser.Expr) (dictMeta, bool) {
	if id, ok := e.(*parser.Ident); ok {
		dm, ok := p.dictVars[id.Name]
		return dm, ok
	}
	return dictMeta{}, false
}

// dictMetaOfValue returns the dict metadata for an assignment RHS that
// produces a dict: a dict literal (tracked via dictExprMeta) or a call to a
// dict-returning function.
func (p *Parser) dictMetaOfValue(rhs parser.Expr) (dictMeta, bool) {
	if dm, ok := p.dictExprMeta[rhs]; ok {
		return dm, true
	}
	if call, ok := rhs.(*parser.CallExpr); ok {
		if id, ok := call.Callee.(*parser.Ident); ok {
			if dm, ok := p.fnRetDict[id.Name]; ok {
				return dm, true
			}
		}
	}
	return dictMeta{}, false
}

// dictGetExpr builds a typed dict read: `any(obj.Get(key)).(ValT)`.
func dictGetExpr(obj, key parser.Expr, val pytype) parser.Expr {
	return &parser.TypeAssertExpr{
		Object: &parser.CallExpr{
			Callee: &parser.SelectorExpr{Object: obj, Field: "Get"},
			Args:   []parser.Expr{key},
		},
		TypeExpr: zincTypeForPy(val),
	}
}

// asDictSetTarget recognizes the read shape produced by dictGetExpr when it
// appears as an assignment target, returning the dict object and key so the
// statement can lower to `obj.Set(key, value)`.
func asDictSetTarget(e parser.Expr) (obj, key parser.Expr, ok bool) {
	ta, ok := e.(*parser.TypeAssertExpr)
	if !ok {
		return nil, nil, false
	}
	call, ok := ta.Object.(*parser.CallExpr)
	if !ok {
		return nil, nil, false
	}
	sel, ok := call.Callee.(*parser.SelectorExpr)
	if !ok || sel.Field != "Get" || len(call.Args) != 1 {
		return nil, nil, false
	}
	return sel.Object, call.Args[0], true
}

// resolveDefaults rewrites a call to a known function/constructor into a plain
// positional call, placing keyword args by name and filling omitted parameters
// from their defaults. Returns ok=false when the callee is unknown or the call
// is already a complete positional call (nothing to do).
func (p *Parser) resolveDefaults(call *parser.CallExpr) (parser.Expr, bool) {
	id, ok := call.Callee.(*parser.Ident)
	if !ok {
		return nil, false
	}
	params, ok := p.defParams[id.Name]
	if !ok {
		return nil, false
	}
	// Variadic functions: let Go's native variadic spread handle the call.
	if len(params) > 0 && params[len(params)-1].Variadic {
		return nil, false
	}
	if len(call.NamedArgs) == 0 && len(call.Args) >= len(params) {
		return nil, false // complete positional call — leave as-is
	}
	final := make([]parser.Expr, len(params))
	filled := make([]bool, len(params))
	for i, a := range call.Args {
		if i >= len(params) {
			break
		}
		final[i], filled[i] = a, true
	}
	for _, na := range call.NamedArgs {
		idx := -1
		for i, pa := range params {
			if pa.Name == na.Name {
				idx = i
				break
			}
		}
		if idx < 0 {
			p.errf(p.cur(), "%s() got an unexpected keyword argument %q", id.Name, na.Name)
			return nil, false
		}
		final[idx], filled[idx] = na.Value, true
	}
	for i, pa := range params {
		if !filled[i] {
			if pa.Default == nil {
				p.errf(p.cur(), "%s() missing required argument %q", id.Name, pa.Name)
				return nil, false
			}
			final[i] = pa.Default
		}
	}
	return &parser.CallExpr{Callee: call.Callee, Args: final}, true
}

func (p *Parser) parseCall(callee parser.Expr) parser.Expr {
	p.expectOp("(")
	call := &parser.CallExpr{Callee: callee}
	for !p.isOp(")") && p.cur().Kind != TEOF {
		// keyword argument `name = value` (but not `name == value`).
		if p.cur().Kind == TName && p.peekOp("=") {
			name := p.advance().Value
			p.advance() // '='
			call.NamedArgs = append(call.NamedArgs, parser.NamedArg{Name: name, Value: p.parseExpr()})
		} else {
			call.Args = append(call.Args, p.parseExpr())
		}
		if !p.acceptOp(",") {
			break
		}
	}
	p.expectOp(")")
	// Resolve keyword args / omitted defaults of a known function or
	// constructor into a plain positional call.
	if resolved, ok := p.resolveDefaults(call); ok {
		return resolved
	}

	// len(d) on an ordered-dict → d.Len() (Go's len doesn't work on it).
	if id, ok := callee.(*parser.Ident); ok && id.Name == "len" && len(call.Args) == 1 {
		if _, isDict := p.dictMetaOf(call.Args[0]); isDict {
			return &parser.CallExpr{Callee: &parser.SelectorExpr{Object: call.Args[0], Field: "Len"}}
		}
		if p.typeOf(call.Args[0]) == tDynamic {
			return callIdent("zincpyLen", call.Args[0])
		}
		if p.isSetVar(call.Args[0]) {
			return &parser.CallExpr{Callee: &parser.SelectorExpr{Object: call.Args[0], Field: "Len"}}
		}
		// len(obj) on a class instance defining __len__ → obj.Len().
		if cls := p.exprClass(call.Args[0]); cls != "" && p.classHasMethod(cls, "Len") {
			return &parser.CallExpr{Callee: &parser.SelectorExpr{Object: call.Args[0], Field: "Len"}}
		}
	}
	// Sequence builtins min/max/sum/sorted/abs → reflection-based runtime
	// helpers (result is dynamic). min/max also take the varargs form
	// min(a, b, ...) → wrap the args in a list.
	if id, ok := callee.(*parser.Ident); ok && len(call.NamedArgs) == 0 {
		switch id.Name {
		case "min", "max":
			helper := "zincpyMin"
			if id.Name == "max" {
				helper = "zincpyMax"
			}
			if len(call.Args) == 1 {
				return callIdent(helper, call.Args[0])
			}
			if len(call.Args) >= 2 {
				return callIdent(helper, &parser.ListLit{Elements: call.Args})
			}
		case "sum":
			if len(call.Args) == 1 {
				return callIdent("zincpySum", call.Args[0])
			}
		case "sorted":
			if len(call.Args) == 1 {
				return callIdent("zincpySorted", call.Args[0])
			}
		case "abs":
			if len(call.Args) == 1 {
				return callIdent("zincpyAbs", call.Args[0])
			}
		case "set":
			if len(call.Args) == 0 {
				return callIdent("zincpyNewSet")
			}
			if len(call.Args) == 1 {
				return callIdent("zincpySetOf", call.Args[0])
			}
		case "enumerate":
			if len(call.Args) == 1 {
				return callIdent("zincpyEnumerate", call.Args[0])
			}
		case "zip":
			if len(call.Args) >= 1 {
				return callIdent("zincpyZip", call.Args...)
			}
		}
	}
	// float()/int()/str() on a dynamic FFI value → runtime coercion helpers,
	// since Go's float64(x)/int(x) conversions reject interface{}.
	if id, ok := callee.(*parser.Ident); ok && len(call.Args) == 1 && p.typeOf(call.Args[0]) == tDynamic {
		switch id.Name {
		case "float":
			return callIdent("zincpyToFloat", call.Args[0])
		case "int":
			return callIdent("zincpyToInt", call.Args[0])
		case "str":
			return callIdent("zincpyStr", call.Args[0])
		}
	}
	// d.keys()/d.values() on an ordered-dict → d.Keys()/d.Values(). Capitalize
	// so codegen's lowercase collection-builtin dispatch doesn't intercept.
	if sel, ok := callee.(*parser.SelectorExpr); ok {
		if _, isDict := p.dictMetaOf(sel.Object); isDict {
			switch sel.Field {
			case "keys":
				sel.Field = "Keys"
			case "values":
				sel.Field = "Values"
			case "items":
				sel.Field = "Items"
			}
		}
		// set mutators on a set var → capitalized runtime methods.
		if p.isSetVar(sel.Object) {
			switch sel.Field {
			case "add":
				sel.Field = "Add"
			case "discard":
				sel.Field = "Discard"
			case "remove":
				sel.Field = "Remove"
			}
		}
	}
	// static/class method call: Class.method(args) / obj.method(args) →
	// Class_method(args) (the lifted package function).
	if sel, ok := callee.(*parser.SelectorExpr); ok {
		cls := ""
		if id, ok := sel.Object.(*parser.Ident); ok && p.classNames[id.Name] {
			cls = id.Name
		} else {
			cls = p.exprClass(sel.Object)
		}
		if cls != "" {
			if st, ok := p.classStatics[cls]; ok && st[sel.Field] {
				return &parser.CallExpr{Callee: &parser.Ident{Name: cls + "_" + sel.Field}, Args: call.Args}
			}
		}
	}
	// FFI module call (`math.sqrt(x)`) → zincpyPyCall("math", "sqrt", x).
	if sel, ok := callee.(*parser.SelectorExpr); ok {
		if id, ok := sel.Object.(*parser.Ident); ok && p.isFFIModule(id.Name) {
			ffiArgs := append([]parser.Expr{
				&parser.StringLit{Value: p.ffiModBind[id.Name]},
				&parser.StringLit{Value: sel.Field},
			}, call.Args...)
			return callIdent("zincpyPyCall", ffiArgs...)
		}
	}
	// str methods on a string-typed receiver → runtime helpers with Python
	// semantics (e.g. join's swapped arg order, no-arg split on whitespace).
	if lowered := p.lowerStrMethod(callee, call.Args); lowered != nil {
		return lowered
	}
	return call
}

func (p *Parser) parseAtom() parser.Expr {
	t := p.cur()
	switch t.Kind {
	case TNumber:
		p.advance()
		if isFloatLit(t.Value) {
			return &parser.FloatLit{Value: t.Value}
		}
		return &parser.IntLit{Value: t.Value}
	case TString:
		p.advance()
		return &parser.StringLit{Value: t.Value}
	case TFString:
		p.advance()
		return p.parseFString(t)
	case TName:
		p.advance()
		switch t.Value {
		case "True":
			return &parser.BoolLit{Value: true}
		case "False":
			return &parser.BoolLit{Value: false}
		case "None":
			return &parser.NullLit{}
		case "self":
			if p.inMethod {
				return &parser.ThisExpr{}
			}
		case "cls":
			// Inside a @classmethod, `cls` is the class: cls(...) constructs,
			// cls.X reads a class member.
			if p.clsAlias != "" {
				return &parser.Ident{Name: p.clsAlias}
			}
		}
		return &parser.Ident{Name: t.Value}
	case TOp:
		switch t.Value {
		case "(":
			p.advance()
			e := p.parseTestList() // grouping, or `(a, b)` tuple
			p.expectOp(")")
			// A parenthesized tuple literal is a first-class tuple VALUE
			// (stored, indexed, printed with parens) — distinct from the bare
			// `a, b` multi-return/unpack form, which parseTestList leaves as a
			// TupleLit for return/assignment to consume.
			if tup, ok := e.(*parser.TupleLit); ok {
				return callIdent("zincpyNewTuple", tup.Elements...)
			}
			return e
		case "[":
			return p.parseListLit()
		case "{":
			return p.parseDictLit()
		}
	}
	p.errf(t, "unexpected token %s %q", t.Kind, t.Value)
	p.advance()
	return &parser.IntLit{Value: "0"} // error placeholder
}

func isFloatLit(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			return true
		}
	}
	return false
}
