package codegen_go

import (
	"fmt"
	"strings"

	"zinc-go/internal/parser"
)

// Generator produces Go source from a Zinc AST.
type Generator struct {
	buf           strings.Builder
	indent        int
	className     string // derived from filename or "main"
	imports       map[string]bool
	interfaces    map[string]bool
	structs       map[string]*parser.ClassDecl
	sourceFile    string // for //line directives
	currentFields  map[string]bool // field names of current class (for implicit self)
	currentMethods map[string]bool // method names of current class (for implicit self)
	currentParams  map[string]bool // parameter names (shadow field names)
}

// New creates a new Go code generator.
func New() *Generator {
	return &Generator{
		imports:    make(map[string]bool),
		interfaces: make(map[string]bool),
		structs:    make(map[string]*parser.ClassDecl),
	}
}

// OutputFile represents a generated .go file.
type OutputFile struct {
	Name    string
	Content string
}

// SetSourceFile sets the source .zn filename for //line directives.
func (g *Generator) SetSourceFile(path string) {
	g.sourceFile = path
}

// RegisterInterface allows external callers to register interface names.
func (g *Generator) RegisterInterface(name string) {
	g.interfaces[name] = true
}

// collectDecls scans declarations to build lookup tables.
func (g *Generator) collectDecls(decls []parser.TopLevelDecl) {
	for _, d := range decls {
		switch decl := d.(type) {
		case *parser.InterfaceDecl:
			g.interfaces[decl.Name] = true
		case *parser.ClassDecl:
			g.structs[decl.Name] = decl
		}
	}
}

// needImport records that a Go import is required.
func (g *Generator) needImport(pkg string) {
	g.imports[pkg] = true
}

// Generate produces a single .go source file from a Zinc program.
func (g *Generator) Generate(prog *parser.Program, className string) string {
	g.buf.Reset()
	g.indent = 0
	g.className = className
	g.imports = make(map[string]bool)
	g.collectDecls(prog.Decls)

	// First pass: generate body into a separate buffer to collect imports
	bodyGen := *g
	bodyGen.buf.Reset()

	for _, d := range prog.Decls {
		bodyGen.emitDecl(d)
		bodyGen.writeln("")
	}

	// Script-mode statements → func main()
	hasExplicitMain := false
	for _, d := range prog.Decls {
		if fn, ok := d.(*parser.FnDecl); ok && fn.Name == "main" {
			hasExplicitMain = true
			break
		}
	}
	if len(prog.Stmts) > 0 && !hasExplicitMain {
		bodyGen.writeln("func main() {")
		bodyGen.indent++
		for _, s := range prog.Stmts {
			bodyGen.emitStmt(s)
		}
		bodyGen.indent--
		bodyGen.writeln("}")
	}

	body := bodyGen.buf.String()
	g.imports = bodyGen.imports

	// Now write the final output with package + imports + body
	g.writeln("package main")
	g.writeln("")

	if len(g.imports) > 0 {
		g.writeln("import (")
		g.indent++
		for pkg := range g.imports {
			g.writeln("%q", pkg)
		}
		g.indent--
		g.writeln(")")
		g.writeln("")
	}

	g.buf.WriteString(body)
	return g.buf.String()
}

// GenerateFiles produces separate .go files per type + a main.go for functions/script.
func (g *Generator) GenerateFiles(prog *parser.Program, className string) []OutputFile {
	// For now, generate everything into a single file
	content := g.Generate(prog, className)
	return []OutputFile{{Name: strings.ToLower(className) + ".go", Content: content}}
}

// --- Declarations ------------------------------------------------------------

func (g *Generator) emitDecl(d parser.TopLevelDecl) {
	switch decl := d.(type) {
	case *parser.FnDecl:
		g.emitFnDecl(decl)
	case *parser.ClassDecl:
		if decl.IsSealed {
			g.emitSealedDecl(decl)
		} else {
			g.emitClassDecl(decl)
		}
	case *parser.DataClassDecl:
		g.emitDataClassDecl(decl)
	case *parser.EnumDecl:
		g.emitEnumDecl(decl)
	case *parser.InterfaceDecl:
		g.emitInterfaceDecl(decl)
	case *parser.ConstDecl:
		g.emitConstDecl(decl)
	}
}

// --- Functions ---------------------------------------------------------------

func (g *Generator) emitFnDecl(fn *parser.FnDecl) {
	if g.sourceFile != "" && fn.Line > 0 {
		g.writeln("//line %s:%d", g.sourceFile, fn.Line)
	}

	name := fn.Name
	if name == "main" {
		g.writeln("func main() {")
		g.indent++
		g.emitBlock(fn.Body)
		g.indent--
		g.writeln("}")
		return
	}

	ret := g.formatReturnType(fn.ReturnType, fn.Body)
	params := g.formatParams(fn.Params)

	g.writeln("func %s(%s)%s {", name, params, ret)
	g.indent++
	g.emitBlock(fn.Body)
	g.indent--
	g.writeln("}")
}

// --- Structs (Classes) -------------------------------------------------------

func (g *Generator) emitClassDecl(cls *parser.ClassDecl) {
	if g.sourceFile != "" && cls.Line > 0 {
		g.writeln("//line %s:%d", g.sourceFile, cls.Line)
	}

	name := cls.Name

	// Struct definition
	g.writeln("type %s struct {", name)
	g.indent++

	// Embedded parent (first non-interface parent)
	for _, p := range cls.Parents {
		if !g.interfaces[p] {
			g.writeln("%s", p)
		}
	}

	for _, f := range cls.Fields {
		typeName := "interface{}"
		if f.Type != nil {
			typeName = g.formatType(f.Type)
		}
		g.writeln("%s %s", exportName(f.Name), typeName)
	}
	g.indent--
	g.writeln("}")
	g.writeln("")

	// Constructor → NewType() function
	if cls.Ctor != nil {
		g.emitConstructor(name, cls.Ctor, cls)
	} else if len(cls.Ctors) > 0 {
		g.emitConstructor(name, cls.Ctors[0], cls)
	}

	// Methods
	for _, m := range cls.Methods {
		g.emitMethodDecl(name, m)
		g.writeln("")
	}
}

func (g *Generator) emitConstructor(typeName string, ctor *parser.CtorDecl, cls *parser.ClassDecl) {
	// Set current fields/methods for implicit self resolution
	g.currentFields = make(map[string]bool)
	g.currentMethods = make(map[string]bool)
	g.currentParams = make(map[string]bool)
	for _, f := range cls.Fields {
		g.currentFields[f.Name] = true
	}
	for _, method := range cls.Methods {
		g.currentMethods[method.Name] = true
	}
	for _, p := range ctor.Params {
		g.currentParams[p.Name] = true
	}
	defer func() { g.currentFields = nil; g.currentMethods = nil; g.currentParams = nil }()

	params := g.formatParams(ctor.Params)
	g.writeln("func New%s(%s) *%s {", typeName, params, typeName)
	g.indent++
	g.writeln("s := &%s{}", typeName)

	if ctor.Body != nil {
		for _, stmt := range ctor.Body.Stmts {
			g.emitCtorStmt(stmt)
		}
	}

	g.writeln("return s")
	g.indent--
	g.writeln("}")
	g.writeln("")
}

func (g *Generator) emitCtorStmt(s parser.Stmt) {
	// Just emit normally — ThisExpr already maps to "s" in formatExpr
	g.emitStmt(s)
}

// formatCtorExpr formats an expression in constructor context,
// replacing this.field with s.Field and bare field references with s.Field.
func (g *Generator) formatCtorExpr(e parser.Expr) string {
	switch expr := e.(type) {
	case *parser.SelectorExpr:
		if _, ok := expr.Object.(*parser.ThisExpr); ok {
			return "s." + exportName(expr.Field)
		}
		return g.formatExpr(e)
	case *parser.Ident:
		return expr.Name
	default:
		return g.formatExpr(e)
	}
}

func (g *Generator) emitMethodDecl(receiver string, m *parser.MethodDecl) {
	// Set current fields/methods for implicit self resolution
	if cls, ok := g.structs[receiver]; ok {
		g.currentFields = make(map[string]bool)
		g.currentMethods = make(map[string]bool)
		g.currentParams = make(map[string]bool)
		for _, f := range cls.Fields {
			g.currentFields[f.Name] = true
		}
		for _, method := range cls.Methods {
			g.currentMethods[method.Name] = true
		}
		for _, p := range m.Params {
			g.currentParams[p.Name] = true
		}
	}
	defer func() { g.currentFields = nil; g.currentMethods = nil; g.currentParams = nil }()

	if m.IsStatic {
		// Static methods → package-level functions prefixed with type name
		name := receiver + exportName(m.Name)
		ret := g.formatReturnType(m.ReturnType, m.Body)
		params := g.formatParams(m.Params)
		g.writeln("func %s(%s)%s {", name, params, ret)
	} else {
		vis := strings.ToLower(m.Name[:1]) + m.Name[1:]
		if m.IsPub {
			vis = exportName(m.Name)
		}
		ret := g.formatReturnType(m.ReturnType, m.Body)
		params := g.formatParams(m.Params)
		g.writeln("func (s *%s) %s(%s)%s {", receiver, vis, params, ret)
	}
	g.indent++
	g.emitBlock(m.Body)
	g.indent--
	g.writeln("}")
}

// --- Data Classes (Structs) --------------------------------------------------

func (g *Generator) emitDataClassDecl(d *parser.DataClassDecl) {
	if g.sourceFile != "" && d.Line > 0 {
		g.writeln("//line %s:%d", g.sourceFile, d.Line)
	}

	g.writeln("type %s struct {", d.Name)
	g.indent++
	for _, f := range d.Params {
		typeName := "interface{}"
		if f.Type != nil {
			typeName = g.formatType(f.Type)
		}
		g.writeln("%s %s", exportName(f.Name), typeName)
	}
	g.indent--
	g.writeln("}")
	g.writeln("")

	// Constructor
	var params []string
	var assignments []string
	for _, f := range d.Params {
		typeName := "interface{}"
		if f.Type != nil {
			typeName = g.formatType(f.Type)
		}
		params = append(params, fmt.Sprintf("%s %s", f.Name, typeName))
		assignments = append(assignments, fmt.Sprintf("%s: %s", exportName(f.Name), f.Name))
	}
	g.writeln("func New%s(%s) %s {", d.Name, strings.Join(params, ", "), d.Name)
	g.indent++
	g.writeln("return %s{%s}", d.Name, strings.Join(assignments, ", "))
	g.indent--
	g.writeln("}")
	g.writeln("")

	// String() method for data classes
	g.needImport("fmt")
	var fmtParts []string
	var fmtArgs []string
	for _, f := range d.Params {
		fmtParts = append(fmtParts, f.Name+"=%v")
		fmtArgs = append(fmtArgs, "s."+exportName(f.Name))
	}
	g.writeln("func (s %s) String() string {", d.Name)
	g.indent++
	g.writeln("return fmt.Sprintf(\"%s(%s)\", %s)", d.Name, strings.Join(fmtParts, ", "), strings.Join(fmtArgs, ", "))
	g.indent--
	g.writeln("}")

	// Methods
	for _, m := range d.Methods {
		g.writeln("")
		g.emitMethodDecl(d.Name, m)
	}
}

// --- Sealed types (interface + variant structs) ------------------------------

func (g *Generator) emitSealedDecl(cls *parser.ClassDecl) {
	// Sealed class → interface with private marker method
	g.writeln("type %s interface {", cls.Name)
	g.indent++
	g.writeln("is%s()", cls.Name)
	g.indent--
	g.writeln("}")
	g.writeln("")

	for _, v := range cls.Variants {
		g.emitDataClassDecl(v)
		g.writeln("")
		// Implement the sealed marker
		g.writeln("func (%s) is%s() {}", v.Name, cls.Name)
		g.writeln("")
	}
}

// --- Enums -------------------------------------------------------------------

func (g *Generator) emitEnumDecl(e *parser.EnumDecl) {
	g.writeln("type %s int", e.Name)
	g.writeln("")
	g.writeln("const (")
	g.indent++
	for i, v := range e.Variants {
		if i == 0 {
			g.writeln("%s %s = iota", v, e.Name)
		} else {
			g.writeln("%s", v)
		}
	}
	g.indent--
	g.writeln(")")
}

// --- Interfaces --------------------------------------------------------------

func (g *Generator) emitInterfaceDecl(iface *parser.InterfaceDecl) {
	g.writeln("type %s interface {", iface.Name)
	g.indent++
	for _, m := range iface.Methods {
		ret := ""
		if m.ReturnType != nil {
			ret = " " + g.formatType(m.ReturnType)
		}
		params := g.formatParams(m.Params)
		g.writeln("%s(%s)%s", exportName(m.Name), params, ret)
	}
	g.indent--
	g.writeln("}")
}

// --- Constants ---------------------------------------------------------------

func (g *Generator) emitConstDecl(c *parser.ConstDecl) {
	g.writeln("const %s = %s", exportName(c.Name), g.formatExpr(c.Value))
}

// --- Statements --------------------------------------------------------------

func (g *Generator) emitStmt(s parser.Stmt) {
	switch stmt := s.(type) {
	case *parser.VarStmt:
		g.emitVarStmt(stmt)
	case *parser.AssignStmt:
		g.emitAssignStmt(stmt)
	case *parser.ReturnStmt:
		g.emitReturnStmt(stmt)
	case *parser.IfStmt:
		g.emitIfStmt(stmt)
	case *parser.ForStmt:
		g.emitForStmt(stmt)
	case *parser.WhileStmt:
		g.writeln("for %s {", g.formatExpr(stmt.Cond))
		g.indent++
		g.emitBlock(stmt.Body)
		g.indent--
		g.writeln("}")
	case *parser.MatchStmt:
		g.emitMatchStmt(stmt)
	case *parser.ExprStmt:
		g.emitExprStmt(stmt)
	case *parser.PrintStmt:
		g.needImport("fmt")
		g.writeln("fmt.Println(%s)", g.formatExpr(stmt.Value))
	case *parser.BreakStmt:
		g.writeln("break")
	case *parser.ContinueStmt:
		g.writeln("continue")
	case *parser.BlockStmt:
		g.emitBlock(stmt)
	case *parser.FnDecl:
		g.emitFnDecl(stmt)
	case *parser.TupleVarStmt:
		g.emitTupleVarStmt(stmt)
	case *parser.GoStmt:
		g.writeln("go func() {")
		g.indent++
		g.emitBlock(stmt.Body)
		g.indent--
		g.writeln("}()")
	case *parser.ParallelForStmt:
		g.emitParallelForStmt(stmt)
	case *parser.ConcurrentStmt:
		g.emitConcurrentStmt(stmt)
	case *parser.WithStmt:
		g.emitWithStmt(stmt)
	case *parser.DeferStmt:
		g.writeln("defer %s", g.formatExpr(stmt.Expr))
	case *parser.AssertStmt:
		g.emitAssertStmt(stmt)
	case *parser.TryStmt:
		// Go doesn't have try/catch — emit as a comment for now
		g.writeln("// try/catch not directly supported in Go")
		g.emitBlock(stmt.Body)
	case *parser.RaiseStmt:
		g.writeln("panic(%s)", g.formatExpr(stmt.Value))
	}
}

func (g *Generator) emitVarStmt(v *parser.VarStmt) {
	if v.OrHandler != nil && v.Value != nil {
		// var x = call() or default → x, err := call(); if err != nil { ... }
		g.emitOrAssignment(v.Name, v.Value, v.OrHandler)
		return
	}

	if v.Value != nil {
		// Typed array/slice: int[] nums = [1, 2, 3] → nums := []int{1, 2, 3}
		if arrType, ok := v.Type.(*parser.ArrayType); ok {
			if listLit, ok := v.Value.(*parser.ListLit); ok {
				elemType := g.formatType(arrType.ElementType)
				elems := g.formatExprList(listLit.Elements)
				g.writeln("%s := []%s{%s}", v.Name, elemType, elems)
				return
			}
		}
		// Typed generic: List<int> nums = [...] → nums := []int{...}
		if genType, ok := v.Type.(*parser.GenericType); ok {
			if listLit, ok := v.Value.(*parser.ListLit); ok {
				goType := g.formatType(genType)
				elems := g.formatExprList(listLit.Elements)
				g.writeln("%s := %s{%s}", v.Name, goType, elems)
				return
			}
			if mapLit, ok := v.Value.(*parser.MapLit); ok {
				goType := g.formatType(genType)
				var pairs []string
				for i := range mapLit.Keys {
					pairs = append(pairs, fmt.Sprintf("%s: %s", g.formatExpr(mapLit.Keys[i]), g.formatExpr(mapLit.Values[i])))
				}
				g.writeln("%s := %s{%s}", v.Name, goType, strings.Join(pairs, ", "))
				return
			}
		}
		g.writeln("%s := %s", v.Name, g.formatExpr(v.Value))
	} else {
		typeName := "interface{}"
		if v.Type != nil {
			typeName = g.formatType(v.Type)
		}
		g.writeln("var %s %s", v.Name, typeName)
	}
}

func (g *Generator) emitAssignStmt(a *parser.AssignStmt) {
	if a.OrHandler != nil {
		// target = call() or default
		targetStr := g.formatExpr(a.Target)
		g.emitOrAssignment(targetStr, a.Value, a.OrHandler)
		return
	}
	g.writeln("%s %s %s", g.formatExpr(a.Target), a.Op, g.formatExpr(a.Value))
}

func (g *Generator) emitReturnStmt(r *parser.ReturnStmt) {
	if r.Value == nil {
		g.writeln("return")
		return
	}

	// return Error(...) → return zero, fmt.Errorf(...)
	if call, ok := r.Value.(*parser.CallExpr); ok {
		if ident, ok := call.Callee.(*parser.Ident); ok && ident.Name == "Error" {
			g.needImport("fmt")
			if len(call.Args) == 1 {
				arg := call.Args[0]
				// return Error(CustomType(...)) → return zero, CustomType(...)
				if innerCall, ok := arg.(*parser.CallExpr); ok {
					if innerIdent, ok := innerCall.Callee.(*parser.Ident); ok {
						args := g.formatExprList(innerCall.Args)
						g.writeln("return *new(T), fmt.Errorf(\"%%v\", New%s(%s))", innerIdent.Name, args)
						return
					}
				}
				// return Error(err) → return zero, err
				if ident, ok := arg.(*parser.Ident); ok && ident.Name == "err" {
					g.writeln("return *new(T), err")
					return
				}
				// return Error("message") → return zero, fmt.Errorf("message")
				g.writeln("return *new(T), fmt.Errorf(%s)", g.formatExpr(arg))
				return
			}
		}
	}

	g.writeln("return %s", g.formatExpr(r.Value))
}

func (g *Generator) emitIfStmt(s *parser.IfStmt) {
	g.writeln("if %s {", g.formatExpr(s.Cond))
	g.indent++
	g.emitBlock(s.Then)
	g.indent--
	if s.ElseStmt != nil {
		switch e := s.ElseStmt.(type) {
		case *parser.IfStmt:
			g.write("} else ")
			g.emitIfStmt(e)
			return
		case *parser.BlockStmt:
			g.writeln("} else {")
			g.indent++
			g.emitBlock(e)
			g.indent--
		}
	}
	g.writeln("}")
}

func (g *Generator) emitForStmt(f *parser.ForStmt) {
	if f.IsRange {
		if rangeExpr, ok := f.Range.(*parser.RangeExpr); ok {
			// Range expression: for i in 0..10 → for i := 0; i < 10; i++
			start := g.formatExpr(rangeExpr.Start)
			end := g.formatExpr(rangeExpr.End)
			op := "<"
			if rangeExpr.Inclusive {
				op = "<="
			}
			g.writeln("for %s := %s; %s %s %s; %s++ {", f.Item, start, f.Item, op, end, f.Item)
		} else if f.IndexVar != "" {
			// for key, value in map → for key, value := range map
			g.writeln("for %s, %s := range %s {", f.IndexVar, f.Item, g.formatExpr(f.Range))
		} else {
			// for item in list → for _, item := range list
			g.writeln("for _, %s := range %s {", f.Item, g.formatExpr(f.Range))
		}
	} else {
		// C-style for
		init := ""
		if f.Init != nil {
			init = g.formatStmtInline(f.Init)
		}
		cond := ""
		if f.Cond != nil {
			cond = g.formatExpr(f.Cond)
		}
		post := ""
		if f.Post != nil {
			post = g.formatStmtInline(f.Post)
		}
		g.writeln("for %s; %s; %s {", init, cond, post)
	}
	g.indent++
	g.emitBlock(f.Body)
	g.indent--
	g.writeln("}")
}

func (g *Generator) emitMatchStmt(m *parser.MatchStmt) {
	g.writeln("switch %s {", g.formatExpr(m.Subject))
	for _, c := range m.Cases {
		if c.Pattern == nil {
			g.writeln("default:")
		} else {
			g.writeln("case %s:", g.formatExpr(c.Pattern))
		}
		g.indent++
		g.emitBlock(c.Body)
		g.indent--
	}
	g.writeln("}")
}

func (g *Generator) emitExprStmt(es *parser.ExprStmt) {
	if es.OrHandler != nil {
		// expr or { handler } → if _, err := expr; err != nil { handler }
		g.emitOrAssignment("_", es.Expr, es.OrHandler)
		return
	}
	// .add() → x = append(x, elem)
	if call, ok := es.Expr.(*parser.CallExpr); ok {
		if sel, ok := call.Callee.(*parser.SelectorExpr); ok && sel.Field == "add" {
			obj := g.formatExpr(sel.Object)
			args := g.formatExprList(call.Args)
			g.writeln("%s = append(%s, %s)", obj, obj, args)
			return
		}
		// .put() → map[key] = value
		if sel, ok := call.Callee.(*parser.SelectorExpr); ok && sel.Field == "put" && len(call.Args) == 2 {
			obj := g.formatExpr(sel.Object)
			g.writeln("%s[%s] = %s", obj, g.formatExpr(call.Args[0]), g.formatExpr(call.Args[1]))
			return
		}
	}
	g.writeln("%s", g.formatExpr(es.Expr))
}

// emitOrAssignment handles: target = call() or default / or { block }
func (g *Generator) emitOrAssignment(target string, value parser.Expr, handler *parser.OrHandler) {
	callExpr := g.formatExpr(value)

	if handler.Body != nil && len(handler.Body.Stmts) == 1 {
		// Single-statement or handler
		if es, ok := handler.Body.Stmts[0].(*parser.ExprStmt); ok {
			// or default_value
			g.writeln("%s, _err := %s", target, callExpr)
			g.writeln("if _err != nil {")
			g.indent++
			g.writeln("%s = %s", target, g.formatExpr(es.Expr))
			g.indent--
			g.writeln("}")
			return
		}
	}

	g.writeln("%s, _err := %s", target, callExpr)
	g.writeln("if _err != nil {")
	g.indent++
	if handler.Body != nil {
		g.emitBlock(handler.Body)
	}
	g.indent--
	g.writeln("}")
}

func (g *Generator) emitParallelForStmt(p *parser.ParallelForStmt) {
	g.needImport("sync")
	g.writeln("var _wg sync.WaitGroup")
	if p.Max > 0 {
		g.writeln("_sem := make(chan struct{}, %d)", p.Max)
	}
	g.writeln("for _, %s := range %s {", p.Item, g.formatExpr(p.Range))
	g.indent++
	g.writeln("%s := %s // capture", p.Item, p.Item)
	g.writeln("_wg.Add(1)")
	if p.Max > 0 {
		g.writeln("_sem <- struct{}{}")
	}
	g.writeln("go func() {")
	g.indent++
	g.writeln("defer _wg.Done()")
	if p.Max > 0 {
		g.writeln("defer func() { <-_sem }()")
	}
	g.emitBlock(p.Body)
	g.indent--
	g.writeln("}()")
	g.indent--
	g.writeln("}")
	g.writeln("_wg.Wait()")
}

func (g *Generator) emitConcurrentStmt(c *parser.ConcurrentStmt) {
	g.needImport("sync")
	g.writeln("var _wg sync.WaitGroup")
	for _, task := range c.Tasks {
		g.writeln("_wg.Add(1)")
		g.writeln("go func() {")
		g.indent++
		g.writeln("defer _wg.Done()")
		g.writeln("%s", g.formatExpr(task))
		g.indent--
		g.writeln("}()")
	}
	g.writeln("_wg.Wait()")
}

func (g *Generator) emitWithStmt(w *parser.WithStmt) {
	if len(w.Resources) == 1 && w.Resources[0].Name == "_lock" {
		// lock mu { body } → mu.Lock(); defer mu.Unlock(); body
		lockExpr := g.formatExpr(w.Resources[0].Value)
		g.writeln("%s.Lock()", lockExpr)
		g.writeln("defer %s.Unlock()", lockExpr)
		g.emitBlock(w.Body)
		return
	}
	// General with → open + defer close
	for _, r := range w.Resources {
		g.writeln("%s := %s", r.Name, g.formatExpr(r.Value))
		g.writeln("defer %s.Close()", r.Name)
	}
	g.emitBlock(w.Body)
}

func (g *Generator) emitTupleVarStmt(t *parser.TupleVarStmt) {
	names := strings.Join(t.Names, ", ")
	g.writeln("%s := %s", names, g.formatExpr(t.Value))
}

func (g *Generator) emitAssertStmt(a *parser.AssertStmt) {
	if a.Message != nil {
		g.writeln("if !(%s) { panic(%s) }", g.formatExpr(a.Cond), g.formatExpr(a.Message))
	} else {
		g.writeln("if !(%s) { panic(\"assertion failed\") }", g.formatExpr(a.Cond))
	}
}

func (g *Generator) emitBlock(block *parser.BlockStmt) {
	if block == nil {
		return
	}
	for _, s := range block.Stmts {
		g.emitStmt(s)
	}
}

// --- Expressions -------------------------------------------------------------

func (g *Generator) formatExpr(e parser.Expr) string {
	switch expr := e.(type) {
	case *parser.Ident:
		if expr.Name == "this" {
			return "s"
		}
		// Implicit self: bare field name → s.Field in method/ctor context
		// But not if it's a parameter name (params shadow fields)
		if g.currentFields != nil && g.currentFields[expr.Name] && !g.currentParams[expr.Name] {
			return "s." + exportName(expr.Name)
		}
		return expr.Name
	case *parser.IntLit:
		return expr.Value
	case *parser.FloatLit:
		return expr.Value
	case *parser.StringLit:
		if strings.Contains(expr.Value, "\n") {
			return fmt.Sprintf("`%s`", expr.Value)
		}
		return fmt.Sprintf("%q", expr.Value)
	case *parser.StringInterpLit:
		return g.formatStringInterp(expr)
	case *parser.BoolLit:
		if expr.Value {
			return "true"
		}
		return "false"
	case *parser.NullLit:
		return "nil"
	case *parser.BinaryExpr:
		return g.formatBinaryExpr(expr)
	case *parser.UnaryExpr:
		return fmt.Sprintf("%s%s", expr.Op, g.formatExpr(expr.Operand))
	case *parser.CallExpr:
		return g.formatCallExpr(expr)
	case *parser.SelectorExpr:
		// .length → len()
		if expr.Field == "length" {
			return fmt.Sprintf("len(%s)", g.formatExpr(expr.Object))
		}
		return fmt.Sprintf("%s.%s", g.formatExpr(expr.Object), exportName(expr.Field))
	case *parser.IndexExpr:
		return fmt.Sprintf("%s[%s]", g.formatExpr(expr.Object), g.formatExpr(expr.Index))
	case *parser.SliceExpr:
		low := ""
		high := ""
		if expr.Low != nil {
			low = g.formatExpr(expr.Low)
		}
		if expr.High != nil {
			high = g.formatExpr(expr.High)
		}
		return fmt.Sprintf("%s[%s:%s]", g.formatExpr(expr.Object), low, high)
	case *parser.ListLit:
		if len(expr.Elements) == 0 {
			return "[]interface{}{}"
		}
		elems := g.formatExprList(expr.Elements)
		return fmt.Sprintf("[]interface{}{%s}", elems)
	case *parser.MapLit:
		if len(expr.Keys) == 0 {
			return "map[string]interface{}{}"
		}
		var pairs []string
		for i := range expr.Keys {
			pairs = append(pairs, fmt.Sprintf("%s: %s", g.formatExpr(expr.Keys[i]), g.formatExpr(expr.Values[i])))
		}
		return fmt.Sprintf("map[string]interface{}{%s}", strings.Join(pairs, ", "))
	case *parser.LambdaExpr:
		return g.formatLambdaExpr(expr)
	case *parser.ThisExpr:
		return "s"
	case *parser.SuperCallExpr:
		return fmt.Sprintf("/* super(%s) */", g.formatExprList(expr.Args))
	case *parser.TypeAssertExpr:
		if expr.IsCheck {
			return fmt.Sprintf("func() bool { _, ok := %s.(%s); return ok }()", g.formatExpr(expr.Object), expr.TypeName)
		}
		return fmt.Sprintf("%s.(%s)", g.formatExpr(expr.Object), expr.TypeName)
	case *parser.SafeNavExpr:
		obj := g.formatExpr(expr.Object)
		if expr.Call != nil {
			args := g.formatExprList(expr.Call.Args)
			return fmt.Sprintf("func() interface{} { if %s != nil { return %s.%s(%s) }; return nil }()", obj, obj, exportName(expr.Field), args)
		}
		return fmt.Sprintf("func() interface{} { if %s != nil { return %s.%s }; return nil }()", obj, obj, exportName(expr.Field))
	case *parser.TupleLit:
		// Go doesn't have tuples — use a struct or slice
		return fmt.Sprintf("[]interface{}{%s}", g.formatExprList(expr.Elements))
	case *parser.SpawnExpr:
		// spawn { body } → go func() { body }()
		// Returns as expression is tricky — use a channel or future pattern
		g.needImport("sync")
		return "/* spawn: use goroutine */"
	case *parser.IfExpr:
		return fmt.Sprintf("func() interface{} { if %s { return %s }; return %s }()",
			g.formatExpr(expr.Cond), g.formatExpr(expr.Then), g.formatExpr(expr.Else))
	case *parser.MatchExpr:
		return g.formatMatchExpr(expr)
	case *parser.RangeExpr:
		// Range as value — not directly expressible in Go
		return fmt.Sprintf("/* range %s..%s */", g.formatExpr(expr.Start), g.formatExpr(expr.End))
	case *parser.RawStringLit:
		return fmt.Sprintf("`%s`", expr.Value)
	case *parser.SpreadExpr:
		return g.formatExpr(expr.Expr) + "..."
	default:
		return "/* unknown expr */"
	}
}

func (g *Generator) formatBinaryExpr(b *parser.BinaryExpr) string {
	left := g.formatExpr(b.Left)
	right := g.formatExpr(b.Right)

	switch b.Op {
	case "and", "&&":
		return fmt.Sprintf("%s && %s", left, right)
	case "or", "||":
		return fmt.Sprintf("%s || %s", left, right)
	case "not":
		return fmt.Sprintf("!%s", right)
	case "**":
		g.needImport("math")
		return fmt.Sprintf("math.Pow(float64(%s), float64(%s))", left, right)
	case "==":
		return fmt.Sprintf("%s == %s", left, right)
	case "!=":
		return fmt.Sprintf("%s != %s", left, right)
	case "===":
		// Reference identity — same as == in Go for pointers
		return fmt.Sprintf("%s == %s", left, right)
	case "!==":
		return fmt.Sprintf("%s != %s", left, right)
	case "in":
		// x in collection — no direct Go equivalent, use helper
		return fmt.Sprintf("/* %s in %s */", left, right)
	case "not in":
		return fmt.Sprintf("/* %s not in %s */", left, right)
	case "is":
		return fmt.Sprintf("func() bool { _, ok := %s.(%s); return ok }()", left, right)
	case "is not":
		return fmt.Sprintf("func() bool { _, ok := %s.(%s); return !ok }()", left, right)
	default:
		return fmt.Sprintf("%s %s %s", left, b.Op, right)
	}
}

// stringMethodMapping maps Zinc string methods to Go equivalents.
var stringMethodMapping = map[string]string{
	"upper":      "strings.ToUpper",
	"lower":      "strings.ToLower",
	"trim":       "strings.TrimSpace",
	// trimStart/trimEnd need special handling
	"contains":   "strings.Contains",
	"startsWith": "strings.HasPrefix",
	"endsWith":   "strings.HasSuffix",
	// replace needs special handling (4th arg for Go)
	"split":      "strings.Split",
	"repeat":     "strings.Repeat",
	"indexOf":    "strings.Index",
}

func (g *Generator) formatCallExpr(c *parser.CallExpr) string {
	// String method rewrites
	if sel, ok := c.Callee.(*parser.SelectorExpr); ok {
		if goFunc, ok := stringMethodMapping[sel.Field]; ok {
			g.needImport("strings")
			obj := g.formatExpr(sel.Object)
			args := g.formatExprList(c.Args)
			if args != "" {
				return fmt.Sprintf("%s(%s, %s)", goFunc, obj, args)
			}
			return fmt.Sprintf("%s(%s)", goFunc, obj)
		}

		// Collection methods
		obj := g.formatExpr(sel.Object)
		switch sel.Field {
		case "add":
			// append returns a new slice — but in Zinc it mutates, so wrap as assignment
			// This is called as a statement, so we handle it in emitExprStmt too
			args := g.formatExprList(c.Args)
			return fmt.Sprintf("append(%s, %s)", obj, args)
		case "put":
			// map.put(key, value) → map[key] = value
			if len(c.Args) == 2 {
				return fmt.Sprintf("func() { %s[%s] = %s }()", obj, g.formatExpr(c.Args[0]), g.formatExpr(c.Args[1]))
			}
		case "size":
			return fmt.Sprintf("len(%s)", obj)
		case "isEmpty":
			return fmt.Sprintf("len(%s) == 0", obj)
		case "length":
			return fmt.Sprintf("len(%s)", obj)
		case "charAt":
			return fmt.Sprintf("string(%s[%s])", obj, g.formatExprList(c.Args))
		case "substring":
			args := c.Args
			if len(args) == 2 {
				return fmt.Sprintf("%s[%s:%s]", obj, g.formatExpr(args[0]), g.formatExpr(args[1]))
			}
			return fmt.Sprintf("%s[%s:]", obj, g.formatExpr(args[0]))
		case "replace":
			g.needImport("strings")
			if len(c.Args) == 2 {
				return fmt.Sprintf("strings.ReplaceAll(%s, %s, %s)", obj, g.formatExpr(c.Args[0]), g.formatExpr(c.Args[1]))
			}
		case "trimStart":
			g.needImport("strings")
			return fmt.Sprintf("strings.TrimLeft(%s, \" \\t\\n\\r\")", obj)
		case "trimEnd":
			g.needImport("strings")
			return fmt.Sprintf("strings.TrimRight(%s, \" \\t\\n\\r\")", obj)
		case "upper":
			g.needImport("strings")
			return fmt.Sprintf("strings.ToUpper(%s)", obj)
		case "lower":
			g.needImport("strings")
			return fmt.Sprintf("strings.ToLower(%s)", obj)
		case "entrySet":
			// map.entrySet() → just the map (used in for range)
			return obj
		case "getKey":
			// entry.getKey() for map iteration — handled by for-range rewrite
			return obj + ".Key"
		case "getValue":
			return obj + ".Value"
		}
	}

	callee := g.formatExpr(c.Callee)

	// Implicit self method calls: address() → s.Address() in method context
	if ident, ok := c.Callee.(*parser.Ident); ok && g.currentMethods != nil {
		if g.currentMethods[ident.Name] {
			callee = "s." + exportName(ident.Name)
		}
		// Getter pattern: getField() → s.Field
		if strings.HasPrefix(ident.Name, "get") && len(ident.Name) > 3 {
			fieldName := strings.ToLower(ident.Name[3:4]) + ident.Name[4:]
			if g.currentFields != nil && g.currentFields[fieldName] {
				return "s." + exportName(fieldName)
			}
		}
	}

	// Rewrite `it` keyword in args
	var argStrs []string
	hasItRewrite := false
	for _, arg := range c.Args {
		if containsIt(arg) {
			hasItRewrite = true
			argStrs = append(argStrs, g.formatExprIt(arg))
		} else {
			argStrs = append(argStrs, g.formatExpr(arg))
		}
	}
	for _, na := range c.NamedArgs {
		argStrs = append(argStrs, g.formatExpr(na.Value))
	}
	_ = hasItRewrite
	args := strings.Join(argStrs, ", ")

	// Builtin rewrites
	switch callee {
	case "print":
		g.needImport("fmt")
		return fmt.Sprintf("fmt.Println(%s)", args)
	case "len":
		return fmt.Sprintf("len(%s)", args)
	case "str":
		g.needImport("fmt")
		return fmt.Sprintf("fmt.Sprint(%s)", args)
	case "int":
		g.needImport("strconv")
		return fmt.Sprintf("strconv.Atoi(%s)", args)
	case "float":
		g.needImport("strconv")
		return fmt.Sprintf("strconv.ParseFloat(%s, 64)", args)
	case "input":
		g.needImport("fmt")
		return fmt.Sprintf("func() string { var s string; fmt.Scanln(&s); return s }()")
	}

	// Constructor calls: new Type() → NewType()
	if c.IsNew {
		return fmt.Sprintf("New%s(%s)", callee, args)
	}

	return fmt.Sprintf("%s(%s)", callee, args)
}

func (g *Generator) formatLambdaExpr(l *parser.LambdaExpr) string {
	var params []string
	for _, p := range l.Params {
		typeName := "interface{}"
		if p.Type != nil {
			typeName = g.formatType(p.Type)
		}
		params = append(params, p.Name+" "+typeName)
	}
	paramStr := strings.Join(params, ", ")

	if l.Expr != nil {
		return fmt.Sprintf("func(%s) interface{} { return %s }", paramStr, g.formatExpr(l.Expr))
	}
	// Block lambda
	return fmt.Sprintf("func(%s) { /* block lambda */ }", paramStr)
}

func (g *Generator) formatStringInterp(s *parser.StringInterpLit) string {
	g.needImport("fmt")
	var fmtStr strings.Builder
	var args []string
	for _, p := range s.Parts {
		switch part := p.(type) {
		case *parser.StringLit:
			// Escape % signs for fmt.Sprintf
			escaped := strings.ReplaceAll(part.Value, "%", "%%")
			fmtStr.WriteString(escaped)
		default:
			fmtStr.WriteString("%v")
			args = append(args, g.formatExpr(part))
		}
	}
	if len(args) == 0 {
		return fmt.Sprintf("%q", fmtStr.String())
	}
	return fmt.Sprintf("fmt.Sprintf(%q, %s)", fmtStr.String(), strings.Join(args, ", "))
}

func (g *Generator) formatMatchExpr(m *parser.MatchExpr) string {
	// Go doesn't have switch expressions — use IIFE
	var sb strings.Builder
	sb.WriteString("func() interface{} { switch ")
	sb.WriteString(g.formatExpr(m.Subject))
	sb.WriteString(" { ")
	for _, c := range m.Cases {
		if c.Pattern == nil {
			sb.WriteString(fmt.Sprintf("default: return %s; ", g.formatExpr(c.Value)))
		} else {
			sb.WriteString(fmt.Sprintf("case %s: return %s; ", g.formatExpr(c.Pattern), g.formatExpr(c.Value)))
		}
	}
	sb.WriteString("}; return nil }()")
	return sb.String()
}

// --- Type formatting ---------------------------------------------------------

var zincToGoType = map[string]string{
	"int":     "int",
	"double":  "float64",
	"String":  "string",
	"boolean": "bool",
	"char":    "rune",
	"long":    "int64",
	"byte":    "byte",
	"void":    "",
	"Object":  "interface{}",
}

func (g *Generator) formatType(t parser.TypeExpr) string {
	switch typ := t.(type) {
	case *parser.SimpleType:
		if mapped, ok := zincToGoType[typ.Name]; ok {
			return mapped
		}
		return typ.Name
	case *parser.GenericType:
		switch typ.Name {
		case "List":
			if len(typ.TypeArgs) > 0 {
				return "[]" + g.formatType(typ.TypeArgs[0])
			}
			return "[]interface{}"
		case "Map":
			if len(typ.TypeArgs) >= 2 {
				return fmt.Sprintf("map[%s]%s", g.formatType(typ.TypeArgs[0]), g.formatType(typ.TypeArgs[1]))
			}
			return "map[string]interface{}"
		case "Set":
			if len(typ.TypeArgs) > 0 {
				return fmt.Sprintf("map[%s]struct{}", g.formatType(typ.TypeArgs[0]))
			}
			return "map[interface{}]struct{}"
		case "Channel", "Chan":
			if len(typ.TypeArgs) > 0 {
				return "chan " + g.formatType(typ.TypeArgs[0])
			}
			return "chan interface{}"
		default:
			// Generic struct — Go doesn't have generics for user types easily,
			// but Go 1.18+ supports them
			var args []string
			for _, a := range typ.TypeArgs {
				args = append(args, g.formatType(a))
			}
			return fmt.Sprintf("%s[%s]", typ.Name, strings.Join(args, ", "))
		}
	case *parser.ArrayType:
		return "[]" + g.formatType(typ.ElementType)
	case *parser.OptionalType:
		return "*" + g.formatType(typ.Inner)
	case *parser.FuncTypeExpr:
		// TODO: Function types — Fn<(Params), Return> → func(params) return
		// This is the known open issue for the Go backend.
		var params []string
		for _, p := range typ.Params {
			params = append(params, g.formatType(p))
		}
		ret := ""
		if typ.ReturnType != nil {
			ret = " " + g.formatType(typ.ReturnType)
		}
		return fmt.Sprintf("func(%s)%s", strings.Join(params, ", "), ret)
	default:
		return "interface{}"
	}
}

// formatReturnType builds the Go return type string including error if needed.
func (g *Generator) formatReturnType(retType parser.TypeExpr, body *parser.BlockStmt) string {
	if retType == nil {
		return ""
	}
	return " " + g.formatType(retType)
}

// formatParams formats function parameters.
func (g *Generator) formatParams(params []*parser.ParamDecl) string {
	var parts []string
	for _, p := range params {
		typeName := "interface{}"
		if p.Type != nil {
			typeName = g.formatType(p.Type)
		}
		if p.Variadic {
			typeName = "..." + typeName
		}
		parts = append(parts, p.Name+" "+typeName)
	}
	return strings.Join(parts, ", ")
}

func (g *Generator) formatExprList(exprs []parser.Expr) string {
	var parts []string
	for _, e := range exprs {
		parts = append(parts, g.formatExpr(e))
	}
	return strings.Join(parts, ", ")
}

func (g *Generator) formatStmtInline(s parser.Stmt) string {
	switch stmt := s.(type) {
	case *parser.VarStmt:
		if stmt.Value != nil {
			return fmt.Sprintf("%s := %s", stmt.Name, g.formatExpr(stmt.Value))
		}
		return fmt.Sprintf("var %s interface{}", stmt.Name)
	case *parser.AssignStmt:
		return fmt.Sprintf("%s %s %s", g.formatExpr(stmt.Target), stmt.Op, g.formatExpr(stmt.Value))
	case *parser.ExprStmt:
		return g.formatExpr(stmt.Expr)
	case *parser.ReturnStmt:
		if stmt.Value != nil {
			return "return " + g.formatExpr(stmt.Value)
		}
		return "return"
	default:
		return "/* inline stmt */"
	}
}

// --- it keyword helpers ------------------------------------------------------

func containsIt(e parser.Expr) bool {
	switch expr := e.(type) {
	case *parser.Ident:
		return expr.Name == "it"
	case *parser.BinaryExpr:
		return containsIt(expr.Left) || containsIt(expr.Right)
	case *parser.UnaryExpr:
		return containsIt(expr.Operand)
	case *parser.SelectorExpr:
		return containsIt(expr.Object)
	case *parser.CallExpr:
		if containsIt(expr.Callee) {
			return true
		}
		for _, a := range expr.Args {
			if containsIt(a) {
				return true
			}
		}
		return false
	case *parser.IndexExpr:
		return containsIt(expr.Object) || containsIt(expr.Index)
	default:
		return false
	}
}

func (g *Generator) formatExprIt(e parser.Expr) string {
	switch expr := e.(type) {
	case *parser.Ident:
		if expr.Name == "it" {
			return "_it"
		}
		return expr.Name
	case *parser.BinaryExpr:
		left := g.formatExprIt(expr.Left)
		right := g.formatExprIt(expr.Right)
		switch expr.Op {
		case "and":
			return fmt.Sprintf("%s && %s", left, right)
		case "or":
			return fmt.Sprintf("%s || %s", left, right)
		default:
			return fmt.Sprintf("%s %s %s", left, expr.Op, right)
		}
	case *parser.UnaryExpr:
		return fmt.Sprintf("%s%s", expr.Op, g.formatExprIt(expr.Operand))
	case *parser.SelectorExpr:
		return fmt.Sprintf("%s.%s", g.formatExprIt(expr.Object), expr.Field)
	case *parser.CallExpr:
		callee := g.formatExprIt(expr.Callee)
		var args []string
		for _, a := range expr.Args {
			args = append(args, g.formatExprIt(a))
		}
		return fmt.Sprintf("%s(%s)", callee, strings.Join(args, ", "))
	default:
		return g.formatExpr(e)
	}
}

// --- Output helpers ----------------------------------------------------------

func (g *Generator) writeln(format string, args ...interface{}) {
	g.buf.WriteString(strings.Repeat("\t", g.indent))
	fmt.Fprintf(&g.buf, format, args...)
	g.buf.WriteString("\n")
}

func (g *Generator) write(format string, args ...interface{}) {
	g.buf.WriteString(strings.Repeat("\t", g.indent))
	fmt.Fprintf(&g.buf, format, args...)
}

// --- Name helpers ------------------------------------------------------------

// exportName capitalizes the first letter to make it exported in Go.
func exportName(name string) string {
	if name == "" {
		return ""
	}
	// Already capitalized
	if name[0] >= 'A' && name[0] <= 'Z' {
		return name
	}
	return strings.ToUpper(name[:1]) + name[1:]
}
