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

import "zinc-go/internal/parser"

// pytype is a coarse static type used only to bridge Python's numeric tower
// to Go's strict typing. Python promotes int→float freely in mixed
// arithmetic; Go does not (`intVar + float64` is a compile error). The
// front-end tracks just enough type to know when to inject a float()
// conversion. `tUnknown` means "don't promote" — we only convert when we are
// certain one side is int and the other float.
type pytype int

const (
	tUnknown pytype = iota
	tInt
	tFloat
	tStr
	tBool
	// tDynamic is a value whose Go representation is `any` and whose runtime
	// type is only known at runtime — currently the result of a CPython FFI
	// call/attribute, and anything derived from one (an indexed element, a
	// loop variable over one). Operations on a dynamic value (index, iterate,
	// len, numeric coercion) are routed through runtime helpers that dispatch
	// on the boxed type, rather than emitted as native Go (which would not
	// compile against an interface{}).
	tDynamic
)

// dictMeta records the key and value types of a dict-typed variable so reads
// can be asserted to the value type and loop variables typed to the key type.
type dictMeta struct{ key, val pytype }

func (t pytype) String() string {
	switch t {
	case tInt:
		return "int"
	case tFloat:
		return "float"
	case tStr:
		return "str"
	case tBool:
		return "bool"
	case tDynamic:
		return "dynamic"
	}
	return "unknown"
}

// narrowHint returns the Python conversion built-in that narrows a dynamic
// value to t, for use in diagnostics suggesting how to keep a local statically
// typed (e.g. "int(...)"). Falls back to "cast" for types with no scalar
// constructor.
func narrowHint(t pytype) string {
	switch t {
	case tInt:
		return "int"
	case tFloat:
		return "float"
	case tStr:
		return "str"
	case tBool:
		return "bool"
	}
	return "cast"
}

// typeFromExpr maps a Zinc type-hint node to a pytype.
func typeFromExpr(t parser.TypeExpr) pytype {
	st, ok := t.(*parser.SimpleType)
	if !ok {
		return tUnknown
	}
	switch st.Name {
	case "int":
		return tInt
	case "float", "double":
		return tFloat
	case "String", "string":
		return tStr
	case "bool":
		return tBool
	case "Any", "any":
		return tDynamic
	}
	return tUnknown
}

// typeOf infers the pytype of an already-built expression using the current
// scope's variable types and known function return types.
func (p *Parser) typeOf(e parser.Expr) pytype {
	switch e := e.(type) {
	case *parser.IntLit:
		return tInt
	case *parser.FloatLit:
		return tFloat
	case *parser.StringLit, *parser.RawStringLit, *parser.StringInterpLit:
		return tStr
	case *parser.BoolLit:
		return tBool
	case *parser.Ident:
		return p.lookupType(e.Name)
	case *parser.IndexExpr:
		// Indexing a known-element-typed list/var yields the element type.
		if id, ok := e.Object.(*parser.Ident); ok {
			if t, ok := p.elemType[id.Name]; ok {
				return t
			}
		}
		return tUnknown
	case *parser.TypeAssertExpr:
		// Dict reads lower to `any(d.Get(k)).(T)` — the asserted type is the
		// value type.
		if e.TypeExpr != nil {
			return typeFromExpr(e.TypeExpr)
		}
		return tUnknown
	case *parser.SelectorExpr:
		// self.field inside a method, or obj.field where obj is a tracked
		// instance — resolve the field's type from the class registry.
		if cls := p.exprClass(e.Object); cls != "" {
			if ft, ok := p.classFields[cls]; ok {
				if t, ok := ft[e.Field]; ok {
					return t
				}
			}
		}
		return tUnknown
	case *parser.IfExpr:
		// Ternary: type is the common branch type, else unknown.
		if t := p.typeOf(e.Then); t == p.typeOf(e.Else) {
			return t
		}
		return tUnknown
	case *parser.UnaryExpr:
		if e.Op == "!" {
			return tBool
		}
		return p.typeOf(e.Operand)
	case *parser.CallExpr:
		return p.callType(e)
	case *parser.BinaryExpr:
		switch e.Op {
		case "==", "!=", "<", ">", "<=", ">=", "&&", "||":
			return tBool
		case "/":
			return tFloat // true division always yields float
		default: // + - * %
			lt, rt := p.typeOf(e.Left), p.typeOf(e.Right)
			if lt == tFloat || rt == tFloat {
				return tFloat
			}
			if lt == tStr && rt == tStr {
				return tStr // string concat / repeat
			}
			if lt == tInt && rt == tInt {
				return tInt
			}
			return tUnknown
		}
	}
	return tUnknown
}

// exprClass returns the class an expression evaluates to an instance of:
// `self` → the enclosing class; a tracked instance var → its class; a
// constructor call `C(...)` → C; a method call → the method's return class
// (so operator-dunder results chain). "" if not a known instance.
func (p *Parser) exprClass(e parser.Expr) string {
	switch o := e.(type) {
	case *parser.ThisExpr:
		return p.currentClass
	case *parser.Ident:
		return p.instanceClass[o.Name]
	case *parser.CallExpr:
		if sel, ok := o.Callee.(*parser.SelectorExpr); ok {
			if cls := p.exprClass(sel.Object); cls != "" {
				if rc, ok := p.classMethodRet[cls]; ok {
					return rc[sel.Field]
				}
			}
		}
		if id, ok := o.Callee.(*parser.Ident); ok && p.classNames[id.Name] {
			return id.Name // constructor
		}
	}
	return ""
}

// classHasMethod reports whether class cls defines (Go) method m.
func (p *Parser) classHasMethod(cls, m string) bool {
	mm, ok := p.classMethods[cls]
	if !ok {
		return false
	}
	_, ok = mm[m]
	return ok
}

// callType infers the return type of a call: builtin conversions and known
// user functions have determinate types; everything else is unknown.
func (p *Parser) callType(c *parser.CallExpr) pytype {
	// Method call obj.method() / self.method(): look up the method's declared
	// return type on the receiver's class.
	if sel, ok := c.Callee.(*parser.SelectorExpr); ok {
		if cls := p.exprClass(sel.Object); cls != "" {
			if mm, ok := p.classMethods[cls]; ok {
				if t, ok := mm[sel.Field]; ok {
					return t
				}
			}
		}
		return tUnknown
	}
	id, ok := c.Callee.(*parser.Ident)
	if !ok {
		return tUnknown
	}
	switch id.Name {
	case "float", "double", rtDiv:
		return tFloat
	case "int", "len", rtFloorDiv, rtMod:
		return tInt
	case "str", "zincpyStr", "zincpyRepr", "zincpyUpper", "zincpyLower",
		"zincpyTitle", "zincpyCapitalize", "zincpyStrip", "zincpyLstrip",
		"zincpyRstrip", "zincpyReplace", "zincpyJoin", "zincpyLjust",
		"zincpyRjust", "zincpyCenter":
		return tStr
	case "zincpyStartswith", "zincpyEndswith", "zincpyTruthy", "zincpyIn",
		"zincpyEq", "zincpyNe", "zincpyLt", "zincpyGt", "zincpyLe", "zincpyGe",
		"zincpyIsInstance", "zincpyAny", "zincpyAll",
		"zincpyIsalpha", "zincpyIsdigit", "zincpyIsalnum", "zincpyIsspace",
		"zincpyIsupper", "zincpyIslower", "zincpyIstitle":
		return tBool
	case "zincpyFind", "zincpyCount", "zincpyLen", "zincpyToInt", "zincpyParseInt", "zincpyRound",
		"zincpyListIndex", "zincpyListCount":
		return tInt
	case "zincpyToFloat", "zincpyTrueDiv", "zincpyParseFloat", "zincpyRoundN":
		return tFloat
	// FFI results and anything derived from a dynamic value stay dynamic, so
	// chained access (data[0][1], iterating a parsed list, arithmetic on a
	// dynamic) keeps routing through the dynamic helpers.
	case "zincpyPyCall", "zincpyPyGet", "zincpyGetItem", "zincpyNewTuple",
		"zincpyAdd", "zincpySub", "zincpyMul", "zincpyPow", "zincpyFloorDivDyn", "zincpyModDyn", "zincpyNeg",
		"zincpyMin", "zincpyMax", "zincpySum", "zincpySorted", "zincpyAbs",
		"zincpySlice", "zincpyEnumerate", "zincpyZip",
		"zincpySortedKey", "zincpyMap", "zincpyFilter", "zincpyList",
		"zincpyStrMethod", "zincpyCallDynamic":
		return tDynamic
	}
	if t, ok := p.fnRet[id.Name]; ok {
		return t
	}
	if p.lambdaVars[id.Name] {
		return tDynamic // a lambda returns interface{}
	}
	return tUnknown
}

// elemTypeOf infers the element type produced by iterating an expression:
// range() yields ints, a list literal yields its element type, a tracked
// list variable yields its recorded element type.
func (p *Parser) elemTypeOf(iter parser.Expr) pytype {
	if _, ok := asRange(iter); ok {
		return tInt
	}
	switch it := iter.(type) {
	case *parser.RangeExpr:
		return tInt
	case *parser.ListLit:
		if len(it.Elements) > 0 {
			return p.typeOf(it.Elements[0])
		}
	case *parser.CallExpr:
		// A 3-arg range materialized to zincpyRangeList([]int) iterates ints.
		if id, ok := it.Callee.(*parser.Ident); ok && id.Name == "zincpyRangeList" {
			return tInt
		}
	case *parser.Ident:
		if t, ok := p.elemType[it.Name]; ok {
			return t
		}
	}
	return tUnknown
}

// recordParamElem records the element type of a `list[T]` parameter, and the
// key/value types of a `dict[K,V]` parameter. The dict's parsed type is
// SimpleType "*zincpyDict" (key/value erased), so the key/value types are
// recovered from the unparsed hint via recordParamDict at the call site below.
func (p *Parser) recordParamElem(pa *parser.ParamDecl) {
	gt, ok := pa.Type.(*parser.GenericType)
	if !ok {
		return
	}
	switch gt.Name {
	case "List":
		if len(gt.TypeArgs) == 1 {
			p.elemType[pa.Name] = typeFromExpr(gt.TypeArgs[0])
		}
	case "PyDict":
		dm := dictMeta{key: tUnknown, val: tUnknown}
		if len(gt.TypeArgs) == 2 {
			dm = dictMeta{key: typeFromExpr(gt.TypeArgs[0]), val: typeFromExpr(gt.TypeArgs[1])}
		}
		p.dictVars[pa.Name] = dm
	}
}

// recordElemType remembers the element type of a list-valued variable, so
// later iteration/indexing infers element types (drives numeric promotion and
// comprehension result typing).
func (p *Parser) recordElemType(name string, value parser.Expr) {
	if lit, ok := value.(*parser.ListLit); ok && len(lit.Elements) > 0 {
		p.elemType[name] = p.typeOf(lit.Elements[0])
	}
	// str.split(...) → []string, so iterating the result yields strings.
	if call, ok := value.(*parser.CallExpr); ok {
		if id, ok := call.Callee.(*parser.Ident); ok {
			switch id.Name {
			case "zincpySplit":
				p.elemType[name] = tStr
			case "zincpyRangeList": // r = range(n) → []int
				p.elemType[name] = tInt
			}
		}
	}
}

// dynBinaryHelper maps a binary operator to the runtime helper that performs
// it on dynamic operands, or "" if the operator isn't routed dynamically.
func dynBinaryHelper(op string) string {
	switch op {
	case "+":
		return "zincpyAdd"
	case "-":
		return "zincpySub"
	case "*":
		return "zincpyMul"
	case "/":
		return "zincpyTrueDiv"
	case "//":
		return "zincpyFloorDivDyn"
	case "%":
		return "zincpyModDyn"
	case "==":
		return "zincpyEq"
	case "!=":
		return "zincpyNe"
	case "<":
		return "zincpyLt"
	case ">":
		return "zincpyGt"
	case "<=":
		return "zincpyLe"
	case ">=":
		return "zincpyGe"
	}
	return ""
}

// opDunder maps a binary operator to the Go method name a class can define to
// overload it (Python's __add__/__eq__/… → the front-end's renamed methods).
var opDunder = map[string]string{
	"+":  "Add",
	"-":  "Sub",
	"*":  "Mul",
	"==": "Eq",
	"!=": "Ne",
	"<":  "Lt",
	"<=": "Le",
	">":  "Gt",
	">=": "Ge",
}

// numericBinary builds a binary expression, inserting a float() conversion on
// whichever operand is int when the other is float — Python's implicit
// int→float promotion, made explicit for Go.
// isListishExpr conservatively reports whether e is statically known to be a
// list: a list literal or a variable with a tracked element type.
func (p *Parser) isListishExpr(e parser.Expr) bool {
	switch e := e.(type) {
	case *parser.ListLit:
		return true
	case *parser.Ident:
		_, ok := p.elemType[e.Name]
		return ok
	}
	return false
}

func (p *Parser) numericBinary(left parser.Expr, op string, right parser.Expr) parser.Expr {
	lt, rt := p.typeOf(left), p.typeOf(right)
	// Operator overloading: if the left operand is a class instance whose class
	// defines the dunder for this operator, route `a op b` → `a.Dunder(b)`.
	if m, ok := opDunder[op]; ok {
		if cls := p.exprClass(left); cls != "" && p.classHasMethod(cls, m) {
			return &parser.CallExpr{
				Callee: &parser.SelectorExpr{Object: left, Field: m},
				Args:   []parser.Expr{right},
			}
		}
	}
	// A dynamic operand (FFI result, tuple element, ...) can't pick a Go
	// operator statically — route to the runtime dispatcher.
	if lt == tDynamic || rt == tDynamic {
		if h := dynBinaryHelper(op); h != "" {
			return callIdent(h, left, right)
		}
	}
	// Sequence repetition `seq * n` / `n * seq` (str*int or list*int): Go has no
	// `*` for these, so route to zincpyMul (which repeats). The result is
	// dynamic, which is fine for the usual `print("=" * 40)` / `[0] * n` uses.
	if op == "*" {
		lSeq, rSeq := lt == tStr || p.isListishExpr(left), rt == tStr || p.isListishExpr(right)
		if (lSeq && rt == tInt) || (lt == tInt && rSeq) {
			return callIdent("zincpyMul", left, right)
		}
	}
	if lt == tInt && rt == tFloat {
		left = floatWrap(left)
	} else if lt == tFloat && rt == tInt {
		right = floatWrap(right)
	}
	return &parser.BinaryExpr{Left: left, Op: op, Right: right}
}
