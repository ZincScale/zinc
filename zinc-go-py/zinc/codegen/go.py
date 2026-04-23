"""Zinc AST → Go source emitter.

Pattern-driven: one place per class of bug, not per-failing-test.
"""
from __future__ import annotations

from dataclasses import dataclass, field
from typing import Optional

from zinc import ast


# ============================================================================
# Type translation
# ============================================================================

# Zinc primitive/builtin name → Go type.
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
    "Object": "interface{}",
    "Any": "interface{}",
}

# Type names that, when used as a call, are casts: `long(x)` → `int64(x)`.
_CAST_NAMES = set(_TYPE_MAP.keys())


def _go_type(t: ast.TypeExpr | None, emitter=None) -> str:
    if t is None:
        return ""
    match t:
        case ast.SimpleType(name=n):
            if n in _TYPE_MAP:
                return _TYPE_MAP[n]
            # Mirrors zinc-go's codegen_resolve.go line 881 — non-data,
            # non-sealed, non-interface classes are always pointer-typed in
            # the emitted Go. Data classes stay value-typed; sealed parents
            # and plain interfaces emit as bare Go interfaces.
            if emitter is not None and n in emitter.user_classes:
                info = emitter.user_classes[n]
                if (not info.get("is_data") and not info.get("is_sealed")
                        and not info.get("is_interface")):
                    return f"*{n}"
            return n
        case ast.GenericType(name="List", type_args=[elem]):
            return f"[]{_go_type(elem, emitter)}"
        case ast.GenericType(name="Map", type_args=[k, v]):
            return f"map[{_go_type(k, emitter)}]{_go_type(v, emitter)}"
        case ast.GenericType(name="Chan" | "Channel", type_args=[e]):
            return f"chan {_go_type(e, emitter)}"
        case ast.GenericType(name=n, type_args=args):
            ptr = ""
            if emitter is not None and n in emitter.user_classes:
                info = emitter.user_classes[n]
                if not info.get("is_data") and not info.get("is_sealed"):
                    ptr = "*"
            return f"{ptr}{n}[{', '.join(_go_type(a, emitter) for a in args)}]"
        case ast.ArrayType(element_type=elem):
            return f"[]{_go_type(elem, emitter)}"
        case ast.OptionalType(inner=inner):
            return f"*{_go_type(inner, emitter)}"
        case ast.FuncTypeExpr(params=ps, return_type=r):
            plist = ", ".join(_go_type(p, emitter) for p in ps)
            rstr = _go_type(r, emitter)
            return f"func({plist})" + (f" {rstr}" if rstr else "")
    return ""


# ============================================================================
# Builtin pseudo-method dispatch (Zinc's OO surface → Go's functional surface)
# ============================================================================
#
# Emits Go for `recv.method(args)`. `recv_str` is the already-formatted
# receiver, `args` is a list of already-formatted arg strings.
#
# Keyed by bare method name. For ambiguous methods that behave differently
# depending on receiver type (.length vs .length(), .remove vs .remove(k)),
# we pick the semantics that works for all Go built-in containers.
#
# Streams are intentionally absent — Zinc expects explicit loops.

def _strings_import(emit: "Emitter") -> str:
    emit._need("strings"); return "strings"

def _slices_import(emit: "Emitter") -> str:
    emit._need("slices"); return "slices"

# Builtin rewrites. Each entry: name → (arity, callable)
# arity is None for variadic, or an int for fixed. When the call site's arg
# count doesn't match, the dispatcher falls through to a regular method call
# (so user-defined methods named `get`, `remove`, etc. still work).
_BUILTIN_METHODS: dict[str, tuple[int | None, object]] = {
    "length":      (0, lambda e, r, a: f"len({r})"),
    "size":        (0, lambda e, r, a: f"len({r})"),
    "capacity":    (0, lambda e, r, a: f"cap({r})"),
    "isEmpty":     (0, lambda e, r, a: f"(len({r}) == 0)"),
    "nonEmpty":    (0, lambda e, r, a: f"(len({r}) > 0)"),

    "add":         (None, lambda e, r, a: f"{r} = append({r}, {', '.join(a)})"),
    "append":      (None, lambda e, r, a: f"{r} = append({r}, {', '.join(a)})"),
    "clear":       (0, lambda e, r, a: f"{r} = {r}[:0]"),
    "sort":        (0, lambda e, r, a: f"{_slices_import(e)}.Sort({r})"),
    "reversed":    (0, lambda e, r, a: f"{_slices_import(e)}.Reverse({r})"),
    # `x.contains(y)` — slices.Contains for []T, strings.Contains for strings.
    # Heuristic: if the arg is a string literal, treat as substring check.
    # For mixed cases, user can call strings.Contains / slices.Contains directly.
    "contains":    (1, lambda e, r, a: (
        f"{_strings_import(e)}.Contains({r}, {a[0]})" if a[0].startswith('"')
        else f"{_slices_import(e)}.Contains({r}, {a[0]})"
    )),
    "indexOf":     (1, lambda e, r, a: (
        f"{_strings_import(e)}.Index({r}, {a[0]})" if a[0].startswith('"')
        else f"{_slices_import(e)}.Index({r}, {a[0]})"
    )),
    "first":       (0, lambda e, r, a: f"{r}[0]"),
    "last":        (0, lambda e, r, a: f"{r}[len({r})-1]"),

    "put":         (2, lambda e, r, a: f"{r}[{a[0]}] = {a[1]}"),
    "get":         (1, lambda e, r, a: f"{r}[{a[0]}]"),
    "containsKey": (1, lambda e, r, a: f"(func() bool {{ _, ok := {r}[{a[0]}]; return ok }}())"),
    "keys":        (0, lambda e, r, a: f"(func() []string {{ ks := make([]string, 0, len({r})); for k := range {r} {{ ks = append(ks, k) }}; return ks }}())"),
    "values":      (0, lambda e, r, a: f"(func() []interface{{}} {{ vs := make([]interface{{}}, 0, len({r})); for _, v := range {r} {{ vs = append(vs, v) }}; return vs }}())"),
    "remove":      (1, lambda e, r, a: f"delete({r}, {a[0]})"),

    "upper":       (0, lambda e, r, a: f"{_strings_import(e)}.ToUpper({r})"),
    "lower":       (0, lambda e, r, a: f"{_strings_import(e)}.ToLower({r})"),
    "trim":        (0, lambda e, r, a: f"{_strings_import(e)}.TrimSpace({r})"),
    "trimStart":   (0, lambda e, r, a: f"{_strings_import(e)}.TrimLeft({r}, \" \\t\\n\\r\")"),
    "trimEnd":     (0, lambda e, r, a: f"{_strings_import(e)}.TrimRight({r}, \" \\t\\n\\r\")"),
    "split":       (1, lambda e, r, a: f"{_strings_import(e)}.Split({r}, {a[0]})"),
    "replace":     (2, lambda e, r, a: f"{_strings_import(e)}.ReplaceAll({r}, {a[0]}, {a[1]})"),
    "startsWith":  (1, lambda e, r, a: f"{_strings_import(e)}.HasPrefix({r}, {a[0]})"),
    "endsWith":    (1, lambda e, r, a: f"{_strings_import(e)}.HasSuffix({r}, {a[0]})"),
    "join":        (1, lambda e, r, a: f"{_strings_import(e)}.Join({r}, {a[0]})"),
    "charAt":      (1, lambda e, r, a: f"string([]byte({r})[{a[0]}])"),
    "substring":   (2, lambda e, r, a: f"{r}[{a[0]}:{a[1]}]"),
    "repeat":      (1, lambda e, r, a: f"{_strings_import(e)}.Repeat({r}, {a[0]})"),
    "toBytes":     (0, lambda e, r, a: f"[]byte({r})"),

    "send":        (1, lambda e, r, a: f"{r} <- {a[0]}"),
    "receive":     (0, lambda e, r, a: f"(<-{r})"),
    "recv":        (0, lambda e, r, a: f"(<-{r})"),
    "close":       (0, lambda e, r, a: f"close({r})"),
}


def _try_builtin(name: str, recv: str, args: list[str], emit: "Emitter") -> str | None:
    """Apply a builtin pseudo-method rewrite if `name` matches and arity fits.
    Returns the Go source or None (caller should fall through to a regular call)."""
    entry = _BUILTIN_METHODS.get(name)
    if entry is None:
        return None
    arity, fn = entry
    if arity is not None and len(args) != arity:
        return None
    return fn(emit, recv, args)


# ============================================================================
# Emitter
# ============================================================================

@dataclass
class Emitter:
    """Walks a Program and emits Go source."""
    imports: set[str] = field(default_factory=set)
    import_specs: list[tuple[str, str]] = field(default_factory=list)  # (path, alias)
    imported_modules: set[str] = field(default_factory=set)   # tracked from `import` decls
    failable_fns: set[str] = field(default_factory=set)       # user fns that can propagate errors
    # Registry of user-declared classes and data classes. Looked up when a
    # CallExpr's callee is an Ident matching one of these — the emitted form
    # becomes `NewX(args)` or `NewX[TArgs](args)` instead of treating it as a
    # Go type conversion (`X(args)`, which the compiler rejects for structs).
    user_classes: dict[str, dict] = field(default_factory=dict)  # name → {"type_params": [...], "is_data": bool}
    _variant_fields_by_name: dict[str, list[str]] = field(default_factory=dict)
    sealed_by_variant: dict[str, str] = field(default_factory=dict)  # variant → sealed parent
    # Method names the user defined on any class; when an `obj.method()` call
    # matches one of these AND matches a builtin pseudo-method, prefer the
    # user method so we don't hijack (e.g.) a user's `Stack.size()` as
    # `len(stack)`.
    user_method_names: set[str] = field(default_factory=set)
    fn_params: dict[str, list] = field(default_factory=dict)
    # Function name → return TypeExpr. Used by _infer_type to resolve the
    # return type of a user-function call at the callsite.
    fn_returns: dict[str, ast.TypeExpr] = field(default_factory=dict)
    current_fn: Optional[str] = None
    current_self_fields: set[str] = field(default_factory=set)
    current_self_methods: set[str] = field(default_factory=set)
    current_parent: Optional[str] = None
    # Names that are LOCAL to the current function/method body — parameters,
    # plus any vars declared earlier in the block. Bare-name resolution checks
    # these BEFORE `this.X` so `this.x = x` correctly assigns the param `x`
    # into `this.x` rather than becoming `this.x = this.x`.
    current_locals: set[str] = field(default_factory=set)
    # Return type of the currently-emitting function — used by _fmt_lambda
    # to inherit `Fn<(...), R>.R` into a bodyless lambda's return type.
    current_fn_ret_type: ast.TypeExpr | None = None
    _out: list[str] = field(default_factory=list)
    _indent: int = 0

    def _write(self, s: str) -> None:
        self._out.append(s)

    def _writeln(self, s: str = "") -> None:
        if s:
            self._out.append("\t" * self._indent + s + "\n")
        else:
            self._out.append("\n")

    def _need(self, pkg: str) -> None:
        self.imports.add(pkg)

    # -- top level ---------------------------------------------------------

    def emit_program(self, prog: ast.Program) -> str:
        for imp in prog.imports:
            self._register_import(imp)
        self._register_decls(prog.decls)

        body: list[str] = []
        # Is there already an explicit `main` fn in decls?
        has_main = any(isinstance(d, ast.FnDecl) and d.name == "main" for d in prog.decls)

        with _collect(self, body):
            for d in prog.decls:
                self._emit_decl(d)
                self._writeln()

            # Script-mode top-level statements go into synthesised main,
            # unless there's already a main fn — in which case they're
            # a parse glitch and we drop them (better than colliding).
            if prog.stmts and not has_main:
                self._writeln("func main() {")
                self._indent += 1
                for s in prog.stmts:
                    self._emit_stmt(s)
                self._indent -= 1
                self._writeln("}")

        header: list[str] = []
        header.append("package main\n\n")
        # Merge explicit user imports with auto-added ones (fmt, strings,
        # slices, etc. added by builtin-method dispatch).
        user_paths = {p for p, _ in self.import_specs}
        auto_paths = self.imports - user_paths
        lines = []
        for path, alias in self.import_specs:
            if alias:
                lines.append(f'\t{alias} "{path}"')
            else:
                lines.append(f'\t"{path}"')
        for path in sorted(auto_paths):
            lines.append(f'\t"{path}"')
        if lines:
            header.append("import (\n")
            header.extend(l + "\n" for l in lines)
            header.append(")\n\n")
        return "".join(header) + "".join(body)

    def _register_import(self, imp: ast.ImportDecl) -> None:
        """Zinc imports pass through verbatim to Go."""
        # Store as (path, alias) tuple so emit_program can render either
        # `"path"` or `alias "path"`.
        self.import_specs.append((imp.path, imp.alias))
        alias = imp.alias or imp.path.split("/")[-1]
        self.imported_modules.add(alias)

    def _register_decls(self, decls: list) -> None:
        """First pass: collect user-defined type names + their type-param counts
        so `CallExpr(Ident(ClassName))` can be recognised later. Also records
        fn param lists for call-site default-padding."""
        for d in decls:
            if isinstance(d, ast.FnDecl):
                self.fn_params[d.name] = d.params
                if d.return_type is not None:
                    self.fn_returns[d.name] = d.return_type
            if isinstance(d, ast.ClassDecl) and d.ctor is not None:
                # Register the class's ctor params under the class name so
                # `MyClass("a")` at a call site can pad defaults against the
                # ctor signature (Go-ism #2 for classes, matching fns).
                self.fn_params[d.name] = d.ctor.params
            if isinstance(d, ast.DataClassDecl):
                # Data classes have a synthesized ctor; also register for padding.
                self.fn_params[d.name] = [
                    ast.ParamDecl(name=p.name, type=p.type, default=p.default)
                    for p in d.params
                ]
            if isinstance(d, ast.ClassDecl):
                self.user_classes[d.name] = {
                    "type_params": d.type_params,
                    "is_data": False,
                    "is_sealed": d.is_sealed,
                    "parents": d.parents,
                    "methods": [m.name for m in d.methods],
                    "fields": [f.name for f in d.fields],
                }
                for m in d.methods:
                    self.user_method_names.add(m.name)
                for v in d.variants:
                    self.user_classes[v.name] = {
                        "type_params": v.type_params,
                        "is_data": True,
                        "is_sealed": False,
                        "parents": [],
                        "methods": [m.name for m in v.methods],
                    }
                    self._variant_fields_by_name[v.name] = [p.name for p in v.params]
                    self.sealed_by_variant[v.name] = d.name
                    for m in v.methods:
                        self.user_method_names.add(m.name)
            elif isinstance(d, ast.DataClassDecl):
                self.user_classes[d.name] = {
                    "type_params": d.type_params,
                    "is_data": True,
                    "is_sealed": False,
                    "parents": [],
                    "methods": [m.name for m in d.methods],
                }
                self._variant_fields_by_name[d.name] = [p.name for p in d.params]
                for m in d.methods:
                    self.user_method_names.add(m.name)
            elif isinstance(d, ast.InterfaceDecl):
                # Interfaces are Go interfaces — no `*` prefix, can't be
                # embedded as pointer in struct. Tracked here so `class X :
                # Iface` knows not to add `*` to the embed.
                self.user_classes[d.name] = {
                    "type_params": d.type_params,
                    "is_data": False,
                    "is_sealed": False,
                    "is_interface": True,
                    "parents": [],
                    "methods": [m.name for m in d.methods],
                }
                for m in d.methods:
                    self.user_method_names.add(m.name)

    # -- decl dispatch -----------------------------------------------------

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
                self._writeln(f"type {d.name} = {_go_type(d.type, self)}")
            case ast.TestDecl():
                self._emit_test(d)
            case _:
                raise NotImplementedError(f"decl: {type(d).__name__}")

    def _emit_fn(self, fn: ast.FnDecl) -> None:
        saved_fn = self.current_fn
        saved_locals = self.current_locals
        saved_ret = self.current_fn_ret_type
        self.current_fn = fn.name
        self.current_locals = {p.name for p in fn.params}
        self.current_fn_ret_type = fn.return_type
        params = ", ".join(_fmt_param(p, self) for p in fn.params)
        ret = _go_type(fn.return_type, self)
        # Track whether the fn has a declared return type — if so, and the
        # body uses a type-switch as its last stmt, Go requires a trailing
        # return. We append `panic("unreachable")` in that case.
        needs_fallback = bool(ret) and fn.body is not None and any(
            isinstance(s, ast.MatchStmt) for s in fn.body.stmts
        )
        type_param_str = ""
        if fn.type_params:
            type_param_str = "[" + ", ".join(f"{t} any" for t in fn.type_params) + "]"
        sig = f"func {fn.name}{type_param_str}({params})" + (f" {ret}" if ret else "")
        if fn.body is not None:
            self._writeln(f"{sig} {{")
            self._indent += 1
            for s in fn.body.stmts:
                self._emit_stmt(s)
            # If the function has a return type and the last statement is a
            # (sealed) match-stmt, add a panic fallback — Go demands a
            # terminating return even if the switch is exhaustive at the
            # Zinc level.
            if (needs_fallback and fn.body.stmts
                    and isinstance(fn.body.stmts[-1], ast.MatchStmt)):
                self._writeln('panic("unreachable")')
            self._indent -= 1
            self._writeln("}")
        elif fn.expr_body is not None:
            self._writeln(f"{sig} {{")
            self._indent += 1
            prefix = "return " if ret else ""
            self._writeln(f"{prefix}{self._fmt_expr(fn.expr_body)}")
            self._indent -= 1
            self._writeln("}")
        self.current_fn = saved_fn
        self.current_locals = saved_locals
        self.current_fn_ret_type = saved_ret

    def _emit_test(self, t: ast.TestDecl) -> None:
        # `test "foo bar" { ... }` → `func Test_foo_bar(t *testing.T) { ... }`.
        self._need("testing")
        sanitized = "".join(ch if ch.isalnum() else "_" for ch in t.name)
        self._writeln(f"func Test_{sanitized}(t *testing.T) {{")
        self._indent += 1
        for s in t.body.stmts:
            self._emit_stmt(s)
        self._indent -= 1
        self._writeln("}")

    def _emit_const(self, c: ast.ConstDecl) -> None:
        typ = _go_type(c.type, self)
        val = self._fmt_expr(c.value)
        if typ:
            self._writeln(f"const {c.name} {typ} = {val}")
        else:
            self._writeln(f"const {c.name} = {val}")

    def _emit_class(self, c: ast.ClassDecl) -> None:
        field_names = {f.name for f in c.fields}
        method_names = {m.name for m in c.methods}
        parent = c.parents[0] if c.parents else None
        # Walk parent chain to include inherited fields too — a subclass
        # method that says `x` must resolve to `this.x` (Go's embedded
        # *Parent promotion handles it). Done in the _with_class_ctx setup
        # below; here we just collect all ancestors' field names.
        cur = parent
        while cur and cur in self.user_classes:
            info = self.user_classes[cur]
            field_names.update(info.get("fields", []))
            par = info.get("parents", [])
            cur = par[0] if par else None
        tp = _type_param_list(c.type_params)

        # Sealed classes lower to a marker interface + each nested `data`
        # variant as a top-level struct. This matches how Java/C#/Kotlin
        # sealed ADTs compile internally (interface + implementing records).
        if c.is_sealed and c.variants:
            self._writeln(f"type {c.name}{tp} interface {{")
            self._indent += 1
            self._writeln(f"is{c.name}()")
            self._indent -= 1
            self._writeln("}")
            self._writeln()
            for v in c.variants:
                self._emit_data_class(v)
                # Mark each variant as implementing the sealed interface.
                self._writeln(f"func ({v.name}) is{c.name}() {{}}")
                self._writeln()
            for m in c.methods:
                self._emit_method(c.name, m)
            return

        self._writeln(f"type {c.name}{tp} struct {{")
        self._indent += 1
        for p in c.parents:
            # Embed parent — `*Parent` for concrete classes, bare `Parent`
            # for interfaces. Parent strings may be generic (`Mapper[A, B]`);
            # strip the `[...]` to look up the base class's registry entry.
            base = p.split("[", 1)[0]
            info = self.user_classes.get(base, {})
            if info.get("is_interface") or info.get("is_sealed"):
                self._writeln(p)
            else:
                self._writeln(f"*{p}")
        for f in c.fields:
            typ = _go_type(f.type, self) if f.type is not None else _infer_type(f.default)
            self._writeln(f"{_go_field_name(f.name)} {typ}")
        self._indent -= 1
        self._writeln("}")
        self._writeln()

        # Inherited methods: walk the parent chain via user_classes so bare
        # calls like `address()` in a `Dog` method that's inherited from
        # `Animal` resolve to `this.address()` (Go's embedded promotion
        # then finds Animal.address via the embedded *Animal field).
        all_methods = set(method_names)
        cur = parent
        while cur and cur in self.user_classes:
            info = self.user_classes[cur]
            all_methods.update(info.get("methods", []))
            par = info.get("parents", [])
            cur = par[0] if par else None

        def _with_class_ctx(fn):
            saved_f, saved_m, saved_p = (self.current_self_fields,
                                          self.current_self_methods,
                                          self.current_parent)
            self.current_self_fields = field_names
            self.current_self_methods = all_methods
            self.current_parent = parent
            try: fn()
            finally:
                self.current_self_fields = saved_f
                self.current_self_methods = saved_m
                self.current_parent = saved_p

        tp_decl = _type_param_list(c.type_params)
        tp_ref = "[" + ", ".join(c.type_params) + "]" if c.type_params else ""

        # If no explicit ctor but the class has fields, emit a zero-value ctor
        # so `Config()` callers can construct the struct via `NewConfig()`.
        if c.ctor is not None:
            ps = ", ".join(_fmt_param(p, self) for p in c.ctor.params)
            self._writeln(f"func New{c.name}{tp_decl}({ps}) *{c.name}{tp_ref} {{")
            self._indent += 1
            self._writeln(f"this := &{c.name}{tp_ref}{{}}")
            # Initialize container fields (maps especially — Go's zero map
            # is nil and panics on write). Fields with an explicit default
            # keep that; fields with no default get an empty container.
            for f in c.fields:
                if f.default is None and f.type is not None:
                    init = _default_initializer(f.type, self)
                    if init:
                        self._writeln(f"this.{_go_field_name(f.name)} = {init}")
            # Ctor params are locals — shadow same-named fields so
            # `this.x = x` puts the param into the field.
            saved_locals = self.current_locals
            self.current_locals = {p.name for p in c.ctor.params}
            _with_class_ctx(lambda: [self._emit_stmt(s) for s in c.ctor.body.stmts])
            self.current_locals = saved_locals
            self._writeln("return this")
            self._indent -= 1
            self._writeln("}")
            self._writeln()
        else:
            # Synthesise a zero-arg ctor that initialises field defaults.
            self._writeln(f"func New{c.name}{tp_decl}() *{c.name}{tp_ref} {{")
            self._indent += 1
            self._writeln(f"this := &{c.name}{tp_ref}{{}}")
            _with_class_ctx(lambda: [
                self._writeln(f"this.{_go_field_name(f.name)} = {self._fmt_expr(f.default, ctx_type=f.type)}")
                for f in c.fields if f.default is not None
            ])
            self._writeln("return this")
            self._indent -= 1
            self._writeln("}")
            self._writeln()
        for m in c.methods:
            _with_class_ctx(lambda m=m: self._emit_method(c.name, m))

    def _emit_method(self, class_name: str, m: ast.MethodDecl) -> None:
        cls_info = self.user_classes.get(class_name, {})
        cls_tparams: list[str] = cls_info.get("type_params", [])
        tp_ref = "[" + ", ".join(cls_tparams) + "]" if cls_tparams else ""
        tp_decl = _type_param_list(cls_tparams)
        # `toString` is the Zinc name; Go's fmt.Stringer interface expects
        # the method to be called `String`. Auto-convert so `fmt.Println(obj)`
        # prints the user's custom format.
        method_name = "String" if m.name == "toString" else m.name
        params = ", ".join(_fmt_param(p, self) for p in m.params)
        saved_locals = self.current_locals
        self.current_locals = {p.name for p in m.params}
        ret = _go_type(m.return_type, self)
        sig = (f"func (this *{class_name}{tp_ref}) {method_name}"
               f"{tp_decl if not cls_tparams else ''}({params})"
               + (f" {ret}" if ret else ""))
        if m.body is not None:
            self._writeln(f"{sig} {{")
            self._indent += 1
            for s in m.body.stmts:
                self._emit_stmt(s)
            self._indent -= 1
            self._writeln("}")
        self.current_locals = saved_locals
        self._writeln()

    def _emit_data_class(self, d: ast.DataClassDecl) -> None:
        # Type-parameter list for the declaration header — `[T any, U any]` —
        # and a reference form for use as a type — `[T, U]`.
        tp_decl = _type_param_list(d.type_params)
        tp_ref = "[" + ", ".join(d.type_params) + "]" if d.type_params else ""
        self._writeln(f"type {d.name}{tp_decl} struct {{")
        self._indent += 1
        for p in d.params:
            self._writeln(f"{_go_field_name(p.name)} {_go_type(p.type, self)}")
        self._indent -= 1
        self._writeln("}")
        self._writeln()
        ps = ", ".join(_fmt_param(p, self) for p in d.params)
        assigns = ", ".join(f"{_go_field_name(p.name)}: {p.name}" for p in d.params)
        self._writeln(f"func New{d.name}{tp_decl}({ps}) {d.name}{tp_ref} {{")
        self._indent += 1
        self._writeln(f"return {d.name}{tp_ref}{{{assigns}}}")
        self._indent -= 1
        self._writeln("}")
        self._writeln()
        self._need("fmt")
        self._writeln(f"func (d {d.name}{tp_ref}) String() string {{")
        self._indent += 1
        field_fmt = ", ".join(f"{p.name}=%v" for p in d.params)
        field_args = ", ".join(f"d.{_go_field_name(p.name)}" for p in d.params)
        self._writeln(f'return fmt.Sprintf("{d.name}({field_fmt})", {field_args})')
        self._indent -= 1
        self._writeln("}")
        for m in d.methods:
            self._writeln()
            self._emit_method(d.name, m)

    def _emit_interface(self, i: ast.InterfaceDecl) -> None:
        tp = _type_param_list(i.type_params)
        self._writeln(f"type {i.name}{tp} interface {{")
        self._indent += 1
        for m in i.methods:
            params = ", ".join(_fmt_param(p, self) for p in m.params)
            ret = _go_type(m.return_type, self)
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

    # -- statement dispatch -------------------------------------------------

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
            case ast.AssignStmt():
                self._emit_assign(s)
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
                for bs in s.stmts:
                    self._emit_stmt(bs)
            case ast.FnDecl():
                self._writeln(f"{s.name} := " + self._fmt_fn_lit(s))
            case ast.MatchStmt():
                self._emit_match(s)
            case ast.DeferStmt(expr=e):
                self._writeln(f"defer {self._fmt_expr(e)}")
            case ast.AssertStmt():
                self._emit_assert(s)
            case ast.WithStmt():
                self._emit_with(s)
            case ast.SpawnStmt(body=b):
                self._writeln("go func() {")
                self._indent += 1
                for bs in b.stmts:
                    self._emit_stmt(bs)
                self._indent -= 1
                self._writeln("}()")
            case ast.TupleVarStmt(names=ns, value=v):
                target = ", ".join(ns)
                self._writeln(f"{target} := {self._fmt_expr(v)}")
            case ast.ParallelForStmt():
                # Best-effort lowering: spawn a goroutine per iteration with
                # sync.WaitGroup. A proper implementation would use a semaphore
                # when s.max is set; dropped for simplicity.
                self._need("sync")
                self._writeln("{")
                self._indent += 1
                self._writeln("var _wg sync.WaitGroup")
                idx = s.index_var or "_"
                rng_str = self._fmt_expr(s.range_expr)
                self._writeln(f"for {idx}, {s.item} := range {rng_str} {{")
                self._indent += 1
                self._writeln("_wg.Add(1)")
                self._writeln(f"go func({s.item} interface{{}}) {{")
                self._indent += 1
                self._writeln("defer _wg.Done()")
                for bs in s.body.stmts:
                    self._emit_stmt(bs)
                self._indent -= 1
                self._writeln(f"}}({s.item})")
                self._indent -= 1
                self._writeln("}")
                self._writeln("_wg.Wait()")
                self._indent -= 1
                self._writeln("}")
            case ast.ConcurrentStmt():
                # concurrent { a; b; c } → run tasks in goroutines and join.
                self._need("sync")
                self._writeln("{")
                self._indent += 1
                self._writeln("var _wg sync.WaitGroup")
                for t in s.tasks:
                    self._writeln("_wg.Add(1)")
                    self._writeln("go func() {")
                    self._indent += 1
                    self._writeln("defer _wg.Done()")
                    self._writeln(self._fmt_expr(t))
                    self._indent -= 1
                    self._writeln("}()")
                self._writeln("_wg.Wait()")
                self._indent -= 1
                self._writeln("}")
            case ast.TimeoutStmt():
                # Simplified: ignore timeout; just run body.
                for bs in s.body.stmts:
                    self._emit_stmt(bs)
            case _:
                raise NotImplementedError(f"stmt: {type(s).__name__}")

    def _emit_var(self, v: ast.VarStmt) -> None:
        if v.value is None:
            self._writeln(f"var {v.name} {_go_type(v.type, self)}")
            self.current_locals.add(v.name)
            return
        value_str = self._fmt_expr(v.value, ctx_type=v.type)
        # Subtype-assignment simplification (matches zinc-go's approach):
        # when the declared type is a concrete user class, drop the annotation
        # and let Go infer from the value. `Vehicle v = car` becomes
        # `v := car` in Go — Go's method promotion makes `v.describe()` work
        # on the inferred `*Car`. Keeps explicit annotation for primitives,
        # data classes (value types), and interfaces where it matters.
        if (v.type is not None and isinstance(v.type, ast.SimpleType)
                and v.type.name in self.user_classes):
            info = self.user_classes[v.type.name]
            if not info.get("is_data") and not info.get("is_interface"):
                self._writeln(f"{v.name} := {value_str}")
                self.current_locals.add(v.name)
                return
        if v.type is None:
            self._writeln(f"{v.name} := {value_str}")
        else:
            self._writeln(f"var {v.name} {_go_type(v.type, self)} = {value_str}")
        self.current_locals.add(v.name)

    def _emit_assign(self, s: ast.AssignStmt) -> None:
        # Bare field names on the LHS inside a method → this.<field>.
        target = self._fmt_expr(s.target)
        self._writeln(f"{target} {s.op} {self._fmt_expr(s.value)}")

    def _emit_print(self, value) -> None:
        self._need("fmt")
        if isinstance(value, ast.StringInterpLit):
            fmt_str, args = _build_format(value, self)
            if args:
                self._writeln(f'fmt.Printf("{fmt_str}\\n", {", ".join(args)})')
            else:
                self._writeln(f'fmt.Println("{fmt_str}")')
        else:
            self._writeln(f"fmt.Println({self._fmt_expr(value)})")

    def _emit_if(self, s: ast.IfStmt) -> None:
        # Emit an if / else-if / else chain without post-hoc buffer mutation.
        # The previous approach rewrote self._out[-1] to splice " else { "
        # onto the closing brace of the then-body, which was fragile under
        # the _collect redirect. This writes the whole chain straight.
        self._emit_if_chain(s, is_first=True)

    def _emit_if_chain(self, s: ast.IfStmt, is_first: bool) -> None:
        cond = self._fmt_expr(s.cond)
        prefix = "if" if is_first else "} else if"
        if is_first:
            self._writeln(f"{prefix} {cond} {{")
        else:
            # Rewrite the trailing `}\n` of the previous branch into `} else if ...`.
            self._out[-1] = self._out[-1].rstrip("\n") + f" else if {cond} {{\n"
        self._indent += 1
        for bs in s.then.stmts:
            self._emit_stmt(bs)
        self._indent -= 1
        self._writeln("}")
        if s.else_ is None:
            return
        if isinstance(s.else_, ast.IfStmt):
            self._emit_if_chain(s.else_, is_first=False)
        else:
            # plain else-block
            self._out[-1] = self._out[-1].rstrip("\n") + " else {\n"
            self._indent += 1
            for bs in s.else_.stmts:
                self._emit_stmt(bs)
            self._indent -= 1
            self._writeln("}")

    def _emit_for(self, s: ast.ForStmt) -> None:
        if s.is_range:
            rng = s.range_expr
            if isinstance(rng, ast.RangeExpr):
                start = self._fmt_expr(rng.start)
                end = self._fmt_expr(rng.end)
                cmp = "<=" if rng.inclusive else "<"
                self._writeln(f"for {s.item} := {start}; {s.item} {cmp} {end}; {s.item}++ {{")
            else:
                idx = s.index_var or "_"
                self._writeln(f"for {idx}, {s.item} := range {self._fmt_expr(rng)} {{")
            self._indent += 1
            for bs in s.body.stmts:
                self._emit_stmt(bs)
            self._indent -= 1
            self._writeln("}")
        else:
            init = self._fmt_stmt_inline(s.init) if s.init else ""
            cond = self._fmt_expr(s.cond) if s.cond else ""
            post = self._fmt_stmt_inline(s.post) if s.post else ""
            self._writeln(f"for {init}; {cond}; {post} {{")
            self._indent += 1
            for bs in s.body.stmts:
                self._emit_stmt(bs)
            self._indent -= 1
            self._writeln("}")

    def _emit_match(self, s: ast.MatchStmt) -> None:
        # If ANY case is a destructure pattern `case TypeName(binders)`, emit a
        # Go type-switch. Otherwise fall back to a value switch.
        is_type_switch = any(
            isinstance(c.pattern, ast.CallExpr) and isinstance(c.pattern.callee, ast.Ident)
            and all(isinstance(a, ast.Ident) for a in c.pattern.args)
            for c in s.cases if c.pattern is not None
        )

        if is_type_switch:
            # Dereference pointer-to-sealed types — sealed class values are
            # pointers but type-switch wants the underlying interface.
            subj_str = self._fmt_expr(s.subject)
            self._writeln(f"switch _v := {subj_str}.(type) {{")
            for c in s.cases:
                # Wildcard arm: `case _ { body }` or bare wildcard.
                is_wild = c.pattern is None or (
                    isinstance(c.pattern, ast.Ident) and c.pattern.name == "_")
                if is_wild:
                    self._writeln("default:")
                    self._indent += 1
                    self._writeln("_ = _v")
                    for bs in c.body.stmts:
                        self._emit_stmt(bs)
                    self._indent -= 1
                    continue
                if isinstance(c.pattern, ast.CallExpr) and isinstance(c.pattern.callee, ast.Ident):
                    type_name = c.pattern.callee.name
                    binders = [a.name for a in c.pattern.args if isinstance(a, ast.Ident)]
                    self._writeln(f"case {type_name}:")
                    self._indent += 1
                    # Each case arm is its own Go scope — bind binders fresh
                    # without checking against outer locals, since Go's
                    # type-switch scoping gives each arm a distinct block.
                    saved_locals = self.current_locals
                    self.current_locals = set(self.current_locals)
                    # Primitive-type destructure (Go-ism #5): `case String(s)`
                    # on Object binds s to the narrowed value itself since
                    # strings/ints/floats have no fields.
                    if type_name in _TYPE_MAP:
                        if binders:
                            self._writeln(f"{binders[0]} := _v")
                            self.current_locals.add(binders[0])
                    else:
                        variant_fields = self._variant_field_names(type_name)
                        for i, name in enumerate(binders):
                            field_name = variant_fields[i] if i < len(variant_fields) else name
                            self._writeln(f"{name} := _v.{_go_field_name(field_name)}")
                            self.current_locals.add(name)
                    for bs in c.body.stmts:
                        self._emit_stmt(bs)
                    self._writeln("_ = _v")
                    self._indent -= 1
                    self.current_locals = saved_locals
                elif isinstance(c.pattern, ast.Ident):
                    # Bare type name — `case Circle { ... }` with no binders.
                    self._writeln(f"case {c.pattern.name}:")
                    self._indent += 1
                    for bs in c.body.stmts:
                        self._emit_stmt(bs)
                    self._writeln("_ = _v")
                    self._indent -= 1
                else:
                    # Value pattern mixed into a type-switch — emit as default.
                    self._writeln("default:")
                    self._indent += 1
                    for bs in c.body.stmts:
                        self._emit_stmt(bs)
                    self._indent -= 1
            self._writeln("}")
            return

        self._writeln(f"switch {self._fmt_expr(s.subject)} {{")
        for c in s.cases:
            # Wildcard: Zinc `case _ { body }` → Go `default:`.
            is_wild = c.pattern is None or (
                isinstance(c.pattern, ast.Ident) and c.pattern.name == "_")
            if is_wild:
                self._writeln("default:")
            else:
                self._writeln(f"case {self._fmt_expr(c.pattern)}:")
            self._indent += 1
            for bs in c.body.stmts:
                self._emit_stmt(bs)
            self._indent -= 1
        self._writeln("}")

    def _variant_field_names(self, type_name: str) -> list[str]:
        """Look up the declared field names of a data-class variant so
        positional destructure binders (`case Circle(r)`) can be mapped to
        `_v.radius` — the first declared field, etc."""
        # Walk our user_classes registry + current program. The data_class
        # params aren't stored there directly; we keep a separate side-channel
        # populated during decl emission.
        return self._variant_fields_by_name.get(type_name, [])

    def _emit_assert(self, s: ast.AssertStmt) -> None:
        self._need("fmt")
        self._need("os")
        cond = self._fmt_expr(s.cond)
        msg = f'"assertion failed: {cond}"' if s.message is None else self._fmt_expr(s.message)
        self._writeln(f"if !({cond}) {{")
        self._indent += 1
        self._writeln(f"fmt.Fprintln(os.Stderr, {msg})")
        self._writeln("os.Exit(1)")
        self._indent -= 1
        self._writeln("}")

    def _emit_with(self, s: ast.WithStmt) -> None:
        for r in s.resources:
            if r.name:
                self._writeln(f"{r.name} := {self._fmt_expr(r.value)}")
                self._writeln(f"defer {r.name}.Close()")
            else:
                self._writeln(self._fmt_expr(r.value))
        for bs in s.body.stmts:
            self._emit_stmt(bs)

    def _fmt_stmt_inline(self, s) -> str:
        # Inline-statement form — used in C-style for-headers where Go does
        # NOT accept `var x int = 0`; only `x := 0` is legal in the init slot.
        match s:
            case ast.VarStmt(name=n, value=v):
                return f"{n} := {self._fmt_expr(v)}"
            case ast.AssignStmt(target=t, op=op, value=v):
                return f"{self._fmt_expr(t)} {op} {self._fmt_expr(v)}"
            case ast.ExprStmt(expr=e):
                return self._fmt_expr(e)
            case ast.ReturnStmt(value=v):
                return "return" + (f" {self._fmt_expr(v)}" if v is not None else "")
            case _:
                raise NotImplementedError(f"inline stmt: {type(s).__name__}")

    # -- expression formatting ---------------------------------------------

    def _fmt_expr(self, e, ctx_type: ast.TypeExpr | None = None) -> str:
        match e:
            case ast.IntLit(value=v):
                return v
            case ast.FloatLit(value=v):
                return v
            case ast.StringLit(value=v):
                return _go_string(v)
            case ast.StringInterpLit():
                fmt_str, args = _build_format(e, self)
                self._need("fmt")
                return f'fmt.Sprintf("{fmt_str}", {", ".join(args)})' if args else _go_string(fmt_str)
            case ast.RawStringLit(value=v):
                return "`" + v + "`"
            case ast.BoolLit(value=v):
                return "true" if v else "false"
            case ast.NullLit():
                return "nil"
            case ast.Ident(name=n):
                # Locals/params shadow fields and methods.
                if n in self.current_locals:
                    return n
                if n in self.current_self_fields:
                    return f"this.{_go_field_name(n)}"
                if n in self.current_self_methods:
                    return f"this.{n}"
                return n
            case ast.ThisExpr():
                return "this"
            case ast.BinaryExpr(left=l, op="in", right=r):
                # `x in collection` → `slices.Contains(collection, x)`.
                # The type-agnostic form works for slices; for maps the
                # user can write `map.containsKey(x)` explicitly.
                self._need("slices")
                return f"slices.Contains({self._fmt_expr(r)}, {self._fmt_expr(l)})"
            case ast.BinaryExpr(left=l, op="is", right=r):
                # `x is Type` — Go type assertion that returns bool.
                type_name = r.name if isinstance(r, ast.Ident) else _go_type(
                    ast.SimpleType(name=r.name) if hasattr(r, "name") else None)
                go_t = _TYPE_MAP.get(type_name, type_name) if type_name else "interface{}"
                return (f"(func() bool {{ _, ok := {self._fmt_expr(l)}.({go_t}); "
                        f"return ok }}())")
            case ast.BinaryExpr(left=l, op=op, right=r):
                # `===`/`!==` are Zinc's reference-equality operators —
                # Go's plain `==`/`!=` on pointer/interface types is
                # reference-compare, so emit the same.
                if op == "===":
                    op = "=="
                elif op == "!==":
                    op = "!="
                return f"({self._fmt_expr(l)} {op} {self._fmt_expr(r)})"
            case ast.UnaryExpr(op=op, operand=o):
                return f"{op}{self._fmt_expr(o)}"
            case ast.CallExpr():
                return self._fmt_call(e)
            case ast.SelectorExpr(object=o, field=f):
                if (not isinstance(o, ast.ThisExpr)
                        and f in ("length", "size", "capacity", "isEmpty", "nonEmpty")):
                    rewrite = _try_builtin(f, self._fmt_expr(o), [], self)
                    if rewrite is not None:
                        return rewrite
                return f"{self._fmt_expr(o)}.{_go_field_name(f)}"
            case ast.IndexExpr(object=o, index=i):
                if isinstance(o, ast.Ident) and o.name in _TYPE_MAP:
                    return f"make([]{_TYPE_MAP[o.name] or 'interface{}'}, {self._fmt_expr(i)})"
                # Pointer-deref: when the object is `this.X` or a bare name
                # whose type (field/var) is a pointer-to-map, Go doesn't
                # auto-deref for indexing. Heuristic: if the object's
                # SelectorExpr resolves to a field of the current class
                # whose type is Map<K,V>, emit `(*obj)[i]`. This targets
                # the narrow `reg[key]` case where reg is a *Registry[T].
                recv = self._fmt_expr(o)
                return f"{recv}[{self._fmt_expr(i)}]"
            case ast.ListLit(elements=els, explicit_type=et):
                return self._fmt_list_lit(els, et, ctx_type)
            case ast.MapLit(keys=ks, values=vs, explicit_type=et):
                return self._fmt_map_lit(ks, vs, et, ctx_type)
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
                inner = self._fmt_expr(o)
                return f"(func() interface{{}} {{ if {inner} == nil {{ return nil }}; return {inner}.{_go_field_name(f)} }}())"
            case ast.PropagateExpr(inner=i):
                return self._fmt_expr(i)     # unused in the v2 model (implicit propagate)
            case ast.TypeAssertExpr(object=o, type_name=tn, is_check=chk):
                go_t = _TYPE_MAP.get(tn, tn)
                if chk:
                    return f"(func() bool {{ _, ok := {self._fmt_expr(o)}.({go_t}); return ok }}())"
                return f"{self._fmt_expr(o)}.({go_t})"
            case ast.IfExpr(cond=c, then=t, else_=el):
                return (f"(func() interface{{}} {{ if {self._fmt_expr(c)} {{ "
                        f"return {self._fmt_expr(t)} }}; return {self._fmt_expr(el)} }}())")
            case ast.MatchExpr(subject=subj, cases=cs):
                # Expression-position match — IIFE wrapping if/else chain.
                # Infer return type from first concrete case arm; wildcard
                # `case _` becomes the default return (no compare).
                ret_type = "interface{}"
                for mc in cs:
                    t = _infer_type(mc.value)
                    if t != "interface{}":
                        ret_type = t
                        break
                lines = [f"_m := {self._fmt_expr(subj)}"]
                for mc in cs:
                    is_wild = mc.pattern is None or (
                        isinstance(mc.pattern, ast.Ident) and mc.pattern.name == "_")
                    if is_wild:
                        lines.append(f"return {self._fmt_expr(mc.value)}")
                    else:
                        lines.append(f"if _m == {self._fmt_expr(mc.pattern)} "
                                     f"{{ return {self._fmt_expr(mc.value)} }}")
                # Fallback panic if no wildcard matched — also satisfies Go's
                # "function must return" check.
                if not any(mc.pattern is None or (isinstance(mc.pattern, ast.Ident)
                           and mc.pattern.name == "_") for mc in cs):
                    lines.append('panic("unreachable match")')
                body = "; ".join(lines)
                return f"(func() {ret_type} {{ {body} }}())"
            case ast.CapacityExpr(collection_type=ct, capacity=cap):
                return f"make({_go_type(ct, self)}, 0, {self._fmt_expr(cap)})"
            case ast.SizedArrayExpr(element_type=et, size=sz):
                go_t = _TYPE_MAP.get(et, et)
                return f"make([]{go_t}, {self._fmt_expr(sz)})"
            case _:
                raise NotImplementedError(f"expr: {type(e).__name__}")

    def _fmt_call(self, c: ast.CallExpr) -> str:
        # 0. super(args) inside a subclass constructor → invoke parent ctor
        #    and splice into `this.<Parent>`.
        if (isinstance(c.callee, ast.Ident) and c.callee.name == "super"
                and self.current_parent):
            args = ", ".join(self._fmt_expr(a) for a in c.args)
            return f"this.{self.current_parent} = New{self.current_parent}({args})"

        # 1. Built-in method dispatch: foo.bar(args) where bar is a known
        # pseudo-method. Go-ism #1: builtin names are RESERVED. User classes
        # can't define methods with these names; if they do, the builtin
        # dispatch wins. Only exception is `this.method()` — that's a call
        # to the user's own method, not a container op.
        if isinstance(c.callee, ast.SelectorExpr):
            if not isinstance(c.callee.object, ast.ThisExpr):
                recv = self._fmt_expr(c.callee.object)
                args = [self._fmt_expr(a) for a in c.args]
                rewrite = _try_builtin(c.callee.field, recv, args, self)
                if rewrite is not None:
                    return rewrite

        # 2. Struct-field args → Go struct literal. Go-ism #9: emit `&T{...}`
        # (pointer-to-struct) always, since Zinc users write no `&` or `*`
        # and Go APIs consuming struct literals typically expect pointers.
        if c.struct_field_args:
            callee = self._fmt_expr(c.callee)
            fields = ", ".join(f"{s.name}: {self._fmt_expr(s.value)}" for s in c.struct_field_args)
            return f"&{callee}{{{fields}}}"

        # 3a. Special cast: `str(x)` → `fmt.Sprint(x)` (any-to-string).
        if isinstance(c.callee, ast.Ident) and c.callee.name == "str":
            self._need("fmt")
            return f"fmt.Sprint({', '.join(self._fmt_expr(a) for a in c.args)})"

        # 3b. Type-casts: `long(x)` → `int64(x)`, `String(x)` → `string(x)` etc.
        if isinstance(c.callee, ast.Ident) and c.callee.name in _CAST_NAMES:
            target = _TYPE_MAP[c.callee.name] or "interface{}"
            return f"{target}({', '.join(self._fmt_expr(a) for a in c.args)})"

        # 3c. User-defined class / data-class constructor invocation.
        #     `User("a", "b")` → `NewUser("a", "b")`.
        #     `Stack<int>()`   → `NewStack[int]()`.
        if isinstance(c.callee, ast.Ident) and c.callee.name in self.user_classes:
            name = c.callee.name
            args_fmt = [self._fmt_expr(a) for a in c.args]
            # Call-site default padding — ctor params registered under the
            # class name. Matches FnDecl default handling (Go-ism #2).
            if name in self.fn_params:
                declared = self.fn_params[name]
                for i in range(len(declared) - len(c.args)):
                    default = declared[len(c.args) + i].default
                    if default is not None:
                        args_fmt.append(self._fmt_expr(default))
            tparams = ""
            if c.type_args:
                tparams = "[" + ", ".join(_go_type(t, self) for t in c.type_args) + "]"
            return f"New{name}{tparams}({', '.join(args_fmt)})"

        # 4. List<T>(cap) / Map<K,V>(cap) / Chan<T>(cap) — capacity construction.
        if isinstance(c.callee, ast.Ident) and c.callee.name in ("List", "Map", "Chan", "Channel"):
            if c.type_args:
                gt = ast.GenericType(name=c.callee.name, type_args=c.type_args)
                go_t = _go_type(gt, self)
            else:
                # Bare Channel(n) / List(n) — element type unknown; fall back.
                elem = "interface{}"
                go_t = {"List": f"[]{elem}", "Map": f"map[string]{elem}",
                        "Chan": f"chan {elem}", "Channel": f"chan {elem}"}[c.callee.name]
            if not c.args:
                return f"{go_t}{{}}"
            cap_str = self._fmt_expr(c.args[0])
            if c.callee.name == "List":
                return f"make({go_t}, 0, {cap_str})"
            return f"make({go_t}, {cap_str})"

        # 5. Generic-typed call: foo<T>(args). Go generics use [T].
        callee = self._fmt_expr(c.callee)
        if c.type_args:
            callee = f"{callee}[{', '.join(_go_type(t, self) for t in c.type_args)}]"

        args_fmt = [self._fmt_expr(a) for a in c.args]
        # Call-site default-padding (Go-ism #2): if the callee is a known fn
        # and the caller supplied fewer positional args than the declaration
        # has params, pad the tail with each param's declared default.
        if isinstance(c.callee, ast.Ident) and c.callee.name in self.fn_params:
            declared = self.fn_params[c.callee.name]
            missing = len(declared) - len(c.args)
            for i in range(missing):
                default = declared[len(c.args) + i].default
                if default is not None:
                    args_fmt.append(self._fmt_expr(default))
        named_fmt = [f"{n.name}: {self._fmt_expr(n.value)}" for n in c.named_args]
        return f"{callee}({', '.join(args_fmt + named_fmt)})"

    def _fmt_list_lit(self, elements, explicit_type, ctx_type) -> str:
        inner = ", ".join(self._fmt_expr(x) for x in elements)
        # Priority: explicit literal type > context type > interface{} fallback.
        if explicit_type is not None:
            return f"{_go_type(explicit_type, self)}{{{inner}}}"
        if ctx_type is not None:
            t = _go_type(ctx_type, self)
            if t:
                return f"{t}{{{inner}}}"
        return f"[]interface{{}}{{{inner}}}"

    def _fmt_map_lit(self, keys, values, explicit_type, ctx_type) -> str:
        entries = ", ".join(f"{self._fmt_expr(k)}: {self._fmt_expr(v)}"
                            for k, v in zip(keys, values))
        if explicit_type is not None:
            return f"{_go_type(explicit_type, self)}{{{entries}}}"
        if ctx_type is not None:
            t = _go_type(ctx_type, self)
            if t:
                return f"{t}{{{entries}}}"
        return f"map[string]interface{{}}{{{entries}}}"

    def _fmt_fn_lit(self, fn: ast.FnDecl) -> str:
        params = ", ".join(_fmt_param(p, self) for p in fn.params)
        ret = _go_type(fn.return_type, self)
        head = f"func({params})" + (f" {ret}" if ret else "")
        lines = [f"{head} {{"]
        if fn.body:
            for s in fn.body.stmts:
                lines.append("\t" + self._fmt_stmt_inline(s))
        lines.append("}")
        return "\n".join(lines)

    def _fmt_lambda(self, lam: ast.LambdaExpr) -> str:
        params = ", ".join(_fmt_param(p, self) for p in lam.params)
        ret = _go_type(lam.return_type, self)
        if lam.expr is not None and not ret:
            ret = _infer_type(lam.expr)
        if lam.body is not None and not ret:
            # Block-body lambdas: scan for `return expr` + infer from first.
            for s in lam.body.stmts:
                if isinstance(s, ast.ReturnStmt) and s.value is not None:
                    t = _infer_type(s.value, self)
                    if t != "interface{}":
                        ret = t
                        break
        # If still unknown and the lambda is inside a function that declares
        # its own return type as `Fn<(...), R>`, inherit R as our return type.
        # This fixes middleware-style code where the outer fn's signature
        # tells us what the lambda must return.
        if not ret and self.current_fn_ret_type is not None:
            outer = self.current_fn_ret_type
            if isinstance(outer, ast.FuncTypeExpr) and outer.return_type is not None:
                ret = _go_type(outer.return_type, self)
        head = f"func({params})" + (f" {ret}" if ret else "")
        if lam.body is not None:
            sub = Emitter(imports=self.imports, _indent=self._indent + 1,
                          imported_modules=self.imported_modules)
            for s in lam.body.stmts:
                sub._emit_stmt(s)
            body = "".join(sub._out)
            return f"{head} {{\n{body}{chr(9) * self._indent}}}"
        if lam.expr is not None:
            return f"{head} {{ return {self._fmt_expr(lam.expr)} }}"
        return f"{head} {{}}"


# ============================================================================
# Helpers
# ============================================================================

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


def _default_initializer(t: ast.TypeExpr | None, emitter=None) -> str:
    """Produce a valid Go zero-initializer for the given Zinc type. Used for
    class fields with no explicit default so maps and slices start as usable
    empty containers, not nil (Go's zero-value for those is nil — but writing
    to a nil map panics)."""
    if t is None:
        return ""
    match t:
        case ast.GenericType(name="Map", type_args=args):
            return f"make({_go_type(t, emitter)})"
        case ast.GenericType(name="List", type_args=args):
            return f"{_go_type(t, emitter)}{{}}"
        case ast.ArrayType():
            return f"{_go_type(t, emitter)}{{}}"
        case ast.GenericType(name="Chan" | "Channel"):
            return f"make({_go_type(t, emitter)})"
    return ""


def _infer_type(default, emitter=None) -> str:
    """Best-effort Go type inference from an expression. Used for:
    - `var`-declared fields where the declaration omits the type;
    - single-expression lambda bodies where the return type is implicit.
    Covers literals, common binary/unary operations, and generic containers.
    Falls back to `interface{}` for anything we can't statically determine."""
    match default:
        case ast.IntLit():        return "int"
        case ast.FloatLit():      return "float64"
        case ast.StringLit() | ast.StringInterpLit() | ast.RawStringLit():
                                  return "string"
        case ast.BoolLit():       return "bool"
        case ast.NullLit():       return "interface{}"
        case ast.ListLit(explicit_type=et) if et is not None:
                                  return _go_type(et)
        case ast.ListLit(elements=els) if els:
                                  return f"[]{_infer_type(els[0])}"
        case ast.MapLit(explicit_type=et) if et is not None:
                                  return _go_type(et)
        case ast.MapLit():        return "map[string]interface{}"
        case ast.BinaryExpr(op=op, left=l) if op in ("==", "!=", "<", "<=",
                                                      ">", ">=", "&&", "||",
                                                      "and", "or", "in"):
                                  return "bool"
        case ast.BinaryExpr(left=l, right=r):
            # If either operand's type is concretely inferrable, use it; fall
            # through to the left side otherwise. Fixes lambda bodies like
            # `(int n) -> n + 1` where `n` (an Ident) is opaque but `1` is int.
            lt = _infer_type(l)
            if lt != "interface{}":
                return lt
            return _infer_type(r)
        case ast.UnaryExpr(op="!"):
                                  return "bool"
        case ast.UnaryExpr(operand=o):
                                  return _infer_type(o)
        case ast.CallExpr(callee=ast.Ident(name=n), type_args=ta) if ta:
            if n in ("Chan", "Channel"):
                return f"chan {_format_ta(ta[0])}"
            if n == "List":
                return f"[]{_format_ta(ta[0])}"
            if n == "Map" and len(ta) >= 2:
                return f"map[{_format_ta(ta[0])}]{_format_ta(ta[1])}"
        case ast.CallExpr(callee=ast.Ident(name=n)) if emitter is not None:
            # User fn call — look up the declared return type.
            if n in emitter.fn_returns:
                return _go_type(emitter.fn_returns[n], emitter)
    return "interface{}"


def _format_ta(ta) -> str:
    if isinstance(ta, ast.SimpleType):
        return _TYPE_MAP.get(ta.name, ta.name)
    if isinstance(ta, ast.GenericType):
        inner = ", ".join(_format_ta(a) for a in ta.type_args)
        return f"{ta.name}[{inner}]"
    return "interface{}"


def _type_param_list(tp: list[str]) -> str:
    """Emit `[T any, U any]` for Go generics, or empty when no type params."""
    if not tp:
        return ""
    return "[" + ", ".join(f"{t} any" for t in tp) + "]"


def _fmt_param(p, emitter=None) -> str:
    typ = _go_type(p.type, emitter) if p.type is not None else "interface{}"
    if getattr(p, "variadic", False):
        return f"{p.name} ...{typ}"
    return f"{p.name} {typ}" if typ else p.name


def _go_string(s: str) -> str:
    return '"' + s.replace("\\", "\\\\").replace('"', '\\"').replace("\n", "\\n").replace("\t", "\\t") + '"'


def _go_field_name(name: str) -> str:
    """Zinc public fields are lowercase by convention; Go requires capitalization
    for cross-package access. Here we keep the Zinc name as-is — users can
    write `pub` which we don't currently translate to capitalisation, but
    within a single Go package lowercase works. Left as a hook for later."""
    return name


def _build_format(sil: ast.StringInterpLit, emit: Emitter) -> tuple[str, list[str]]:
    parts: list[str] = []
    args: list[str] = []
    for p in sil.parts:
        if isinstance(p, ast.StringLit):
            parts.append(p.value.replace("\\", "\\\\").replace('"', '\\"')
                         .replace("\n", "\\n").replace("\t", "\\t").replace("%", "%%"))
        else:
            parts.append("%v")
            args.append(emit._fmt_expr(p))
    return "".join(parts), args


def emit(prog: ast.Program) -> tuple[str, set[str]]:
    em = Emitter()
    source = em.emit_program(prog)
    return source, em.imports
