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

package parser

// Node is the base interface for all AST nodes.
type Node interface {
	nodeTag()
}

// --- Top-level ---------------------------------------------------------------

// Program is the root AST node.
type Program struct {
	SourceFile string             // source .zn file (set by project mode)
	Package    *PackageDecl       // optional package declaration (nil = package main)
	Imports    []*ImportDecl
	Decls      []TopLevelDecl // ClassDecl | InterfaceDecl | FnDecl
	Stmts      []Stmt         // v2: top-level script statements (script mode)
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

// ClassDecl: [@annotations] [abstract] [sealed] class Dog[<T>] : Animal, Speaker { ... }
type ClassDecl struct {
	Line             int // source line number (1-indexed)
	Name             string
	IsSealed         bool                  // sealed class (has variant data classes)
	TypeParams       []string              // generic type parameter names
	TypeParamBounds  map[string][]TypeExpr // bounds per type param (3.6.1); nil if no bounds declared
	Parents          []ParentRef           // base class + interfaces (with generic args)
	Fields      []*FieldDecl
	Ctor        *CtorDecl   // primary constructor (nil if none)
	Ctors       []*CtorDecl // overloaded constructors (nil or empty if none)
	Methods     []*MethodDecl
	Variants    []*DataClassDecl // sealed class variants
	Annotations []*Annotation
}

// ParentRef is one entry in a class's parent list. zinc syntax
// `class Foo : Bar, Container<T>, core.Describable` produces:
//   ParentRef{Name: "Bar"}
//   ParentRef{Name: "Container", TypeArgs: [T]}
//   ParentRef{Name: "core.Describable"}
//
// TypeArgs is nil for non-generic parents. Codegen for targets that
// need to propagate generics through inheritance (Crystal: `include
// Container(T)`) consumes TypeArgs; Go-target ignores it because Go's
// type system carries generics inline at the use site.
type ParentRef struct {
	Name     string
	TypeArgs []TypeExpr
}

func (c *ClassDecl) nodeTag()      {}
func (c *ClassDecl) topLevelTag() {}

// InterfaceDecl: interface Speaker[<T>] { ... }
type InterfaceDecl struct {
	Line            int
	Name            string
	TypeParams      []string              // generic type parameter names
	TypeParamBounds map[string][]TypeExpr // bounds per type param (3.6.1); nil if no bounds declared
	Methods         []*MethodSig
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

// CtorDecl: new(params) { body }
type CtorDecl struct {
	Params      []*ParamDecl
	Body        *BlockStmt
	SuperArgs   []Expr // args extracted from super(...) call in body
	SuperCalled bool   // true when super(...) appeared in body (even with zero args)
}

// MethodDecl: [@annotations] [pub] fn name(params) [: ReturnType] { body }
type MethodDecl struct {
	Name        string
	IsPub       bool
	Params      []*ParamDecl
	ReturnType  TypeExpr // nil = void
	Body        *BlockStmt
	Annotations []*Annotation
}

func (m *MethodDecl) nodeTag() {}

// FnDecl: [@annotations] [pub] fn name[<T, U>](params) [: ReturnType] { body }
type FnDecl struct {
	Line            int // source line number (1-indexed)
	Name            string
	IsPub           bool
	TypeParams      []string              // generic type parameter names, e.g. ["T", "U"]
	TypeParamBounds map[string][]TypeExpr // bounds per type param (3.6.1); nil if no bounds declared
	Params          []*ParamDecl
	ReturnType      TypeExpr // nil = void
	Body            *BlockStmt
	Annotations     []*Annotation
}

func (f *FnDecl) nodeTag()      {}
func (f *FnDecl) topLevelTag() {}
func (f *FnDecl) stmtTag()     {} // v2: nested functions are statements

// TestDecl: `test "name" { body }` — a test case.
// Top-level in *_test.zn files. Compiled to `func TestName(t *testing.T) { ... }`
// in Go, where `t` is an implicit parameter visible to the body (assertion
// helpers in stdlib/testing expect it as their first arg).
type TestDecl struct {
	Line int
	Name string // from the string literal
	Body *BlockStmt
}

func (t *TestDecl) nodeTag()     {}
func (t *TestDecl) topLevelTag() {}

// DataClassDecl: data User(pub String name, pub Int age) { optional methods }
type DataClassDecl struct {
	Line            int // source line number (1-indexed)
	Name            string
	TypeParams      []string              // generic type parameter names
	TypeParamBounds map[string][]TypeExpr // bounds per type param (3.6.1); nil if no bounds declared
	Parents         []ParentRef           // base class + interfaces (with generic args)
	Params          []*FieldDecl          // constructor params (become fields)
	Methods         []*MethodDecl
}

func (d *DataClassDecl) nodeTag()      {}
func (d *DataClassDecl) topLevelTag() {}

// EnumDecl: enum Color { Red, Green, Blue }
type EnumDecl struct {
	Line     int // source line number (1-indexed)
	Name     string
	Variants []string
}

func (e *EnumDecl) nodeTag()      {}
func (e *EnumDecl) topLevelTag() {}

// ConstDecl: [pub] const NAME: Type = expr
type ConstDecl struct {
	Line  int // source line number (1-indexed)
	Name  string
	IsPub bool
	Type  TypeExpr // may be nil (inferred)
	Value Expr
}

func (c *ConstDecl) nodeTag()      {}
func (c *ConstDecl) topLevelTag() {}

// TypeAliasDecl: type Name = TypeExpr
type TypeAliasDecl struct {
	Line int
	Name string
	Type TypeExpr
}

func (t *TypeAliasDecl) nodeTag()      {}
func (t *TypeAliasDecl) topLevelTag() {}

// Annotation: @Name or @Name("arg1", "arg2")
type Annotation struct {
	Name string   // annotation name (e.g. "JsonPropertyName", "Route")
	Args []string // string arguments (may be empty)
}

func (a *Annotation) nodeTag() {}

// FieldDecl: var type name [= default]  |  const type name = default  |  init type name
type FieldDecl struct {
	Name        string
	IsPub       bool
	IsReadonly  bool
	IsConst     bool // const field (immutable with default)
	IsInit      bool // init field (set in constructor, frozen after)
	Type        TypeExpr
	Default     Expr // may be nil
	Annotations []*Annotation
}

// ParamDecl: [const] type name [= expr]  OR  *args  OR  **kwargs
type ParamDecl struct {
	Name     string
	Type     TypeExpr
	Default  Expr // nil if no default
	Variadic bool // true for ...Type params
	IsConst  bool // true for const params (cannot reassign in body)
}

// NamedArg: name: expr at a call site
type NamedArg struct {
	Name  string
	Value Expr
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


// ArrayType: int[], String[], Type[]
type ArrayType struct {
	ElementType TypeExpr
}

func (a *ArrayType) nodeTag() {}
func (a *ArrayType) typeTag() {}

// FuncTypeExpr: Fn<(Int, String), Bool> → func(int, string) bool
type FuncTypeExpr struct {
	Params     []TypeExpr // parameter types
	ReturnType TypeExpr   // return type (nil = void)
}

func (f *FuncTypeExpr) nodeTag() {}
func (f *FuncTypeExpr) typeTag() {}

// TupleType: (Int, String) — multi-value return position only.
// Lowers to Go's tuple-return shape `(int, string)`. A single-element
// `(T)` is unwrapped at parse time, so a TupleType always has >= 2
// elements. Allowed only at function/method return-type position and
// inside Fn<(...), HERE>; never as a value/var/field type.
type TupleType struct {
	Elements []TypeExpr
}

func (t *TupleType) nodeTag() {}
func (t *TupleType) typeTag() {}

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

// VarStmt: var [type] name = expr  OR  const [type] name = expr
type VarStmt struct {
	Line         int // source line number (1-indexed)
	Name         string
	Type         TypeExpr   // may be nil (inferred)
	Value        Expr       // may be nil
	IsConst      bool       // true for const declarations (immutable)
	OrHandler    *OrHandler // optional or { } handler for failable calls
	ResolvedType string     // filled by typechecker for var + or handler inference
}

func (v *VarStmt) nodeTag() {}
func (v *VarStmt) stmtTag() {}

// TupleVarStmt: (a, b) := expr — multi-value unpacking (error auto-propagates)
type TupleVarStmt struct {
	Line      int // source line number (1-indexed)
	Names     []string
	Value     Expr
	OrHandler *OrHandler // optional or { } handler for failable calls
}

func (t *TupleVarStmt) nodeTag() {}
func (t *TupleVarStmt) stmtTag() {}

// AssignStmt: target = expr  OR  target op= expr
type AssignStmt struct {
	Line      int // source line number (1-indexed)
	Target    Expr
	Op        string // "=", "+=", "-=", "*=", "/="
	Value     Expr
	OrHandler *OrHandler // optional or { } handler for failable calls
}

func (a *AssignStmt) nodeTag() {}
func (a *AssignStmt) stmtTag() {}

// ReturnStmt: return [expr]
type ReturnStmt struct {
	Line  int  // source line number (1-indexed)
	Value Expr // nil for bare return
}

func (r *ReturnStmt) nodeTag() {}
func (r *ReturnStmt) stmtTag() {}

// IfStmt: if (cond) { } [else { }]
type IfStmt struct {
	Line     int // source line number (1-indexed)
	Cond     Expr
	Then     *BlockStmt
	ElseStmt Stmt // *BlockStmt or *IfStmt (else if)
}

func (i *IfStmt) nodeTag() {}
func (i *IfStmt) stmtTag() {}

// ForStmt: for (init; cond; post) { }  OR  for item in list { }  OR  for (i, item) in list { }
type ForStmt struct {
	Line  int // source line number (1-indexed)

	// C-style
	Init Stmt // VarStmt or AssignStmt
	Cond Expr
	Post Stmt // AssignStmt

	// Range-style (for item in list OR for (i, item) in list)
	IsRange  bool
	IndexVar string // optional index variable (for (i, item) in list); empty if not present
	Item     string
	Range    Expr

	Body *BlockStmt
}

func (f *ForStmt) nodeTag() {}
func (f *ForStmt) stmtTag() {}

// WhileStmt: while (cond) { }
type WhileStmt struct {
	Line  int // source line number (1-indexed)
	Cond  Expr
	Body  *BlockStmt
}

func (w *WhileStmt) nodeTag() {}
func (w *WhileStmt) stmtTag() {}

// GoStmt: go { ... }
type GoStmt struct {
	Line int
	Body *BlockStmt
}

func (g *GoStmt) nodeTag() {}
func (g *GoStmt) stmtTag() {}

// OrHandler: or { body } — inline error handler. `err` is implicitly available.
// The `or match err { case Type -> ... }` form was removed 2026-05-01;
// callers wanting type-switching errors write `or { match (err) { ... } }`.
type OrHandler struct {
	Body *BlockStmt // handler body; `err` is implicitly available
}

func (o *OrHandler) nodeTag() {}

// PrintStmt: print(expr)
type PrintStmt struct {
	Line  int // source line number (1-indexed)
	Value Expr
}

func (p *PrintStmt) nodeTag() {}
func (p *PrintStmt) stmtTag() {}

// ExprStmt wraps an expression used as a statement.
type ExprStmt struct {
	Line      int // source line number (1-indexed)
	Expr      Expr
	OrHandler *OrHandler // optional or { } handler for failable calls
}

func (e *ExprStmt) nodeTag() {}
func (e *ExprStmt) stmtTag() {}

// MatchStmt: match expr { case val => { body } ... _ => { body } }
type MatchStmt struct {
	Line    int // source line number (1-indexed)
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

// SelectStmt: multiplex over channel send/recv operations.
//
//   select {
//       case x = ch1.recv(): ...      // recv with binding
//       case ch2.recv(): ...           // recv without binding
//       case ch3.send(value): ...      // send
//       case time.After(d).recv(): ... // timer (just a recv on a timer chan)
//       default: ...                   // optional, fires when none ready
//   }
//
// Maps 1:1 to Go's select statement. Each case's CallExpr is restricted
// to <chan>.recv() or <chan>.send(arg) at parse time.
type SelectStmt struct {
	Line    int           // source line number (1-indexed)
	Cases   []*SelectCase
	Default *BlockStmt    // nil if no default
}

func (s *SelectStmt) nodeTag() {}
func (s *SelectStmt) stmtTag() {}

// SelectCase: one arm of a select. Kind is "recv" or "send".
//   - Recv:  Channel.Recv() with optional Binding (binding == "" → no binding)
//   - Send:  Channel.Send(SendValue)
type SelectCase struct {
	Kind      string     // "recv" | "send"
	Channel   Expr       // the channel expression (left of .recv()/.send())
	Binding   string     // recv-only: var name to bind, "" if none
	SendValue Expr       // send-only: the value being sent
	Body      *BlockStmt
}

// BreakStmt: break
type BreakStmt struct{}

func (b *BreakStmt) nodeTag() {}
func (b *BreakStmt) stmtTag() {}

// ContinueStmt: continue
type ContinueStmt struct{}

func (c *ContinueStmt) nodeTag() {}
func (c *ContinueStmt) stmtTag() {}

// DeferStmt: defer expr
type DeferStmt struct {
	Expr Expr
}

func (d *DeferStmt) nodeTag() {}
func (d *DeferStmt) stmtTag() {}

// ParallelForStmt: parallel [( max: N )] for item in items { body } [or { handler }]
type ParallelForStmt struct {
	Line      int
	Item      string
	IndexVar  string
	Range     Expr
	Body      *BlockStmt
	OrHandler *OrHandler // optional error handler
	Max       int        // 0 = unbounded, >0 = bounded concurrency via semaphore
}

func (p *ParallelForStmt) nodeTag() {}
func (p *ParallelForStmt) stmtTag() {}

// AssertStmt: assert expr [, "message"]
type AssertStmt struct {
	Line    int
	Cond    Expr
	Message Expr // optional message (nil if no message)
}

func (a *AssertStmt) nodeTag() {}
func (a *AssertStmt) stmtTag() {}

// WithResource is a single resource binding inside a with statement.
type WithResource struct {
	Name      string
	Value     Expr
	AutoErr   bool       // set by codegen when multi-return (T, error) is auto-detected
	OrHandler *OrHandler // optional or { } handler for failable calls
}

// WithStmt: with (var name = expr [, var name = expr ...]) { body }
// Each resource has .Close() deferred automatically. Also used as the
// AST node for `lock (mu) { ... }` (Resources[0].Name == "_lock") and
// `using (var r = init) { ... }` (regular resource, .Close() deferred).
type WithStmt struct {
	Line      int // source line number (1-indexed)
	Resources []*WithResource
	Body      *BlockStmt
}

func (w *WithStmt) nodeTag() {}
func (w *WithStmt) stmtTag() {}

// TimeoutStmt: timeout(dur) { body } [or { fallback }]
type TimeoutStmt struct {
	Line      int
	Duration  Expr       // duration expression
	Body      *BlockStmt // body to execute with deadline
	OrHandler *OrHandler // optional fallback on timeout
}

func (t *TimeoutStmt) nodeTag() {}
func (t *TimeoutStmt) stmtTag() {}

// --- Expressions -------------------------------------------------------------

// Expr is an expression node.
type Expr interface {
	Node
	exprTag()
}

// BinaryExpr: left op right
type BinaryExpr struct {
	Left         Expr
	Op           string
	Right        Expr
	ResolvedType string // filled by typechecker for codegen type decisions
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

// CallExpr: callee(args)  e.g. Dog("Rex") or obj.method(x)
// CallExpr with type args: callee<T>(args)  e.g. jsonDecode<Config>(data)
type CallExpr struct {
	Callee    Expr
	Args      []Expr     // positional args (must come before named args)
	NamedArgs []NamedArg // named args, may be empty
	TypeArgs  []string   // type arguments, e.g. ["Config"] in jsonDecode<Config>(data)
	IsNew     bool       // true when preceded by `new` keyword
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

// SafeNavExpr: obj?.field or obj?.method(args)
type SafeNavExpr struct {
	Object Expr
	Field  string
	Call   *CallExpr // non-nil if obj?.method(args)
}

func (s *SafeNavExpr) nodeTag() {}
func (s *SafeNavExpr) exprTag() {}

// IndexExpr: obj[index]
type IndexExpr struct {
	Object Expr
	Index  Expr
}

func (i *IndexExpr) nodeTag() {}
func (i *IndexExpr) exprTag() {}

// SliceExpr: obj[low:high] — low and high are optional
type SliceExpr struct {
	Object Expr
	Low    Expr // nil means from start
	High   Expr // nil means to end
}

func (s *SliceExpr) nodeTag() {}
func (s *SliceExpr) exprTag() {}

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

// CapacityExpr: List<T>(capacity) or Map<K,V>(capacity) — collection with initial capacity.
type CapacityExpr struct {
	CollectionType *GenericType // List<String>, Map<String, int>, etc.
	Capacity       Expr         // capacity expression
}

func (c *CapacityExpr) nodeTag() {}
func (c *CapacityExpr) exprTag() {}

// SizedArrayExpr: Type[size] — creates a zero-filled array of given size.
// e.g. byte[4], int[10], String[5]
type SizedArrayExpr struct {
	ElementType string // "byte", "int", "String", etc.
	Size        Expr   // size expression (usually IntLit)
}

func (s *SizedArrayExpr) nodeTag() {}
func (s *SizedArrayExpr) exprTag() {}

// ListLit: [a, b, c]
type ListLit struct {
	Elements     []Expr
	ResolvedType string   // set by typechecker; Go type string like "[]int"
	ExplicitType TypeExpr // set when parsed as List<Type>[] — the generic type
}

func (l *ListLit) nodeTag() {}
func (l *ListLit) exprTag() {}

// MapLit: {"key": value, ...}
type MapLit struct {
	Keys         []Expr
	Values       []Expr
	ResolvedType string   // set by typechecker; Go type string like "map[string]int"
	ExplicitType TypeExpr // set when parsed as Map<K,V>{} — the generic type
}

func (m *MapLit) nodeTag() {}
func (m *MapLit) exprTag() {}

// DefaultExpr: default(T) — Go's zero value for type T. Useful in
// expression positions where a bare var-decl's auto-zero doesn't
// apply (function args, returns, ternary branches, generic bodies).
// Codegen consults zeroValueFor on the resolved Go type.
type DefaultExpr struct {
	Type TypeExpr
}

func (*DefaultExpr) nodeTag() {}
func (*DefaultExpr) exprTag() {}

// TypeAssertExpr: x as Type  OR  x is Type
//
// TypeExpr carries the full parsed RHS (supports generics, arrays,
// optionals, qualified names). TypeName is the display string used
// in error messages and as a fallback for older codegen paths that
// only need the bare name; populated by the parser from TypeExpr.
type TypeAssertExpr struct {
	Object   Expr
	TypeExpr TypeExpr
	TypeName string
	IsCheck  bool // true = "is" (returns bool), false = "as" (type assertion)
}

func (*TypeAssertExpr) nodeTag() {}
func (*TypeAssertExpr) exprTag() {}

// RawStringLit: `raw string` (backtick literal — no escape processing)
type RawStringLit struct {
	Value string
}

func (r *RawStringLit) nodeTag() {}
func (r *RawStringLit) exprTag() {}

// The following builtin method AST nodes have been removed.
// All method calls (.add, .remove, .size, .clone, .upper, .lower, .contains,
// .startsWith, .endsWith, .trim, .split, .replace, .join, .sort, .keys,
// .values, .containsKey, .send, .receive) are now parsed as regular CallExpr
// and dispatched to builtin behavior in codegen based on receiver type.

// SpawnExpr: spawn { body } [or { handler }] — spawns a virtual thread
type SpawnExpr struct {
	Line      int
	Body      *BlockStmt
	OrHandler *OrHandler // optional error handler
}

func (*SpawnExpr) nodeTag() {}
func (*SpawnExpr) exprTag() {}

// SpreadExpr: expr... — spread a list into variadic args
type SpreadExpr struct {
	Expr Expr
}

func (*SpreadExpr) nodeTag() {}
func (*SpreadExpr) exprTag() {}

// IfExpr: if cond { expr } else { expr } — expression-position if
type IfExpr struct {
	Cond Expr
	Then Expr
	Else Expr // required — expression if must have else
}

func (*IfExpr) nodeTag() {}
func (*IfExpr) exprTag() {}

// MatchExpr: match subject { case pat -> expr, ... } — expression-position match
type MatchExpr struct {
	Subject Expr
	Cases   []*MatchExprCase
}

func (*MatchExpr) nodeTag() {}
func (*MatchExpr) exprTag() {}

// MatchExprCase: case pattern -> expr
type MatchExprCase struct {
	Pattern Expr // nil = wildcard (_)
	Value   Expr
}

// RangeExpr: start..end (exclusive) or start..=end (inclusive)
type RangeExpr struct {
	Start     Expr
	End       Expr
	Inclusive bool // true for ..=, false for ..
}

func (*RangeExpr) nodeTag() {}
func (*RangeExpr) exprTag() {}

// TupleLit: (a, b, c)
type TupleLit struct {
	Elements []Expr
}

func (*TupleLit) nodeTag() {}
func (*TupleLit) exprTag() {}

// LambdaExpr: (params): ReturnType => expr   OR   (params): ReturnType => { ... }
type LambdaExpr struct {
	Params     []*ParamDecl
	ReturnType TypeExpr   // nil = void (block) or interface{} (single-expr)
	Body       *BlockStmt // non-nil for block-body form
	Expr       Expr       // non-nil for single-expression form
}

func (*LambdaExpr) nodeTag()            {}
func (*LambdaExpr) exprTag()            {}
