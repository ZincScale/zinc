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
	rtPrintSep = "zincpyPrintSep"
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

// goReserved are Go keywords that are valid Python identifiers and that the
// front-end does NOT special-case (range/map are deliberately excluded — they
// are Python builtins handled by name). A Python variable/param/function with
// one of these names is renamed with a trailing underscore so the emitted Go
// is valid. Applied consistently at every identifier site, declarations and
// references alike, so the rename stays internally consistent.
var goReserved = map[string]bool{
	"type": true, "func": true, "var": true, "const": true, "chan": true,
	"go": true, "goto": true, "interface": true, "package": true,
	"select": true, "struct": true, "switch": true, "case": true,
	"default": true, "defer": true, "fallthrough": true,
}

// goSafe renames a Python identifier that collides with a Go keyword.
func goSafe(name string) string {
	if goReserved[name] {
		return name + "_"
	}
	return name
}

// coerceEmptyList gives an empty list literal `[]` the element type of the
// context it flows into (a typed parameter or return type), so it emits []T{}
// rather than []interface{}{} and matches the target type.
func coerceEmptyList(arg parser.Expr, target parser.TypeExpr) {
	lit, ok := arg.(*parser.ListLit)
	if !ok || len(lit.Elements) > 0 || lit.ExplicitType != nil {
		return
	}
	if gt, ok := target.(*parser.GenericType); ok && gt.Name == "List" {
		lit.ExplicitType = gt
	}
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

// namedArg returns the value of a call's keyword argument by name.
func namedArg(call *parser.CallExpr, name string) (parser.Expr, bool) {
	for _, na := range call.NamedArgs {
		if na.Name == name {
			return na.Value, true
		}
	}
	return nil, false
}

// isNonNegIntLit reports whether e is a plain integer literal (always
// non-negative; negatives parse as UnaryExpr) — a definitely-in-range index
// that can be emitted directly without negative-index wrapping.
func isNonNegIntLit(e parser.Expr) bool {
	_, ok := e.(*parser.IntLit)
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

	// lambdaVars marks variables bound to a lambda, so calling one yields a
	// dynamic result (the lambda returns interface{}).
	lambdaVars map[string]bool

	// classFieldDecl maps class→field→FieldDecl so an empty-list field's
	// element type can be refined in place from later `self.f.append(v)` calls.
	classFieldDecl map[string]map[string]*parser.FieldDecl

	// classParent maps a class to its (first) base class name, so a
	// `super().method(args)` call inside a method body lowers to the embedded
	// parent's method `this.<Parent>.Method(args)`.
	classParent map[string]string
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
		lambdaVars:     map[string]bool{},
		classFieldDecl: map[string]map[string]*parser.FieldDecl{},
		classParent:    map[string]string{},
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
		case "with":
			return p.parseWith()
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

// parseAnnotatedAssign parses `x: T = value` (the leading `x` is already
// consumed as id, the cursor is on `:`). The annotation pins the local's type;
// a dynamic value is coerced to a scalar annotation at the boundary, and an
// empty list literal takes the annotated element type.
// rewritePrintKwargs lowers print(*args, sep=S, end=E) to a zincpyPrintSep call
// with S and E threaded ahead of the positional args (defaulting to " " and
// "\n"). Only sep/end are recognized — `file=`/`flush=` and any other keyword
// is rejected with a clear message.
func (p *Parser) rewritePrintKwargs(line int, call *parser.CallExpr) {
	sep := parser.Expr(&parser.StringLit{Value: " "})
	end := parser.Expr(&parser.StringLit{Value: "\n"})
	for _, na := range call.NamedArgs {
		switch na.Name {
		case "sep":
			sep = na.Value
		case "end":
			end = na.Value
		default:
			p.errf(Token{Line: line}, "print() keyword %q is not supported (only sep= and end=)", na.Name)
		}
	}
	call.Callee = &parser.Ident{Name: rtPrintSep}
	call.Args = append([]parser.Expr{sep, end}, call.Args...)
	call.NamedArgs = nil
}

func (p *Parser) parseAnnotatedAssign(line int, id *parser.Ident) parser.Stmt {
	p.advance() // ':'
	typ := p.parseType()
	if !p.isOp("=") {
		// Annotation-only `x: T` doesn't bind a value at runtime in Python and
		// leaves no Go declaration to anchor; require an initializer.
		p.errf(p.cur(), "annotation-only statement %q: T is not supported; give it a value (%s: T = ...)", id.Name, id.Name)
		p.endSimple()
		return &parser.ExprStmt{Line: line, Expr: id}
	}
	p.advance() // '='
	rhs := p.parseExpr()
	p.endSimple()
	if p.isDeclared(id.Name) {
		p.errf(Token{Line: line}, "variable %q is already declared; re-annotating it is not supported", id.Name)
	}
	// The annotation drives the value's type: narrow a dynamic boundary value to
	// a scalar annotation, and fill an empty `[]` with the annotated element type.
	coerceEmptyList(rhs, typ)
	rhs = p.coerceDynamicTo(rhs, typ)
	p.declare(id.Name, typeFromExpr(typ))
	p.recordElemType(id.Name, rhs)
	p.recordInstanceClass(id.Name, rhs)
	p.recordSetVar(id.Name, rhs)
	if dm, ok := p.dictMetaOfValue(rhs); ok {
		p.dictVars[id.Name] = dm
	}
	return &parser.VarStmt{Line: line, Name: id.Name, Type: typ, Value: rhs}
}

func (p *Parser) parseSimpleStmt() parser.Stmt {
	line := p.cur().Line
	lhs := p.parseExpr()

	// Annotated assignment (PEP 526): `x: T = value`. The annotation is
	// authoritative — it pins the local's static type and drives boundary
	// narrowing, so `x: int = json.loads(s)` coerces the dynamic value to int
	// rather than erroring. `x: Any = value` is the explicit escape hatch for a
	// genuinely-dynamic local (interface{}). Only a bare-name target is allowed.
	if id, ok := lhs.(*parser.Ident); ok && p.isOp(":") {
		return p.parseAnnotatedAssign(line, id)
	}

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
					if _, isLambda := rhs.(*parser.LambdaExpr); isLambda {
						p.lambdaVars[id.Name] = true
					}
					if dm, ok := p.dictMetaOfValue(rhs); ok {
						p.dictVars[id.Name] = dm
					}
					return &parser.VarStmt{Line: line, Name: id.Name, Value: rhs}
				}
				// Type-checker-clean contract: a statically-typed variable keeps
				// one type. We enforce this as strictly as a strict mypy config
				// would, rejecting at parse time with a clear message rather than
				// leaking a downstream Go type error. Two violations, both only
				// when the existing type is a known concrete (non-dynamic) type:
				if old := p.lookupType(id.Name); old != tUnknown && old != tDynamic {
					nw := p.typeOf(rhs)
					switch {
					case nw == tDynamic:
						// A dynamic value (FFI/deserialization result, Any) flowing
						// into a statically-typed local. Strict mypy flags this
						// implicit-Any leak; narrow it at the boundary instead
						// (e.g. int(x)/float(x)/str(x), or validate into a typed
						// model) so the local stays its static type.
						p.errf(Token{Line: line}, "variable %q is %s but reassigned from a dynamic value; zinc-py enforces static types — narrow it at the boundary (e.g. %s(...) or a typed model)", id.Name, old, narrowHint(old))
					case nw != tUnknown && old != nw:
						// e.g. int → float: a genuine type change mypy rejects and
						// Go's single-typed vars can't express.
						p.errf(Token{Line: line}, "variable %q is %s but reassigned to %s; zinc-py requires type-consistent variables (mypy would reject this)", id.Name, old, nw)
					}
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
			// `self.field.append(v)` refines an empty-list field's element type.
			if fld, ok := sel.Object.(*parser.SelectorExpr); ok {
				if _, isThis := fld.Object.(*parser.ThisExpr); isThis {
					p.refineListFieldElem(fld.Field, call.Args[0])
				}
			}
			return &parser.AssignStmt{
				Line: line, Target: sel.Object, Op: "=",
				Value: callIdent("append", sel.Object, call.Args[0]),
			}
		}
	}

	// print(...): route to the runtime shim so floats/bools format the
	// Python way (3.0 not 3, True not true) rather than Go's fmt defaults.
	// One arg → zincpyPrint; zero or many → zincpyPrintN (space-separated,
	// matching Python's default sep). The sep=/end= keywords route to
	// zincpyPrintSep with the sep/end values threaded ahead of the positionals.
	if call, ok := lhs.(*parser.CallExpr); ok {
		if id, ok := call.Callee.(*parser.Ident); ok && id.Name == "print" {
			if len(call.NamedArgs) != 0 {
				p.rewritePrintKwargs(line, call)
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

// coerceDynamicTo wraps a dynamic value (e.g. derived from an unannotated /
// duck-typed param, or an FFI result) being returned from a function with a
// concrete scalar return type, so the value's interface{} representation
// becomes the Go return type. The helpers reproduce Python int()/float()/str()
// and a bool pass-through; for a well-annotated program the value already holds
// that type so the conversion is identity. Non-scalar return types (classes,
// containers) are left alone.
func (p *Parser) coerceDynamicTo(e parser.Expr, t parser.TypeExpr) parser.Expr {
	if p.typeOf(e) != tDynamic {
		return e
	}
	st, ok := t.(*parser.SimpleType)
	if !ok {
		return e
	}
	switch st.Name {
	case "int":
		return callIdent("zincpyToInt", e)
	case "float", "double":
		return callIdent("zincpyToFloat", e)
	case "String", "string":
		return callIdent("zincpyStr", e)
	case "bool":
		return callIdent("zincpyToBool", e)
	}
	return e
}

func (p *Parser) parseReturn() parser.Stmt {
	line := p.advance().Line // 'return'
	var val parser.Expr
	if p.cur().Kind != TNewline && p.cur().Kind != TEOF {
		val = p.parseTestList()
	}
	p.endSimple()
	if val != nil && p.currentFnRet != nil {
		coerceEmptyList(val, p.currentFnRet) // `return []` from a `-> list[T]` fn
		val = p.coerceDynamicTo(val, p.currentFnRet)
	}
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

// paramPyType is the pytype to declare for a parameter. An unannotated param
// (like *args) carries no static type, so it is dynamic: its uses route
// through the runtime helpers (arithmetic, indexing, iteration on `any`),
// matching Python's duck typing. An annotated param uses its hint.
func paramPyType(pa *parser.ParamDecl) pytype {
	if pa.Variadic || pa.Type == nil {
		return tDynamic
	}
	return typeFromExpr(pa.Type)
}

// voidIfNone maps a `-> None` return annotation to a void return (nil type).
// Python spells "returns nothing" as None; Go spells it as an absent return
// type, and a literal "None" type would be an undefined Go identifier.
func voidIfNone(t parser.TypeExpr) parser.TypeExpr {
	if st, ok := t.(*parser.SimpleType); ok && st.Name == "None" {
		return nil
	}
	return t
}

func (p *Parser) parseDef() *parser.FnDecl {
	line := p.advance().Line // 'def'
	name := goSafe(p.expectKind(TName).Value)
	p.expectOp("(")
	var params []*parser.ParamDecl
	for !p.isOp(")") && p.cur().Kind != TEOF {
		if p.isOp("**") {
			p.errf(p.cur(), "**kwargs is not yet supported")
		}
		variadic := p.acceptOp("*") // *args
		pname := goSafe(p.expectKind(TName).Value)
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
		ret = voidIfNone(p.parseType())
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
		p.declare(pa.Name, paramPyType(pa))
		p.recordParamElem(pa)
		p.recordParamInstance(pa)
	}
	body := p.parseBlock()
	p.popScope()
	p.currentFnRet = prevRet
	// An unannotated function that returns a value gets a dynamic (interface{})
	// Go return type rather than void — so a duck-typed `def add(a, b): return
	// a + b` returns the (boxed) result instead of failing as "too many return
	// values". Its uses route through the dynamic helpers (fnRet → tDynamic).
	if ret == nil && returnsValueInBlock(body) {
		ret = &parser.SimpleType{Name: "any"}
		p.fnRet[name] = tDynamic
	}
	ensureTrailingReturn(body, ret)
	return &parser.FnDecl{Line: line, Name: name, Params: params, ReturnType: ret, Body: body}
}

func (p *Parser) parseFor() parser.Stmt {
	line := p.advance().Line // 'for'
	targets := []string{goSafe(p.expectKind(TName).Value)}
	for p.acceptOp(",") {
		if p.isKw("in") {
			break
		}
		targets = append(targets, goSafe(p.expectKind(TName).Value))
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
		stmts = append(stmts, blankUse(targets)...)
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

// parseWith lowers `with expr as name: body` (context managers) to an IIFE
// that calls __enter__ (Enter), defers __exit__ (Exit), then runs the body —
// the IIFE scopes the defer to the with-block. __enter__ conventionally
// returns self, so `name` binds to the context object. Limitation: a `return`
// inside the body escapes only the IIFE, not the enclosing function.
func (p *Parser) parseWith() parser.Stmt {
	line := p.advance().Line // 'with'
	ctx := p.parseExpr()
	var name string
	if p.isKw("as") {
		p.advance()
		name = p.expectKind(TName).Value
	}
	tmp := fmt.Sprintf("_cm%d", p.tmpCount)
	p.tmpCount++
	cls := p.exprClass(ctx)
	if name != "" {
		p.declare(name, tUnknown)
		if cls != "" {
			p.instanceClass[name] = cls // __enter__ returns self → name is an instance
		}
	}
	body := p.parseBlock()

	enter := &parser.CallExpr{Callee: &parser.SelectorExpr{Object: &parser.Ident{Name: tmp}, Field: "Enter"}}
	stmts := []parser.Stmt{&parser.VarStmt{Name: tmp, Value: ctx}}
	if name != "" {
		stmts = append(stmts, &parser.VarStmt{Name: name, Value: enter})
	} else {
		stmts = append(stmts, &parser.ExprStmt{Expr: enter})
	}
	stmts = append(stmts, &parser.DeferStmt{Expr: &parser.CallExpr{
		Callee: &parser.SelectorExpr{Object: &parser.Ident{Name: tmp}, Field: "Exit"},
		Args:   []parser.Expr{&parser.NullLit{}, &parser.NullLit{}, &parser.NullLit{}},
	}})
	stmts = append(stmts, body.Stmts...)
	iife := &parser.CallExpr{Callee: &parser.LambdaExpr{Body: &parser.BlockStmt{Stmts: stmts}}}
	return &parser.ExprStmt{Line: line, Expr: iife}
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
	return &parser.SimpleType{Name: "any"} // Optional/Union — not yet modelled
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
	case "Any":
		// The typechecker treats the lowercase "any" as boxing-permissive (any
		// value assignable); capital "Any" maps to a strict interface{}.
		return "any"
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
	return p.condExpr(p.truthyWrap(cond), e, elseE)
}

// truthyWrap wraps a non-bool expression in zincpyTruthy so it can be used in
// a boolean context (Go requires a bool; Python accepts any value).
func (p *Parser) truthyWrap(e parser.Expr) parser.Expr {
	if p.typeOf(e) == tBool {
		return e
	}
	return callIdent("zincpyTruthy", e)
}

// condExpr builds a value-returning conditional `then if cond else els` (cond
// already truthy-wrapped). When both branches share a known type it uses a
// Zinc IfExpr (codegen infers that concrete Go type); when they differ it
// emits an explicit interface{} IIFE so the heterogeneous branches box safely.
// Backs both the ternary and value-returning and/or.
func (p *Parser) condExpr(cond, then, els parser.Expr) parser.Expr {
	if tt := p.typeOf(then); tt != tUnknown && tt == p.typeOf(els) {
		return &parser.IfExpr{Cond: cond, Then: then, Else: els}
	}
	return &parser.CallExpr{Callee: &parser.LambdaExpr{
		ReturnType: &parser.SimpleType{Name: "interface{}"},
		Body: &parser.BlockStmt{Stmts: []parser.Stmt{
			&parser.IfStmt{Cond: cond, Then: &parser.BlockStmt{Stmts: []parser.Stmt{&parser.ReturnStmt{Value: then}}}},
			&parser.ReturnStmt{Value: els},
		}},
	}}
}

func (p *Parser) parseOr() parser.Expr {
	left := p.parseAnd()
	for p.isKw("or") {
		p.advance()
		right := p.parseAnd()
		if p.typeOf(left) == tBool && p.typeOf(right) == tBool {
			left = &parser.BinaryExpr{Left: left, Op: "||", Right: right}
		} else {
			// Python `a or b` returns the operand value: a if truthy(a) else b.
			left = p.condExpr(p.truthyWrap(left), left, right)
		}
	}
	return left
}

func (p *Parser) parseAnd() parser.Expr {
	left := p.parseNot()
	for p.isKw("and") {
		p.advance()
		right := p.parseNot()
		if p.typeOf(left) == tBool && p.typeOf(right) == tBool {
			left = &parser.BinaryExpr{Left: left, Op: "&&", Right: right}
		} else {
			// Python `a and b` returns the operand value: b if truthy(a) else a.
			left = p.condExpr(p.truthyWrap(left), right, left)
		}
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
		operand := p.parseUnary()
		if p.typeOf(operand) == tDynamic {
			// Unary on a dynamic value: `-x` → zincpyNeg(x); `+x` is identity.
			if op == "-" {
				return callIdent("zincpyNeg", operand)
			}
			return operand
		}
		return &parser.UnaryExpr{Op: op, Operand: operand}
	}
	return p.parsePower()
}

// parsePower handles `base ** exp`. Python's `**` binds tighter than a leading
// unary minus (so `-2**2 == -4`) and is right-associative (`2**3**2 ==
// 2**(3**2)`); the exponent is therefore parsed as a full unary expression,
// which recurses back here. The result reproduces Python's typing (int when
// both operands are non-negative ints, float otherwise) via zincpyPow, so it is
// dynamic — fine for the usual `x**2` / `2**n` uses.
func (p *Parser) parsePower() parser.Expr {
	base := p.parsePostfix()
	if p.isOp("**") {
		p.advance()
		exp := p.parseUnary()
		return callIdent("zincpyPow", base, exp)
	}
	return base
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
			} else if isNonNegIntLit(idx) {
				// xs[0], xs[2] — definitely in range form → direct (fast path).
				e = &parser.IndexExpr{Object: e, Index: idx}
			} else {
				// xs[i] / xs[i-1] / xs[-k] on a native sequence: wrap the index
				// through zincpyIdx so a negative value counts from the end
				// (Go would panic). Stays typed: xs[int] → element type.
				e = &parser.IndexExpr{Object: e,
					Index: callIdent("zincpyIdx", idx, callIdent("len", e))}
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
	outStart := p.pos
	first := p.parseExpr()
	if p.isKw("for") {
		return p.parseListComprehension(outStart, "]")
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

// isSuperCall reports whether e is a bare `super()` call (no arguments), the
// receiver of a `super().method(...)` chain.
func isSuperCall(e parser.Expr) bool {
	call, ok := e.(*parser.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	id, ok := call.Callee.(*parser.Ident)
	return ok && id.Name == "super"
}

// isinstanceTypeNames extracts the type-name operands of an isinstance second
// argument: a bare type name (`int`, `Dog`) or a tuple of them (`(A, B)`),
// which parseAtom may have lowered to a zincpyNewTuple call or kept as a
// TupleLit. Returns nil if the argument is not a name / tuple-of-names.
func (p *Parser) isinstanceTypeNames(e parser.Expr) []string {
	identNames := func(es []parser.Expr) []string {
		var names []string
		for _, el := range es {
			id, ok := el.(*parser.Ident)
			if !ok {
				return nil
			}
			names = append(names, p.isinstanceName(id.Name))
		}
		return names
	}
	switch x := e.(type) {
	case *parser.Ident:
		return []string{p.isinstanceName(x.Name)}
	case *parser.TupleLit:
		return identNames(x.Elements)
	case *parser.CallExpr:
		if cid, ok := x.Callee.(*parser.Ident); ok && cid.Name == "zincpyNewTuple" {
			return identNames(x.Args)
		}
	}
	return nil
}

// isinstanceName maps a Python type name to the string the runtime check
// expects: a user class is capitalized to match its emitted Go struct name;
// builtin type names (int, str, list, ...) are matched verbatim by the helper.
func (p *Parser) isinstanceName(name string) string {
	if p.classNames[name] {
		return strings.ToUpper(name[:1]) + name[1:]
	}
	return name
}

func (p *Parser) parseCall(callee parser.Expr) parser.Expr {
	p.expectOp("(")
	call := &parser.CallExpr{Callee: callee}
	genexprClosed := false
	for !p.isOp(")") && p.cur().Kind != TEOF {
		// keyword argument `name = value` (but not `name == value`).
		if p.cur().Kind == TName && p.peekOp("=") {
			name := p.advance().Value
			p.advance() // '='
			call.NamedArgs = append(call.NamedArgs, parser.NamedArg{Name: name, Value: p.parseExpr()})
		} else {
			outStart := p.pos
			arg := p.parseExpr()
			// A bare generator expression `f(expr for x in it)` — Python allows
			// it only as the sole argument. Materialize it as a list
			// comprehension (byte-identical for the usual consumers: join, sum,
			// any, all, max, min, sorted). parseListComprehension consumes the
			// closing ')'.
			if p.isKw("for") {
				if len(call.Args) != 0 || len(call.NamedArgs) != 0 {
					p.errf(p.cur(), "generator expression must be parenthesized when not the sole argument")
				}
				call.Args = append(call.Args, p.parseListComprehension(outStart, ")"))
				genexprClosed = true
				break
			}
			call.Args = append(call.Args, arg)
		}
		if !p.acceptOp(",") {
			break
		}
	}
	if !genexprClosed {
		p.expectOp(")")
	}
	// Empty list args to a known callable take the parameter's element type.
	if id, ok := callee.(*parser.Ident); ok {
		if params, ok := p.defParams[id.Name]; ok {
			for i, a := range call.Args {
				if i < len(params) {
					coerceEmptyList(a, params[i].Type)
				}
			}
		}
	}
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
	// isinstance(x, T) / isinstance(x, (A, B)) → zincpyIsInstance(x, "T", ...),
	// a runtime reflection check (builtin types by Go representation, user
	// classes by walking the embedded-struct chain). The type operands are
	// emitted as name strings, not expressions (a bare `int`/`Dog` would be an
	// undefined Go identifier).
	if id, ok := callee.(*parser.Ident); ok && id.Name == "isinstance" &&
		len(call.Args) == 2 && len(call.NamedArgs) == 0 {
		names := p.isinstanceTypeNames(call.Args[1])
		if names == nil {
			p.errf(p.cur(), "isinstance: second argument must be a type name or a tuple of type names")
			return call
		}
		args := []parser.Expr{call.Args[0]}
		for _, n := range names {
			args = append(args, &parser.StringLit{Value: n})
		}
		return callIdent("zincpyIsInstance", args...)
	}
	// sorted(xs, key=f) takes a keyword arg, so it's handled before the
	// no-kwargs builtins switch below.
	if id, ok := callee.(*parser.Ident); ok && id.Name == "sorted" && len(call.Args) == 1 {
		if key, ok := namedArg(call, "key"); ok {
			return callIdent("zincpySortedKey", call.Args[0], key)
		}
	}
	// enumerate(xs, start=N): the keyword form, handled here because the
	// builtins switch below skips any call carrying named args.
	if id, ok := callee.(*parser.Ident); ok && id.Name == "enumerate" && len(call.Args) == 1 {
		if start, ok := namedArg(call, "start"); ok {
			return callIdent("zincpyEnumerate", call.Args[0], start)
		}
	}
	// Sequence builtins min/max/sum/sorted/abs → reflection-based runtime
	// helpers (result is dynamic). min/max also take the varargs form
	// min(a, b, ...) → wrap the args in a list.
	if id, ok := callee.(*parser.Ident); ok && len(call.NamedArgs) == 0 {
		switch id.Name {
		case "list":
			if len(call.Args) == 0 {
				return &parser.ListLit{}
			}
			return callIdent("zincpyList", call.Args[0])
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
		case "round":
			if len(call.Args) == 1 {
				return callIdent("zincpyRound", call.Args[0])
			}
			if len(call.Args) == 2 {
				return callIdent("zincpyRoundN", call.Args[0], call.Args[1])
			}
		case "any":
			if len(call.Args) == 1 {
				return callIdent("zincpyAny", call.Args[0])
			}
		case "all":
			if len(call.Args) == 1 {
				return callIdent("zincpyAll", call.Args[0])
			}
		case "sorted":
			if len(call.Args) == 1 {
				if key, ok := namedArg(call, "key"); ok {
					return callIdent("zincpySortedKey", call.Args[0], key)
				}
				return callIdent("zincpySorted", call.Args[0])
			}
		case "map":
			if len(call.Args) == 2 {
				return callIdent("zincpyMap", call.Args[0], call.Args[1])
			}
		case "filter":
			if len(call.Args) == 2 {
				return callIdent("zincpyFilter", call.Args[0], call.Args[1])
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
			// enumerate(xs) / enumerate(xs, start). The start=N keyword form is
			// handled before this no-kwargs switch.
			if len(call.Args) == 1 {
				return callIdent("zincpyEnumerate", call.Args[0])
			}
			if len(call.Args) == 2 {
				return callIdent("zincpyEnumerate", call.Args[0], call.Args[1])
			}
		case "zip":
			if len(call.Args) >= 1 {
				return callIdent("zincpyZip", call.Args...)
			}
		}
	}
	// str(x) → zincpyStr for any argument, so floats/bools/lists format the
	// Python way (str(2.0) == "2.0", not Go's fmt.Sprint "2"). int()/float()
	// keep Go conversions for typed args; only dynamic values need coercion.
	if id, ok := callee.(*parser.Ident); ok && id.Name == "str" &&
		len(call.Args) == 1 && len(call.NamedArgs) == 0 {
		return callIdent("zincpyStr", call.Args[0])
	}
	// int(str)/float(str) on a statically-string argument → parsing helpers that
	// raise ValueError on a bad literal (Go's int(string) would emit a two-value
	// strconv.Atoi in a single-value context).
	if id, ok := callee.(*parser.Ident); ok && len(call.Args) == 1 && len(call.NamedArgs) == 0 && p.typeOf(call.Args[0]) == tStr {
		switch id.Name {
		case "int":
			return callIdent("zincpyParseInt", call.Args[0])
		case "float":
			return callIdent("zincpyParseFloat", call.Args[0])
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
			case "get":
				// d.get(k) → d.Get(k) (None on miss); d.get(k, default) →
				// d.GetDefault(k, default).
				if len(call.Args) == 2 {
					sel.Field = "GetDefault"
				} else {
					sel.Field = "Get"
				}
			case "setdefault":
				sel.Field = "SetDefault"
			case "pop":
				sel.Field = "Pop"
			case "update":
				sel.Field = "Update"
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
	// super().method(args) inside a method body → call the embedded parent's
	// method directly: this.<Parent>.Method(args). (super().__init__ is handled
	// separately by extractSuperInit and removed from the constructor body.)
	if sel, ok := callee.(*parser.SelectorExpr); ok && isSuperCall(sel.Object) &&
		sel.Field != "__init__" { // __init__ is hoisted by extractSuperInit
		parent := p.classParent[p.currentClass]
		if parent == "" {
			p.errf(p.cur(), "super() used in %q which has no base class", p.currentClass)
			return call
		}
		field := sel.Field
		if g, ok := dunderMethods[field]; ok {
			field = g
		}
		return &parser.CallExpr{
			Callee: &parser.SelectorExpr{
				Object: &parser.SelectorExpr{Object: &parser.ThisExpr{}, Field: parent},
				Field:  field,
			},
			Args: call.Args,
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

// parseLambda parses `lambda a, b: expr` into a Zinc single-expression
// LambdaExpr. Parameters are dynamic (any), so the body's operations route
// through the dynamic helpers; the function value is `func(...any) any`.
func (p *Parser) parseLambda() parser.Expr {
	p.advance() // 'lambda'
	var params []*parser.ParamDecl
	for p.cur().Kind == TName {
		params = append(params, &parser.ParamDecl{Name: goSafe(p.advance().Value)})
		if !p.acceptOp(",") {
			break
		}
	}
	p.expectOp(":")
	p.pushScope()
	for _, pa := range params {
		p.declare(pa.Name, tDynamic)
	}
	body := p.parseExpr()
	p.popScope()
	return &parser.LambdaExpr{
		Params:     params,
		ReturnType: &parser.SimpleType{Name: "interface{}"},
		Expr:       body,
	}
}

func (p *Parser) parseAtom() parser.Expr {
	if p.isKw("lambda") {
		return p.parseLambda()
	}
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
		return &parser.Ident{Name: goSafe(t.Value)}
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
