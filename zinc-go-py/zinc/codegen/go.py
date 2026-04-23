"""Zinc AST → Go source emitter.

First-cut implementation. Covers the constructs exercised by the hello-world
path; extends outward. Designed so adding a new node type is a single `case`
clause in the relevant dispatcher.
"""
from __future__ import annotations

from dataclasses import dataclass, field

from zinc import ast


# --- Type mapping ------------------------------------------------------------

# Zinc primitive / builtin type → Go type. Anything not in this map is treated
# as a user-defined type and passed through as-is (first-letter capitalised).
_TYPE_MAP = {
    "int": "int",
    "long": "int64",
    "float": "float32",
    "double": "float64",
    "bool": "bool",
    "boolean": "bool",
    "byte": "byte",
    "char": "rune",
    "string": "string",
    "String": "string",
    "void": "",
}


def _go_type(t: ast.TypeExpr | None) -> str:
    if t is None:
        return ""
    match t:
        case ast.SimpleType(name=n):
            return _TYPE_MAP.get(n, n)
        case ast.GenericType(name="List", type_args=[elem]):
            return f"[]{_go_type(elem)}"
        case ast.GenericType(name="Map", type_args=[k, v]):
            return f"map[{_go_type(k)}]{_go_type(v)}"
        case ast.GenericType(name="Chan", type_args=[e]):
            return f"chan {_go_type(e)}"
        case ast.GenericType(name=n, type_args=args):
            return f"{n}[{', '.join(_go_type(a) for a in args)}]"
        case ast.ArrayType(element_type=elem):
            return f"[]{_go_type(elem)}"
        case ast.OptionalType(inner=inner):
            return f"*{_go_type(inner)}"
        case ast.FuncTypeExpr(params=ps, return_type=r):
            plist = ", ".join(_go_type(p) for p in ps)
            rstr = _go_type(r)
            return f"func({plist})" + (f" {rstr}" if rstr else "")
    return ""


# --- Emitter -----------------------------------------------------------------

@dataclass
class Emitter:
    """Walks a Program and emits Go source as a string."""
    imports: set[str] = field(default_factory=set)
    _out: list[str] = field(default_factory=list)
    _indent: int = 0

    # -- emit primitives -----------------------------------------------------

    def _write(self, s: str) -> None:
        self._out.append(s)

    def _writeln(self, s: str = "") -> None:
        if s:
            self._out.append("\t" * self._indent + s + "\n")
        else:
            self._out.append("\n")

    def _need(self, pkg: str) -> None:
        self.imports.add(pkg)

    # -- top level -----------------------------------------------------------

    def emit_program(self, prog: ast.Program) -> str:
        # Body first, so imports set gets populated via side effects.
        body: list[str] = []
        with _collect(self, body):
            # Type declarations come before top-level functions in Go.
            for d in prog.decls:
                self._emit_decl(d)
                self._writeln()

            if prog.stmts:
                self._writeln("func main() {")
                self._indent += 1
                for s in prog.stmts:
                    self._emit_stmt(s)
                self._indent -= 1
                self._writeln("}")

        header: list[str] = []
        header.append("package main\n")
        header.append("\n")
        if self.imports:
            header.append("import (\n")
            for pkg in sorted(self.imports):
                header.append(f'\t"{pkg}"\n')
            header.append(")\n")
            header.append("\n")
        return "".join(header) + "".join(body)

    # -- decl dispatch -------------------------------------------------------

    def _emit_decl(self, d) -> None:
        match d:
            case ast.FnDecl():
                self._emit_fn(d)
            case ast.ConstDecl():
                self._emit_const(d)
            case ast.ClassDecl():
                self._emit_class(d)
            case ast.DataClassDecl():
                self._emit_data_class(d)
            case ast.InterfaceDecl():
                self._emit_interface(d)
            case ast.EnumDecl():
                self._emit_enum(d)
            case ast.TypeAliasDecl():
                self._writeln(f"type {d.name} = {_go_type(d.type)}")
            case _:
                raise NotImplementedError(f"decl: {type(d).__name__}")

    def _emit_fn(self, fn: ast.FnDecl) -> None:
        params = ", ".join(_fmt_param(p) for p in fn.params)
        ret = _go_type(fn.return_type)
        sig = f"func {fn.name}({params})" + (f" {ret}" if ret else "")
        if fn.body is not None:
            self._writeln(f"{sig} {{")
            self._indent += 1
            for s in fn.body.stmts:
                self._emit_stmt(s)
            self._indent -= 1
            self._writeln("}")
        elif fn.expr_body is not None:
            self._writeln(f"{sig} {{")
            self._indent += 1
            self._writeln(f"return {self._fmt_expr(fn.expr_body)}")
            self._indent -= 1
            self._writeln("}")

    def _emit_const(self, c: ast.ConstDecl) -> None:
        typ = _go_type(c.type)
        val = self._fmt_expr(c.value)
        if typ:
            self._writeln(f"const {c.name} {typ} = {val}")
        else:
            self._writeln(f"const {c.name} = {val}")

    def _emit_class(self, c: ast.ClassDecl) -> None:
        # Struct for fields; methods become receiver functions.
        self._writeln(f"type {c.name} struct {{")
        self._indent += 1
        for parent in c.parents:
            # Embed parent for inheritance-ish composition.
            self._writeln(parent)
        for f in c.fields:
            self._writeln(f"{f.name} {_go_type(f.type)}")
        self._indent -= 1
        self._writeln("}")
        self._writeln()
        # Constructor via New<ClassName>
        if c.ctor is not None:
            ps = ", ".join(_fmt_param(p) for p in c.ctor.params)
            self._writeln(f"func New{c.name}({ps}) *{c.name} {{")
            self._indent += 1
            self._writeln(f"var this = &{c.name}{{}}")
            for s in c.ctor.body.stmts:
                self._emit_stmt(s)
            self._writeln("return this")
            self._indent -= 1
            self._writeln("}")
            self._writeln()
        for m in c.methods:
            params = ", ".join(_fmt_param(p) for p in m.params)
            ret = _go_type(m.return_type)
            sig = f"func (this *{c.name}) {m.name}({params})" + (f" {ret}" if ret else "")
            if m.body is not None:
                self._writeln(f"{sig} {{")
                self._indent += 1
                for s in m.body.stmts:
                    self._emit_stmt(s)
                self._indent -= 1
                self._writeln("}")
            self._writeln()

    def _emit_data_class(self, d: ast.DataClassDecl) -> None:
        self._writeln(f"type {d.name} struct {{")
        self._indent += 1
        for p in d.params:
            self._writeln(f"{p.name} {_go_type(p.type)}")
        self._indent -= 1
        self._writeln("}")
        self._writeln()
        # Auto-generate a constructor and a String() method.
        ps = ", ".join(_fmt_param(p) for p in d.params)
        assigns = ", ".join(f"{p.name}: {p.name}" for p in d.params)
        self._writeln(f"func New{d.name}({ps}) {d.name} {{")
        self._indent += 1
        self._writeln(f"return {d.name}{{{assigns}}}")
        self._indent -= 1
        self._writeln("}")

    def _emit_interface(self, i: ast.InterfaceDecl) -> None:
        self._writeln(f"type {i.name} interface {{")
        self._indent += 1
        for m in i.methods:
            params = ", ".join(_fmt_param(p) for p in m.params)
            ret = _go_type(m.return_type)
            self._writeln(f"{m.name}({params})" + (f" {ret}" if ret else ""))
        self._indent -= 1
        self._writeln("}")

    def _emit_enum(self, e: ast.EnumDecl) -> None:
        self._writeln(f"type {e.name} int")
        self._writeln("const (")
        self._indent += 1
        for i, v in enumerate(e.variants):
            if i == 0:
                self._writeln(f"{v} {e.name} = iota")
            else:
                self._writeln(v)
        self._indent -= 1
        self._writeln(")")

    # -- statement dispatch --------------------------------------------------

    def _emit_stmt(self, s) -> None:
        match s:
            case ast.VarStmt():
                self._emit_var(s)
            case ast.ReturnStmt(value=v):
                self._writeln("return" + (f" {self._fmt_expr(v)}" if v is not None else ""))
            case ast.PrintStmt(value=v):
                self._emit_print(v)
            case ast.ExprStmt(expr=e):
                self._writeln(self._fmt_expr(e))
            case ast.AssignStmt(target=t, op=op, value=v):
                self._writeln(f"{self._fmt_expr(t)} {op} {self._fmt_expr(v)}")
            case ast.IfStmt():
                self._emit_if(s)
            case ast.ForStmt():
                self._emit_for(s)
            case ast.WhileStmt(cond=c, body=b):
                self._writeln(f"for {self._fmt_expr(c)} {{")
                self._indent += 1
                for bs in b.stmts:
                    self._emit_stmt(bs)
                self._indent -= 1
                self._writeln("}")
            case ast.BreakStmt():
                self._writeln("break")
            case ast.ContinueStmt():
                self._writeln("continue")
            case ast.BlockStmt():
                self._writeln("{")
                self._indent += 1
                for bs in s.stmts:
                    self._emit_stmt(bs)
                self._indent -= 1
                self._writeln("}")
            case ast.FnDecl():
                # Nested function → closure assignment.
                self._writeln(f"{s.name} := " + self._fmt_fn_lit(s))
            case _:
                raise NotImplementedError(f"stmt: {type(s).__name__}")

    def _emit_var(self, v: ast.VarStmt) -> None:
        if v.value is None:
            self._writeln(f"var {v.name} {_go_type(v.type)}")
        elif v.type is None:
            self._writeln(f"{v.name} := {self._fmt_expr(v.value)}")
        else:
            self._writeln(f"var {v.name} {_go_type(v.type)} = {self._fmt_expr(v.value)}")

    def _emit_print(self, value) -> None:
        self._need("fmt")
        if isinstance(value, ast.StringInterpLit):
            fmt_str, args = _build_format(value)
            if args:
                self._writeln(f'fmt.Printf("{fmt_str}\\n", {", ".join(args)})')
            else:
                self._writeln(f'fmt.Println("{fmt_str}")')
        else:
            self._writeln(f"fmt.Println({self._fmt_expr(value)})")

    def _emit_if(self, s: ast.IfStmt) -> None:
        self._writeln(f"if {self._fmt_expr(s.cond)} {{")
        self._indent += 1
        for bs in s.then.stmts:
            self._emit_stmt(bs)
        self._indent -= 1
        if s.else_ is None:
            self._writeln("}")
        else:
            if isinstance(s.else_, ast.IfStmt):
                # chain: } else if ... {
                self._out[-1] = self._out[-1].rstrip("\n")
                self._write(" else ")
                # Emit else-if inline
                cond = self._fmt_expr(s.else_.cond)
                self._write(f"if {cond} {{\n")
                self._indent += 1
                for bs in s.else_.then.stmts:
                    self._emit_stmt(bs)
                self._indent -= 1
                if s.else_.else_ is None:
                    self._writeln("}")
                else:
                    # Recurse by re-synthesising an IfStmt with just the trailing else.
                    self._emit_stmt(ast.IfStmt(cond=ast.BoolLit(True),
                                               then=s.else_.else_ if isinstance(s.else_.else_, ast.BlockStmt)
                                               else ast.BlockStmt(stmts=[s.else_.else_]),
                                               else_=None))
            else:
                self._out[-1] = self._out[-1].rstrip("\n")
                self._write(" else {\n")
                self._indent += 1
                for bs in s.else_.stmts:
                    self._emit_stmt(bs)
                self._indent -= 1
                self._writeln("}")

    def _emit_for(self, s: ast.ForStmt) -> None:
        if s.is_range:
            idx = s.index_var or "_"
            rng = self._fmt_expr(s.range_expr)
            # Range expression may be a RangeExpr → translate to C-style.
            if isinstance(s.range_expr, ast.RangeExpr):
                start = self._fmt_expr(s.range_expr.start)
                end = self._fmt_expr(s.range_expr.end)
                cmp = "<=" if s.range_expr.inclusive else "<"
                self._writeln(f"for {s.item} := {start}; {s.item} {cmp} {end}; {s.item}++ {{")
            else:
                self._writeln(f"for {idx}, {s.item} := range {rng} {{")
            self._indent += 1
            for bs in s.body.stmts:
                self._emit_stmt(bs)
            self._indent -= 1
            self._writeln("}")
        else:
            # C-style for
            init = self._fmt_stmt_inline(s.init) if s.init else ""
            cond = self._fmt_expr(s.cond) if s.cond else ""
            post = self._fmt_stmt_inline(s.post) if s.post else ""
            self._writeln(f"for {init}; {cond}; {post} {{")
            self._indent += 1
            for bs in s.body.stmts:
                self._emit_stmt(bs)
            self._indent -= 1
            self._writeln("}")

    def _fmt_stmt_inline(self, s) -> str:
        match s:
            case ast.VarStmt(name=n, value=v, type=None):
                return f"{n} := {self._fmt_expr(v)}"
            case ast.VarStmt(name=n, value=v, type=t):
                return f"var {n} {_go_type(t)} = {self._fmt_expr(v)}"
            case ast.AssignStmt(target=t, op=op, value=v):
                return f"{self._fmt_expr(t)} {op} {self._fmt_expr(v)}"
            case ast.ExprStmt(expr=e):
                return self._fmt_expr(e)
            case ast.ReturnStmt(value=v):
                return "return" + (f" {self._fmt_expr(v)}" if v is not None else "")
            case _:
                raise NotImplementedError(f"inline stmt: {type(s).__name__}")

    # -- expression formatting -----------------------------------------------

    def _fmt_expr(self, e) -> str:
        match e:
            case ast.IntLit(value=v):
                return v
            case ast.FloatLit(value=v):
                return v
            case ast.StringLit(value=v):
                return _go_string(v)
            case ast.StringInterpLit():
                fmt_str, args = _build_format(e)
                self._need("fmt")
                return f'fmt.Sprintf("{fmt_str}", {", ".join(args)})' if args else _go_string(fmt_str)
            case ast.RawStringLit(value=v):
                return "`" + v + "`"
            case ast.BoolLit(value=v):
                return "true" if v else "false"
            case ast.NullLit():
                return "nil"
            case ast.Ident(name=n):
                return n
            case ast.ThisExpr():
                return "this"
            case ast.BinaryExpr(left=l, op=op, right=r):
                return f"({self._fmt_expr(l)} {op} {self._fmt_expr(r)})"
            case ast.UnaryExpr(op=op, operand=o):
                return f"{op}{self._fmt_expr(o)}"
            case ast.CallExpr():
                return self._fmt_call(e)
            case ast.SelectorExpr(object=o, field=f):
                return f"{self._fmt_expr(o)}.{f}"
            case ast.IndexExpr(object=o, index=i):
                return f"{self._fmt_expr(o)}[{self._fmt_expr(i)}]"
            case ast.ListLit(elements=els, explicit_type=et):
                inner = ", ".join(self._fmt_expr(x) for x in els)
                if et is not None:
                    return f"{_go_type(et)}{{{inner}}}"
                return f"[]interface{{}}{{{inner}}}"
            case ast.MapLit(keys=ks, values=vs, explicit_type=et):
                entries = ", ".join(f"{self._fmt_expr(k)}: {self._fmt_expr(v)}"
                                    for k, v in zip(ks, vs))
                if et is not None:
                    return f"{_go_type(et)}{{{entries}}}"
                return f"map[string]interface{{}}{{{entries}}}"
            case ast.LambdaExpr():
                return self._fmt_lambda(e)
            case ast.TupleLit(elements=els):
                return "(" + ", ".join(self._fmt_expr(x) for x in els) + ")"
            case ast.SpreadExpr(expr=inner):
                return f"{self._fmt_expr(inner)}..."
            case ast.RangeExpr(start=s, end=end, inclusive=inc):
                cmp = "<=" if inc else "<"
                return f"/* range {self._fmt_expr(s)} {cmp} {self._fmt_expr(end)} */"
            case ast.SliceExpr(object=o, low=l, high=h):
                lo = self._fmt_expr(l) if l is not None else ""
                hi = self._fmt_expr(h) if h is not None else ""
                return f"{self._fmt_expr(o)}[{lo}:{hi}]"
            case ast.SafeNavExpr(object=o, field=f):
                return f"{self._fmt_expr(o)}.{f}"   # naive — needs nil-check pass
            case ast.PropagateExpr(inner=i):
                return self._fmt_expr(i)             # naive — needs error-return rewrite
            case ast.TypeAssertExpr(object=o, type_name=tn, is_check=chk):
                go_t = _TYPE_MAP.get(tn, tn)
                if chk:
                    return f"(func() bool {{ _, ok := {self._fmt_expr(o)}.({go_t}); return ok }}())"
                return f"{self._fmt_expr(o)}.({go_t})"
            case ast.IfExpr(cond=c, then=t, else_=el):
                return (f"(func() interface{{}} {{ if {self._fmt_expr(c)} {{ "
                        f"return {self._fmt_expr(t)} }}; return {self._fmt_expr(el)} }}())")
            case _:
                raise NotImplementedError(f"expr: {type(e).__name__}")

    def _fmt_call(self, c: ast.CallExpr) -> str:
        callee = self._fmt_expr(c.callee)
        args_fmt = [self._fmt_expr(a) for a in c.args]
        # Struct-field args → struct literal: T{Field: value, ...}
        if c.struct_field_args:
            fields = ", ".join(f"{s.name}: {self._fmt_expr(s.value)}" for s in c.struct_field_args)
            return f"{callee}{{{fields}}}"
        named_fmt = [f"{n.name}: {self._fmt_expr(n.value)}" for n in c.named_args]
        return f"{callee}({', '.join(args_fmt + named_fmt)})"

    def _fmt_fn_lit(self, fn: ast.FnDecl) -> str:
        params = ", ".join(_fmt_param(p) for p in fn.params)
        ret = _go_type(fn.return_type)
        head = f"func({params})" + (f" {ret}" if ret else "")
        lines = [f"{head} {{"]
        if fn.body:
            for s in fn.body.stmts:
                lines.append("\t" + self._fmt_stmt_inline(s))
        lines.append("}")
        return "\n".join(lines)

    def _fmt_lambda(self, lam: ast.LambdaExpr) -> str:
        params = ", ".join(_fmt_param(p) for p in lam.params)
        ret = _go_type(lam.return_type)
        head = f"func({params})" + (f" {ret}" if ret else "")
        if lam.body is not None:
            # Build body inline.
            saved_indent = self._indent
            # Capture into a local emitter for clean indentation.
            sub = Emitter(imports=self.imports, _indent=self._indent + 1)
            for s in lam.body.stmts:
                sub._emit_stmt(s)
            body = "".join(sub._out)
            self._indent = saved_indent
            return f"{head} {{\n{body}{chr(9) * self._indent}}}"
        if lam.expr is not None:
            return f"{head} {{ return {self._fmt_expr(lam.expr)} }}"
        return f"{head} {{}}"


# --- helpers -----------------------------------------------------------------

class _collect:
    """Context manager that redirects `_out` writes to an external buffer."""
    def __init__(self, emitter: Emitter, target: list[str]):
        self.e = emitter; self.target = target; self._saved = None
    def __enter__(self):
        self._saved = self.e._out
        self.e._out = self.target
        return self
    def __exit__(self, *exc):
        self.e._out = self._saved


def _fmt_param(p) -> str:
    # Accepts either ParamDecl (regular params) or FieldDecl (data-class
    # constructor params, which reuse the same call shape).
    typ = _go_type(p.type) if p.type is not None else "interface{}"
    if getattr(p, "variadic", False):
        typ = f"...{typ}" if not typ.startswith("...") else typ
    return f"{p.name} {typ}" if typ else p.name


def _go_string(s: str) -> str:
    """Emit `s` as a Go double-quoted string with appropriate escapes."""
    return '"' + s.replace("\\", "\\\\").replace('"', '\\"').replace("\n", "\\n").replace("\t", "\\t") + '"'


def _build_format(sil: ast.StringInterpLit) -> tuple[str, list[str]]:
    """Given a StringInterpLit, return (go_format_string, [arg_exprs]).
    The format string uses %v for every placeholder (Go's generic verb)."""
    from zinc.codegen.go import Emitter  # local fmt call
    e = Emitter()
    parts: list[str] = []
    args: list[str] = []
    for p in sil.parts:
        if isinstance(p, ast.StringLit):
            parts.append(p.value.replace("\\", "\\\\").replace('"', '\\"')
                         .replace("\n", "\\n").replace("\t", "\\t").replace("%", "%%"))
        else:
            parts.append("%v")
            args.append(e._fmt_expr(p))
    return "".join(parts), args


def emit(prog: ast.Program) -> tuple[str, set[str]]:
    """Top-level entry: returns (go_source, imports_set)."""
    em = Emitter()
    source = em.emit_program(prog)
    return source, em.imports
