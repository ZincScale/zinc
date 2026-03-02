package parser

// Node is the base interface for all AST nodes.
type Node interface {
	nodeTag()
}

// --- Top-level ---------------------------------------------------------------

// Program is the root AST node.
type Program struct {
	Package  *PackageDecl   // optional package declaration (nil = package main)
	Imports  []*ImportDecl
	Decls    []TopLevelDecl // ClassDecl | InterfaceDecl | FnDecl
}

// PackageDecl: package "path/to/pkg"
type PackageDecl struct {
	Path string // e.g. "myapp/utils"
}

func (p *PackageDecl) nodeTag() {}

func (p *Program) nodeTag() {}

// TopLevelDecl marks top-level declarations.
type TopLevelDecl interface {
	Node
	topLevelTag()
}

// ImportDecl represents: import "path" as alias
type ImportDecl struct {
	Path  string // bare path, e.g. "fmt"
	Alias string // optional alias
}

func (i *ImportDecl) nodeTag()      {}
func (i *ImportDecl) topLevelTag() {}

// --- Declarations ------------------------------------------------------------

// ClassDecl: class Dog[<T>] : Animal, Speaker { ... }
type ClassDecl struct {
	Name       string
	TypeParams []string // generic type parameter names
	Parents    []string // base class + interfaces
	Fields     []*FieldDecl
	Ctor       *CtorDecl // nil if no constructor
	Methods    []*MethodDecl
}

func (c *ClassDecl) nodeTag()      {}
func (c *ClassDecl) topLevelTag() {}

// InterfaceDecl: interface Speaker { ... }
type InterfaceDecl struct {
	Name    string
	Methods []*MethodSig
}

func (i *InterfaceDecl) nodeTag()      {}
func (i *InterfaceDecl) topLevelTag() {}

// MethodSig is an interface method signature.
type MethodSig struct {
	Name       string
	IsPub      bool
	Params     []*ParamDecl
	ReturnType TypeExpr // nil = void
}

// CtorDecl: construct new(params) { body }
type CtorDecl struct {
	Params    []*ParamDecl
	Body      *BlockStmt
	SuperArgs []Expr // args extracted from super(...) call in body
}

// MethodDecl: [pub] [static] fn name(params) [: ReturnType] { body }
type MethodDecl struct {
	Name       string
	IsPub      bool
	IsStatic   bool
	Params     []*ParamDecl
	ReturnType TypeExpr // nil = void
	Body       *BlockStmt
	CanThrow   bool // set by codegen first pass
}

func (m *MethodDecl) nodeTag() {}

// FnDecl: [pub] fn name[<T, U>](params) [: ReturnType] { body }
type FnDecl struct {
	Name       string
	IsPub      bool
	TypeParams []string  // generic type parameter names, e.g. ["T", "U"]
	Params     []*ParamDecl
	ReturnType TypeExpr // nil = void
	Body       *BlockStmt
	CanThrow   bool // set by codegen first pass
}

func (f *FnDecl) nodeTag()      {}
func (f *FnDecl) topLevelTag() {}

// EnumDecl: enum Color { Red, Green, Blue }
type EnumDecl struct {
	Name     string
	Variants []string
}

func (e *EnumDecl) nodeTag()      {}
func (e *EnumDecl) topLevelTag() {}

// FieldDecl: var name: Type [= expr]
type FieldDecl struct {
	Name    string
	Type    TypeExpr
	Default Expr // may be nil
}

// ParamDecl: name: Type
type ParamDecl struct {
	Name string
	Type TypeExpr
}

// --- Type Expressions --------------------------------------------------------

// TypeExpr represents a type.
type TypeExpr interface {
	Node
	typeTag()
}

// SimpleType: Int, String, MyClass, etc.
type SimpleType struct {
	Name string
}

func (s *SimpleType) nodeTag()  {}
func (s *SimpleType) typeTag()  {}

// GenericType: List<T>, Map<K,V>, Chan<T>
type GenericType struct {
	Name     string // List, Map, Chan
	TypeArgs []TypeExpr
}

func (g *GenericType) nodeTag() {}
func (g *GenericType) typeTag() {}

// OptionalType: String? — nullable/pointer type
type OptionalType struct {
	Inner TypeExpr
}

func (o *OptionalType) nodeTag() {}
func (o *OptionalType) typeTag() {}

// --- Statements --------------------------------------------------------------

// Stmt is a statement node.
type Stmt interface {
	Node
	stmtTag()
}

// BlockStmt: { stmts... }
type BlockStmt struct {
	Stmts []Stmt
}

func (b *BlockStmt) nodeTag() {}
func (b *BlockStmt) stmtTag() {}

// VarStmt: var name [: Type] = expr  OR  var name: Type
type VarStmt struct {
	Name  string
	Type  TypeExpr // may be nil (inferred)
	Value Expr     // may be nil
}

func (v *VarStmt) nodeTag() {}
func (v *VarStmt) stmtTag() {}

// TupleVarStmt: var (a, b) = expr  — multi-value unpacking
type TupleVarStmt struct {
	Names []string
	Value Expr
}

func (t *TupleVarStmt) nodeTag() {}
func (t *TupleVarStmt) stmtTag() {}

// AssignStmt: target = expr  OR  target op= expr
type AssignStmt struct {
	Target Expr
	Op     string // "=", "+=", "-=", "*=", "/="
	Value  Expr
}

func (a *AssignStmt) nodeTag() {}
func (a *AssignStmt) stmtTag() {}

// ReturnStmt: return [expr]
type ReturnStmt struct {
	Value Expr // nil for bare return
}

func (r *ReturnStmt) nodeTag() {}
func (r *ReturnStmt) stmtTag() {}

// IfStmt: if (cond) { } [else { }]
type IfStmt struct {
	Cond     Expr
	Then     *BlockStmt
	ElseStmt Stmt // *BlockStmt or *IfStmt (else if)
}

func (i *IfStmt) nodeTag() {}
func (i *IfStmt) stmtTag() {}

// ForStmt: for (init; cond; post) { }  OR  for item in list { }
type ForStmt struct {
	// C-style
	Init Stmt // VarStmt or AssignStmt
	Cond Expr
	Post Stmt // AssignStmt

	// Range-style (for item in list)
	IsRange bool
	Item    string
	Range   Expr

	Body *BlockStmt
}

func (f *ForStmt) nodeTag() {}
func (f *ForStmt) stmtTag() {}

// WhileStmt: while (cond) { }
type WhileStmt struct {
	Cond Expr
	Body *BlockStmt
}

func (w *WhileStmt) nodeTag() {}
func (w *WhileStmt) stmtTag() {}

// GoStmt: go { ... }
type GoStmt struct {
	Body *BlockStmt
}

func (g *GoStmt) nodeTag() {}
func (g *GoStmt) stmtTag() {}

// TryStmt: try { } catch(err) { }
type TryStmt struct {
	Body     *BlockStmt
	ErrVar   string
	CatchBody *BlockStmt
}

func (t *TryStmt) nodeTag() {}
func (t *TryStmt) stmtTag() {}

// ThrowStmt: throw expr
type ThrowStmt struct {
	Value Expr
}

func (t *ThrowStmt) nodeTag() {}
func (t *ThrowStmt) stmtTag() {}

// PrintStmt: print(expr)
type PrintStmt struct {
	Value Expr
}

func (p *PrintStmt) nodeTag() {}
func (p *PrintStmt) stmtTag() {}

// ExprStmt wraps an expression used as a statement.
type ExprStmt struct {
	Expr Expr
}

func (e *ExprStmt) nodeTag() {}
func (e *ExprStmt) stmtTag() {}

// MatchStmt: match expr { case val => { body } ... _ => { body } }
type MatchStmt struct {
	Subject Expr
	Cases   []*MatchCase
}

func (m *MatchStmt) nodeTag() {}
func (m *MatchStmt) stmtTag() {}

// MatchCase: case val => { body }  OR  _ => { body }
type MatchCase struct {
	Pattern Expr       // nil = wildcard (_)
	Body    *BlockStmt
}

// BreakStmt: break
type BreakStmt struct{}

func (b *BreakStmt) nodeTag() {}
func (b *BreakStmt) stmtTag() {}

// ContinueStmt: continue
type ContinueStmt struct{}

func (c *ContinueStmt) nodeTag() {}
func (c *ContinueStmt) stmtTag() {}

// --- Expressions -------------------------------------------------------------

// Expr is an expression node.
type Expr interface {
	Node
	exprTag()
}

// BinaryExpr: left op right
type BinaryExpr struct {
	Left  Expr
	Op    string
	Right Expr
}

func (b *BinaryExpr) nodeTag() {}
func (b *BinaryExpr) exprTag() {}

// UnaryExpr: op operand
type UnaryExpr struct {
	Op      string
	Operand Expr
}

func (u *UnaryExpr) nodeTag() {}
func (u *UnaryExpr) exprTag() {}

// CallExpr: callee(args)  e.g. Dog.new("Rex") or obj.method(x)
type CallExpr struct {
	Callee Expr
	Args   []Expr
}

func (c *CallExpr) nodeTag() {}
func (c *CallExpr) exprTag() {}

// SelectorExpr: obj.field
type SelectorExpr struct {
	Object Expr
	Field  string
}

func (s *SelectorExpr) nodeTag() {}
func (s *SelectorExpr) exprTag() {}

// IndexExpr: obj[index]
type IndexExpr struct {
	Object Expr
	Index  Expr
}

func (i *IndexExpr) nodeTag() {}
func (i *IndexExpr) exprTag() {}

// SendExpr: ch.send(val)  → ch <- val
type SendExpr struct {
	Chan  Expr
	Value Expr
}

func (s *SendExpr) nodeTag() {}
func (s *SendExpr) exprTag() {}

// ReceiveExpr: ch.receive()  → <-ch
type ReceiveExpr struct {
	Chan Expr
}

func (r *ReceiveExpr) nodeTag() {}
func (r *ReceiveExpr) exprTag() {}

// ThisExpr: this
type ThisExpr struct{}

func (t *ThisExpr) nodeTag() {}
func (t *ThisExpr) exprTag() {}

// SuperCallExpr: super(args) — used inside ctor body
type SuperCallExpr struct {
	Args []Expr
}

func (s *SuperCallExpr) nodeTag() {}
func (s *SuperCallExpr) exprTag() {}

// Ident: a bare identifier
type Ident struct {
	Name string
}

func (i *Ident) nodeTag() {}
func (i *Ident) exprTag() {}

// IntLit: 42
type IntLit struct {
	Value string
}

func (i *IntLit) nodeTag() {}
func (i *IntLit) exprTag() {}

// FloatLit: 3.14
type FloatLit struct {
	Value string
}

func (f *FloatLit) nodeTag() {}
func (f *FloatLit) exprTag() {}

// StringLit: "hello"
type StringLit struct {
	Value string
}

func (s *StringLit) nodeTag() {}
func (s *StringLit) exprTag() {}

// StringInterpLit: "Hello, {name}!" — string with interpolated expressions
// Parts alternate: StringLit (static text), then Expr (interpolated), etc.
type StringInterpLit struct {
	Parts []Expr // alternating StringLit and Expr nodes
}

func (s *StringInterpLit) nodeTag() {}
func (s *StringInterpLit) exprTag() {}

// BoolLit: true / false
type BoolLit struct {
	Value bool
}

func (b *BoolLit) nodeTag() {}
func (b *BoolLit) exprTag() {}

// NullLit: null
type NullLit struct{}

func (n *NullLit) nodeTag() {}
func (n *NullLit) exprTag() {}

// ListLit: [a, b, c]
type ListLit struct {
	Elements []Expr
}

func (l *ListLit) nodeTag() {}
func (l *ListLit) exprTag() {}

// MapLit: {"key": value, ...}
type MapLit struct {
	Keys   []Expr
	Values []Expr
}

func (m *MapLit) nodeTag() {}
func (m *MapLit) exprTag() {}
