"""Parse tree → AST transformer.

Walks Lark's tree output and builds `zinc.ast` dataclasses. Kept deliberately
flat (one method per grammar rule) so missing rules surface as clear errors.
"""
from __future__ import annotations

import re

from lark import Transformer, v_args, Token, Tree

from zinc import ast


def _first(items, default=None):
    return items[0] if items else default


def _collect(items, cls):
    return [x for x in items if isinstance(x, cls)]


def _as_list(x):
    if x is None:
        return []
    if isinstance(x, list):
        return x
    return [x]


# --- Sentinel marker objects for grammar fragments that aren't AST nodes -----

class _Modifier(str):
    """Keyword-string wrapper so class_body / method_decl / field_decl can tell
    modifiers apart from NAME idents (which are plain strings)."""
    pass


class _Parents(list):
    pass


class _TypeSuffix:
    def __init__(self, kind: str, inner=None):
        self.kind = kind          # "array" or "optional"
        self.inner = inner


class _CallArgs:
    def __init__(self, positional=None, named=None, struct_fields=None, spread_last=False):
        self.positional = positional or []
        self.named = named or []
        self.struct_fields = struct_fields or []
        self.spread_last = spread_last


class _Annotations(list):
    pass


@v_args(inline=True)
class ZincTransformer(Transformer):

    # --- top-level -----------------------------------------------------------

    def start(self, program):
        return program

    def program(self, *items):
        prog = ast.Program()
        for it in items:
            if isinstance(it, ast.PackageDecl):
                prog.package = it
            elif isinstance(it, ast.ImportDecl):
                prog.imports.append(it)
            elif isinstance(it, (ast.ClassDecl, ast.DataClassDecl, ast.InterfaceDecl,
                                 ast.EnumDecl, ast.FnDecl, ast.ConstDecl,
                                 ast.TypeAliasDecl, ast.TestDecl)):
                prog.decls.append(it)
            else:
                # script-mode top-level statement
                prog.stmts.append(it)
        return prog

    def package_decl(self, dotted):
        return ast.PackageDecl(path=dotted)

    def import_decl(self, path, alias=None):
        path_str = path
        if isinstance(path, Token):
            if path.type == "STRING":
                path_str = _string_value(str(path))
            else:
                path_str = str(path)
        return ast.ImportDecl(path=path_str, alias=str(alias) if alias else "")

    def dotted_ident(self, *parts):
        return ".".join(str(p) for p in parts)

    # --- decls ---------------------------------------------------------------

    def class_decl(self, *items):
        annots = []
        modifiers = []
        name = ""
        type_params = []
        parents = []
        members = []
        i = 0
        while i < len(items):
            it = items[i]
            if isinstance(it, ast.Annotation):
                annots.append(it); i += 1
            elif isinstance(it, _Modifier):
                modifiers.append(str(it)); i += 1
            else:
                # the first non-annot non-modifier should be the class name (Token)
                break
        if i < len(items):
            name = str(items[i]); i += 1
        # Remaining items: optional type_params list, optional parents list, then member list
        for it in items[i:]:
            if isinstance(it, list) and it and isinstance(it[0], str) and not isinstance(it, _Parents):
                type_params = it
            elif isinstance(it, _Parents):
                parents = list(it)
            elif isinstance(it, list):
                members = it
        fields = [m for m in members if isinstance(m, ast.FieldDecl)]
        ctors = [m for m in members if isinstance(m, ast.CtorDecl)]
        methods = [m for m in members if isinstance(m, ast.MethodDecl)]
        variants = [m for m in members if isinstance(m, ast.DataClassDecl)]
        return ast.ClassDecl(
            name=name, fields=fields, ctor=ctors[0] if ctors else None,
            ctors=ctors, methods=methods, parents=parents,
            type_params=type_params,
            is_abstract="abstract" in modifiers,
            is_sealed="sealed" in modifiers or bool(variants),
            variants=variants,
            annotations=annots,
        )

    def class_modifier(self, kw):
        return _Modifier(str(kw))

    def parents(self, *types):
        # Keep full type info as formatted strings so generic parents
        # (`Mapper<int, String>`) emit as `Mapper[int, string]` in Go
        # rather than losing the type args to just `Mapper`.
        return _Parents([_format_type_for_parent(t) for t in types])

    @v_args(inline=False)
    def class_body(self, items):
        return list(items)

    def field_decl(self, *items):
        annots = []
        modifiers = []
        type_ref = None
        name = None
        default = None
        for it in items:
            if isinstance(it, ast.Annotation):
                annots.append(it)
            elif isinstance(it, _Modifier):
                modifiers.append(str(it))
            elif isinstance(it, (ast.SimpleType, ast.GenericType, ast.ArrayType,
                                  ast.OptionalType, ast.FuncTypeExpr)):
                type_ref = it
            elif isinstance(it, Token) and it.type == "NAME" and name is None:
                name = str(it)
            else:
                default = it
        return ast.FieldDecl(
            name=name, type=type_ref, default=default,
            is_pub="pub" in modifiers,
            is_readonly="readonly" in modifiers,
            is_const="const" in modifiers,
            is_init="init" in modifiers,
            is_static="static" in modifiers,
            annotations=annots,
        )

    def field_modifier(self, kw):
        return _Modifier(str(kw))

    def var_field_decl(self, *items):
        annots = []
        modifiers = []
        name = None
        value = None
        for it in items:
            if isinstance(it, ast.Annotation):
                annots.append(it)
            elif isinstance(it, _Modifier):
                modifiers.append(str(it))
            elif isinstance(it, Token) and it.type == "NAME" and name is None:
                name = str(it)
            else:
                value = it
        return ast.FieldDecl(
            name=name, type=None, default=value,
            is_pub="pub" in modifiers,
            is_readonly="readonly" in modifiers,
            is_const="const" in modifiers,
            is_init="init" in modifiers,
            is_static="static" in modifiers,
            annotations=annots,
        )

    def ctor_decl(self, *items):
        params = []
        body = None
        for it in items:
            if isinstance(it, list):
                params = it
            elif isinstance(it, ast.BlockStmt):
                body = it
        return ast.CtorDecl(params=params, body=body)

    def method_decl(self, *items):
        annots = []
        modifiers = []
        ret = None
        name = None
        params = []
        body = None
        expr_body = None
        for it in items:
            if isinstance(it, ast.Annotation):
                annots.append(it)
            elif isinstance(it, _Modifier):
                modifiers.append(str(it))
            elif isinstance(it, (ast.SimpleType, ast.GenericType, ast.ArrayType,
                                  ast.OptionalType, ast.FuncTypeExpr)) and name is None:
                ret = it
            elif isinstance(it, Token) and it.type == "NAME" and name is None:
                name = str(it)
            elif isinstance(it, list):
                params = it
            elif isinstance(it, ast.BlockStmt):
                body = it
            else:
                expr_body = it
        if ret is not None and _type_as_name(ret) == "void":
            ret = None
        return ast.MethodDecl(
            name=name, params=params, return_type=ret,
            body=body, expr_body=expr_body,
            is_pub="pub" in modifiers,
            is_static="static" in modifiers,
            is_abstract="abstract" in modifiers,
            is_override="override" in modifiers,
            annotations=annots,
        )

    def method_modifier(self, kw):
        return _Modifier(str(kw))

    def method_body(self, body=None):
        return body  # None for abstract

    def method_sig(self, *items):
        modifiers = []
        ret = None
        name = None
        params = []
        for it in items:
            if isinstance(it, _Modifier):
                modifiers.append(str(it))
            elif isinstance(it, (ast.SimpleType, ast.GenericType, ast.ArrayType,
                                  ast.OptionalType, ast.FuncTypeExpr)) and name is None:
                ret = it
            elif isinstance(it, Token) and it.type == "NAME" and name is None:
                name = str(it)
            elif isinstance(it, list):
                params = it
        if ret is not None and _type_as_name(ret) == "void":
            ret = None
        return ast.MethodSig(
            name=name, params=params, return_type=ret,
            is_pub="pub" in modifiers,
        )

    def data_class_decl(self, *items):
        annots = []
        name = None
        type_params = []
        params = []
        parents = []
        methods = []
        for it in items:
            if isinstance(it, ast.Annotation):
                annots.append(it)
            elif isinstance(it, Token) and it.type == "NAME" and name is None:
                name = str(it)
            elif isinstance(it, _Parents):
                parents = list(it)
            elif isinstance(it, list) and it and isinstance(it[0], ast.FieldDecl):
                params = it
            elif isinstance(it, list) and it and isinstance(it[0], ast.MethodDecl):
                methods = it
            elif isinstance(it, list) and all(isinstance(x, str) for x in it):
                type_params = it
        return ast.DataClassDecl(
            name=name, params=params, methods=methods,
            parents=parents, type_params=type_params,
        )

    @v_args(inline=False)
    def data_body(self, items):
        return list(items)

    @v_args(inline=False)
    def field_params(self, items):
        return list(items)

    def field_param(self, *items):
        modifiers = []
        type_ref = None
        name = None
        default = None
        for it in items:
            if isinstance(it, _Modifier):
                modifiers.append(str(it))
            elif isinstance(it, (ast.SimpleType, ast.GenericType, ast.ArrayType,
                                  ast.OptionalType, ast.FuncTypeExpr)) and name is None:
                type_ref = it
            elif isinstance(it, Token) and it.type == "NAME" and name is None:
                name = str(it)
            else:
                default = it
        return ast.FieldDecl(
            name=name, type=type_ref, default=default,
            is_pub="pub" in modifiers,
            is_readonly="readonly" in modifiers,
            is_const="const" in modifiers,
            is_init="init" in modifiers,
            is_static="static" in modifiers,
        )

    def interface_decl(self, *items):
        name = None
        type_params = []
        sigs = []
        for it in items:
            if isinstance(it, Token) and it.type == "NAME" and name is None:
                name = str(it)
            elif isinstance(it, list):
                type_params = it
            elif isinstance(it, ast.MethodSig):
                sigs.append(it)
        return ast.InterfaceDecl(name=name, methods=sigs, type_params=type_params)

    def enum_decl(self, name, *variants):
        return ast.EnumDecl(name=str(name), variants=[str(v) for v in variants])

    def enum_variant(self, name):
        return str(name)

    def fn_decl(self, *items):
        annots = []
        modifiers = []
        ret = None
        name = None
        type_params = []
        params = []
        body = None
        expr_body = None
        saw_name = False
        for it in items:
            if isinstance(it, ast.Annotation):
                annots.append(it)
            elif isinstance(it, _Modifier):
                modifiers.append(str(it))
            elif isinstance(it, (ast.SimpleType, ast.GenericType, ast.ArrayType,
                                  ast.OptionalType, ast.FuncTypeExpr)) and not saw_name:
                ret = it
            elif isinstance(it, Token) and it.type == "NAME" and not saw_name:
                name = str(it)
                saw_name = True
            elif isinstance(it, list) and it and all(isinstance(x, str) for x in it):
                type_params = it
            elif isinstance(it, list):
                params = it
            elif isinstance(it, ast.BlockStmt):
                body = it
            else:
                expr_body = it
        if ret is not None and _type_as_name(ret) == "void":
            ret = None
        return ast.FnDecl(
            name=name, params=params, return_type=ret,
            body=body, expr_body=expr_body,
            is_pub="pub" in modifiers,
            type_params=type_params,
            annotations=annots,
        )

    def fn_modifier(self, kw):
        return _Modifier(str(kw))

    def const_decl(self, *items):
        is_pub = False
        type_ref = None
        name = None
        value = None
        for it in items:
            if isinstance(it, _Modifier) and str(it) == "pub":
                is_pub = True
            elif isinstance(it, (ast.SimpleType, ast.GenericType, ast.ArrayType,
                                  ast.OptionalType, ast.FuncTypeExpr)):
                type_ref = it
            elif isinstance(it, Token) and it.type == "NAME" and name is None:
                name = str(it)
            else:
                value = it
        return ast.ConstDecl(name=name, value=value, type=type_ref, is_pub=is_pub)

    def type_alias_decl(self, name, type_ref):
        return ast.TypeAliasDecl(name=str(name), type=type_ref)

    def test_decl(self, name_tok, body):
        return ast.TestDecl(name=_string_value(str(name_tok)), body=body)

    def annotation(self, name, *args):
        return ast.Annotation(name=str(name), args=list(args) if args else [])

    def annotation_args(self, *args):
        return [_string_value(str(a)) for a in args]

    @v_args(inline=False)
    def params(self, items):
        return list(items)

    def param(self, *items):
        is_const = False
        type_ref = None
        variadic = False
        name = None
        default = None
        for it in items:
            if isinstance(it, _Modifier) and str(it) == "const":
                is_const = True
            elif isinstance(it, Token) and it.type == "VARIADIC_MARK":
                variadic = True
            elif isinstance(it, (ast.SimpleType, ast.GenericType, ast.ArrayType,
                                  ast.OptionalType, ast.FuncTypeExpr)) and name is None:
                type_ref = it
            elif isinstance(it, Token) and it.type == "NAME" and name is None:
                name = str(it)
            else:
                default = it
        return ast.ParamDecl(
            name=name, type=type_ref, default=default,
            variadic=variadic, is_const=is_const,
        )

    def type_params(self, *names):
        return [str(n) for n in names]

    # --- types ---------------------------------------------------------------

    def func_type(self, *items):
        params = []
        ret = None
        for it in items:
            if isinstance(it, list):
                params = it
            else:
                if params and ret is None and isinstance(it, (ast.SimpleType, ast.GenericType,
                                                              ast.ArrayType, ast.OptionalType,
                                                              ast.FuncTypeExpr)):
                    ret = it
        return ast.FuncTypeExpr(params=params, return_type=ret)

    @v_args(inline=False)
    def type_list(self, items):
        return list(items)

    @v_args(inline=False)
    def type_args(self, items):
        return list(items)

    def type_ref(self, *items):
        # Handles: _type_base followed by type_suffix*
        # _type_base is inlined (starts with "_"), so we see: dotted_ident or (dotted_ident, type_args)
        if len(items) == 1 and isinstance(items[0], (ast.SimpleType, ast.GenericType,
                                                      ast.ArrayType, ast.OptionalType,
                                                      ast.FuncTypeExpr)):
            return items[0]
        base = None
        type_args = None
        suffixes = []
        for it in items:
            if isinstance(it, _TypeSuffix):
                suffixes.append(it)
            elif isinstance(it, list) and it and isinstance(it[0], (ast.SimpleType, ast.GenericType,
                                                                     ast.ArrayType, ast.OptionalType,
                                                                     ast.FuncTypeExpr)):
                type_args = it
            elif isinstance(it, str):
                base = it
            elif isinstance(it, Token):
                base = str(it)
        if base is None:
            return items[0]
        typ = ast.GenericType(name=base, type_args=type_args) if type_args else ast.SimpleType(name=base)
        for s in suffixes:
            if s.kind == "array":
                typ = ast.ArrayType(element_type=typ)
            elif s.kind == "optional":
                typ = ast.OptionalType(inner=typ)
        return typ

    def array_suffix(self):
        return _TypeSuffix("array")

    def optional_suffix(self):
        return _TypeSuffix("optional")

    # --- statements ----------------------------------------------------------

    @v_args(inline=False)
    def block(self, items):
        return ast.BlockStmt(stmts=list(items))

    def var_stmt(self, *items):
        # Forms:
        #   "var" type_ref? NAME ("=" expr)? or_handler?
        #   "const" type_ref? NAME "=" expr
        #   type_ref NAME "=" expr or_handler?
        is_const = False
        type_ref = None
        name = None
        value = None
        or_h = None
        # Check keyword-first forms by spying on the first token
        # Lark doesn't pass literal string tokens through to us, so we detect
        # "const" form via shape: 2-3 args where first is a type_ref or name.
        # The "is_const" distinction is important for codegen.
        # Workaround: we infer const-vs-var from context — left as False unless
        # the grammar explicitly flags it. (See const_decl for top-level consts.)
        for it in items:
            if isinstance(it, (ast.SimpleType, ast.GenericType, ast.ArrayType,
                                ast.OptionalType, ast.FuncTypeExpr)) and name is None:
                type_ref = it
            elif isinstance(it, Token) and it.type == "NAME" and name is None:
                name = str(it)
            elif isinstance(it, ast.OrHandler):
                or_h = it
            else:
                value = it
        return ast.VarStmt(
            name=name, value=value, type=type_ref,
            is_const=is_const, or_handler=or_h,
        )

    def tuple_var_stmt(self, *items):
        names = []
        value = None
        or_h = None
        for it in items:
            if isinstance(it, Token) and it.type == "NAME":
                names.append(str(it))
            elif isinstance(it, ast.OrHandler):
                or_h = it
            else:
                value = it
        return ast.TupleVarStmt(names=names, value=value, or_handler=or_h)

    def return_stmt(self, inner):
        return inner

    def return_with_value(self, value):
        return ast.ReturnStmt(value=value)

    def return_void(self):
        return ast.ReturnStmt(value=None)

    def if_stmt(self, *items):
        cond = items[0]
        then = items[1]
        else_ = items[2] if len(items) > 2 else None
        return ast.IfStmt(cond=cond, then=then, else_=else_)

    def for_stmt(self, *items):
        # Three shapes:
        #   for (header) block                    — C-style
        #   for NAME "in" expr block              — range-style, single var
        #   for (NAME, NAME) "in" expr block      — range-style, (index, item)
        if len(items) == 2:
            header, body = items
            if isinstance(header, _ForHeader):
                return ast.ForStmt(
                    init=header.init, cond=header.cond, post=header.post, body=body,
                )
        # Range-style
        if len(items) == 3:
            name_tok, range_expr, body = items
            return ast.ForStmt(
                is_range=True, item=str(name_tok), range_expr=range_expr, body=body,
            )
        if len(items) == 4:
            idx_tok, item_tok, range_expr, body = items
            return ast.ForStmt(
                is_range=True, index_var=str(idx_tok), item=str(item_tok),
                range_expr=range_expr, body=body,
            )
        raise ValueError(f"unhandled for_stmt arity: {len(items)}")

    def for_header(self, *items):
        # init? ; cond? ; post?
        init = items[0] if len(items) > 0 else None
        cond = items[1] if len(items) > 1 else None
        post = items[2] if len(items) > 2 else None
        return _ForHeader(init=init, cond=cond, post=post)

    def while_stmt(self, cond, body):
        return ast.WhileStmt(cond=cond, body=body)

    def match_stmt(self, subject, *cases):
        return ast.MatchStmt(subject=subject, cases=list(cases))

    def match_case(self, pattern, body):
        # "_" -> body  (pattern is a Token for the underscore rule)
        if isinstance(pattern, Token) and str(pattern) == "_":
            return ast.MatchCase(pattern=None, body=body)
        return ast.MatchCase(pattern=pattern, body=body)

    def match_pattern(self, expr):
        return expr

    def break_stmt(self):
        return ast.BreakStmt()

    def continue_stmt(self):
        return ast.ContinueStmt()

    def throw_stmt(self, value):
        return ast.ThrowStmt(value=value)

    def try_stmt(self, body, *rest):
        catches = [r for r in rest if isinstance(r, ast.CatchClause)]
        finally_ = next((r.body for r in rest if isinstance(r, _FinallyClause)), None)
        return ast.TryStmt(body=body, catches=catches, finally_=finally_)

    def catch_clause(self, *items):
        exc_type = None
        var_name = ""
        body = None
        for it in items:
            if isinstance(it, (ast.SimpleType, ast.GenericType, ast.ArrayType,
                                ast.OptionalType, ast.FuncTypeExpr)):
                exc_type = it
            elif isinstance(it, Token) and it.type == "NAME":
                var_name = str(it)
            elif isinstance(it, ast.BlockStmt):
                body = it
        return ast.CatchClause(exception_type=exc_type, var_name=var_name, body=body)

    def finally_clause(self, body):
        return _FinallyClause(body=body)

    def with_stmt(self, *items):
        *resources, body = items
        return ast.WithStmt(resources=list(resources), body=body)

    def with_resource(self, *items):
        if len(items) == 1:
            # bare expression resource
            return ast.WithResource(name="", value=items[0])
        name = None
        value = None
        or_h = None
        for it in items:
            if isinstance(it, Token) and it.type == "NAME":
                name = str(it)
            elif isinstance(it, ast.OrHandler):
                or_h = it
            else:
                value = it
        return ast.WithResource(name=name or "", value=value, or_handler=or_h)

    def defer_stmt(self, expr):
        return ast.DeferStmt(expr=expr)

    def print_stmt(self, value):
        return ast.PrintStmt(value=value)

    def assert_stmt(self, cond, message=None):
        return ast.AssertStmt(cond=cond, message=message)

    def parallel_for_stmt(self, *items):
        # optional max expr + NAME + range + block + or_handler?
        max_expr = None
        item_name = None
        range_expr = None
        body = None
        or_h = None
        for it in items:
            if isinstance(it, Token) and it.type == "NAME" and item_name is None:
                item_name = str(it)
            elif isinstance(it, ast.BlockStmt):
                body = it
            elif isinstance(it, ast.OrHandler):
                or_h = it
            elif range_expr is None and max_expr is None and item_name is None:
                max_expr = it
            elif range_expr is None:
                range_expr = it
            else:
                range_expr = it
        return ast.ParallelForStmt(
            item=item_name or "", range_expr=range_expr, body=body,
            max=max_expr, or_handler=or_h,
        )

    def concurrent_stmt(self, *items):
        first_only = False
        tasks = []
        or_h = None
        for it in items:
            if isinstance(it, ast.OrHandler):
                or_h = it
            elif isinstance(it, ast.BoolLit):
                first_only = it.value
            else:
                tasks.append(it)
        return ast.ConcurrentStmt(tasks=tasks, first_only=first_only, or_handler=or_h)

    def timeout_stmt(self, duration, body, or_h=None):
        return ast.TimeoutStmt(duration=duration, body=body, or_handler=or_h)

    def spawn_stmt(self, body, or_h=None):
        return ast.SpawnStmt(body=body, or_handler=or_h)

    def or_handler(self, *items):
        # Three shapes: block | short-default expr | "or" "match" NAME block
        if len(items) == 1:
            it = items[0]
            if isinstance(it, ast.BlockStmt):
                return ast.OrHandler(body=it)
            return ast.OrHandler(default_value=it)
        if len(items) == 2:
            var_tok, body = items
            return ast.OrHandler(match_var=str(var_tok), body=body)
        return ast.OrHandler()

    def assign_or_expr_stmt(self, *items):
        if len(items) == 1:
            return ast.ExprStmt(expr=items[0])
        target = items[0]
        op = items[1] if isinstance(items[1], str) else str(items[1])
        value = items[2]
        or_h = items[3] if len(items) > 3 else None
        return ast.AssignStmt(target=target, op=op, value=value, or_handler=or_h)

    def assign_op(self, op):
        return str(op)

    # stmt_expr family — parallel to expression grammar but restricted to
    # valid statement-leading shapes. Each stmt_postfix rule emits a small
    # "suffix marker" that stmt_expr folds into the left-hand expression.

    def stmt_expr(self, first, *suffixes):
        expr = first
        for s in suffixes:
            expr = s(expr)
        return expr

    def stmt_ident(self, tok):
        return ast.Ident(name=str(tok))

    def stmt_this(self):
        return ast.ThisExpr()

    def stmt_super(self):
        return ast.Ident(name="super")

    def stmt_selector(self, name):
        return lambda obj: ast.SelectorExpr(object=obj, field=str(name))

    def stmt_safe_nav(self, name):
        return lambda obj: ast.SafeNavExpr(object=obj, field=str(name))

    def stmt_call(self, call_args=None):
        ca = call_args or _CallArgs()
        def apply(obj):
            return ast.CallExpr(
                callee=obj, args=ca.positional,
                named_args=ca.named, struct_field_args=ca.struct_fields,
            )
        return apply

    def stmt_call_typed(self, type_args, call_args=None):
        ca = call_args or _CallArgs()
        def apply(obj):
            return ast.CallExpr(
                callee=obj, args=ca.positional,
                named_args=ca.named, struct_field_args=ca.struct_fields,
                type_args=type_args,
            )
        return apply

    def stmt_index(self, idx):
        return lambda obj: ast.IndexExpr(object=obj, index=idx)

    def stmt_propagate(self):
        return lambda obj: ast.PropagateExpr(inner=obj)

    # --- expressions ---------------------------------------------------------

    def if_expr(self, cond, then, else_):
        return ast.IfExpr(cond=cond, then=then, else_=else_)

    def type_cast(self, obj, type_ref):
        # `x as T` — Zinc's type-cast/narrowing operator.
        type_name = _type_as_name(type_ref) or "Object"
        return ast.TypeAssertExpr(object=obj, type_name=type_name, is_check=False)

    def bin_or(self, left, op_tok, right):
        # Zinc accepts both `||` and `or` — both emit Go's `||`.
        return ast.BinaryExpr(left=left, op="||", right=right)

    def bin_and(self, left, op_tok, right):
        return ast.BinaryExpr(left=left, op="&&", right=right)

    def not_(self, operand):
        return ast.UnaryExpr(op="!", operand=operand)

    def bit_or(self, left, right):
        return ast.BinaryExpr(left=left, op="|", right=right)

    def bit_xor(self, left, right):
        return ast.BinaryExpr(left=left, op="^", right=right)

    def bit_and(self, left, right):
        return ast.BinaryExpr(left=left, op="&", right=right)

    def bin_cmp(self, left, op, right):
        return ast.BinaryExpr(left=left, op=str(op), right=right)

    def comp_op(self, op):
        return str(op)

    def range_(self, start, op, end):
        return ast.RangeExpr(start=start, end=end, inclusive=str(op) == "..=")

    def bin_addsub(self, left, op, right):
        return ast.BinaryExpr(left=left, op=str(op), right=right)

    def bin_shift(self, left, op, right):
        return ast.BinaryExpr(left=left, op=str(op), right=right)

    def bin_muldiv(self, left, op, right):
        return ast.BinaryExpr(left=left, op=str(op), right=right)

    def neg(self, operand):
        return ast.UnaryExpr(op="-", operand=operand)

    def not_bang(self, operand):
        return ast.UnaryExpr(op="!", operand=operand)

    def power(self, base, exponent):
        return ast.BinaryExpr(left=base, op="**", right=exponent)

    # postfix
    def selector(self, obj, name):
        return ast.SelectorExpr(object=obj, field=str(name))

    def safe_nav(self, obj, name):
        return ast.SafeNavExpr(object=obj, field=str(name))

    def call(self, callee, call_args=None):
        ca = call_args or _CallArgs()
        return ast.CallExpr(
            callee=callee,
            args=ca.positional,
            named_args=ca.named,
            struct_field_args=ca.struct_fields,
        )

    def call_typed(self, callee, type_args, call_args=None):
        ca = call_args or _CallArgs()
        return ast.CallExpr(
            callee=callee,
            args=ca.positional,
            named_args=ca.named,
            struct_field_args=ca.struct_fields,
            type_args=type_args,
        )

    def typed_list_lit(self, callee, type_args, *elements):
        name = _expr_as_name(callee)
        etype = ast.GenericType(name=name, type_args=type_args) if name else None
        return ast.ListLit(elements=list(elements), explicit_type=etype)

    def typed_map_lit(self, callee, type_args, *entries):
        keys, values = [], []
        for k, v in entries:
            keys.append(k); values.append(v)
        name = _expr_as_name(callee)
        etype = ast.GenericType(name=name, type_args=type_args) if name else None
        return ast.MapLit(keys=keys, values=values, explicit_type=etype)

    def index(self, obj, idx):
        return ast.IndexExpr(object=obj, index=idx)

    def slice(self, obj, *bounds):
        low, high = None, None
        # Varies: could be (obj,) for [:], (obj, low) for [low:], etc.
        # Simple mapping: first = low if present, second = high if present.
        if len(bounds) >= 1:
            low = bounds[0]
        if len(bounds) >= 2:
            high = bounds[1]
        return ast.SliceExpr(object=obj, low=low, high=high)

    def propagate(self, inner):
        return ast.PropagateExpr(inner=inner)

    # call_args
    def call_args(self, *args):
        positional = []
        named = []
        struct_fields = []
        for a in args:
            if isinstance(a, ast.NamedArg):
                named.append(a)
            elif isinstance(a, ast.StructFieldArg):
                struct_fields.append(a)
            else:
                positional.append(a)
        return _CallArgs(positional=positional, named=named, struct_fields=struct_fields)

    def named_arg(self, name, value):
        return ast.NamedArg(name=str(name), value=value)

    def struct_field_arg(self, name, value):
        return ast.StructFieldArg(name=str(name), value=value)

    def spread_arg(self, expr):
        return ast.SpreadExpr(expr=expr)

    def positional_arg(self, expr):
        return expr

    # primary
    def int_lit(self, tok):
        return ast.IntLit(value=str(tok))

    def float_lit(self, tok):
        return ast.FloatLit(value=str(tok))

    def string_lit(self, tok):
        raw = str(tok)
        value = _string_value(raw)
        # Detect interpolation: ${...} in a double-quoted string.
        if raw.startswith('"') and "${" in value:
            return _parse_interpolation(value)
        return ast.StringLit(value=value)

    def interp_string_lit(self, tok):
        return _parse_interpolation(_string_value(str(tok)))

    def raw_string_lit(self, tok):
        s = str(tok)
        return ast.RawStringLit(value=s[1:-1])

    def bool_lit(self, tok):
        return ast.BoolLit(value=str(tok) == "true")

    def null_lit(self):
        return ast.NullLit()

    def this_(self):
        return ast.ThisExpr()

    def super_call(self, call_args=None):
        ca = call_args or _CallArgs()
        return ast.SuperCallExpr(args=ca.positional)

    def new_expr(self, callee):
        if isinstance(callee, ast.CallExpr):
            callee.is_new = True
            return callee
        return ast.CallExpr(callee=callee, is_new=True)

    @v_args(inline=False)
    def list_lit(self, items):
        return ast.ListLit(elements=list(items))

    @v_args(inline=False)
    def map_lit(self, items):
        keys, values = [], []
        for k, v in items:
            keys.append(k); values.append(v)
        return ast.MapLit(keys=keys, values=values)

    def map_entry(self, key, value):
        return (key, value)

    def tuple_or_paren(self, *exprs):
        if len(exprs) == 1:
            return exprs[0]
        return ast.TupleLit(elements=list(exprs))

    def lambda_expr(self, *items):
        params = []
        ret_type = None
        body = None
        expr = None
        for it in items:
            if isinstance(it, Token) and it.type == "NAME" and not params:
                # single-param shorthand: NAME "->" body
                params = [ast.ParamDecl(name=str(it))]
            elif isinstance(it, list):
                params = it
            elif isinstance(it, (ast.SimpleType, ast.GenericType, ast.ArrayType,
                                  ast.OptionalType, ast.FuncTypeExpr)) and body is None and expr is None:
                ret_type = it
            elif isinstance(it, ast.BlockStmt):
                body = it
            else:
                expr = it
        return ast.LambdaExpr(params=params, body=body, expr=expr, return_type=ret_type)

    @v_args(inline=False)
    def lambda_params(self, items):
        return list(items)

    def lambda_param(self, *items):
        type_ref = None
        name = None
        default = None
        for it in items:
            if isinstance(it, (ast.SimpleType, ast.GenericType, ast.ArrayType,
                                ast.OptionalType, ast.FuncTypeExpr)) and name is None:
                type_ref = it
            elif isinstance(it, Token) and it.type == "NAME" and name is None:
                name = str(it)
            else:
                default = it
        return ast.ParamDecl(name=name, type=type_ref, default=default)

    def match_expr(self, subject, *cases):
        return ast.MatchExpr(subject=subject, cases=list(cases))

    def match_expr_case(self, pattern, value):
        if isinstance(pattern, Token) and str(pattern) == "_":
            return ast.MatchExprCase(pattern=None, value=value)
        return ast.MatchExprCase(pattern=pattern, value=value)

    def spawn_expr(self, body, or_h=None):
        return ast.SpawnExpr(body=body, or_handler=or_h)

    def ident(self, tok):
        return ast.Ident(name=str(tok))


# --- Helpers -----------------------------------------------------------------

class _ForHeader:
    def __init__(self, init=None, cond=None, post=None):
        self.init = init; self.cond = cond; self.post = post


class _FinallyClause:
    def __init__(self, body):
        self.body = body


def _type_as_name(t) -> str:
    if isinstance(t, ast.SimpleType):
        return t.name
    if isinstance(t, ast.GenericType):
        return t.name
    return ""


_TYPE_MAP = {
    "int": "int", "long": "int64", "float": "float32", "double": "float64",
    "bool": "bool", "boolean": "bool", "byte": "byte", "char": "rune",
    "string": "string", "String": "string", "void": "",
}


def _format_type_for_parent(t) -> str:
    """Render a parent type_ref to a Go-friendly type string, preserving
    generic args: `Mapper<int, String>` → `Mapper[int, string]`."""
    if isinstance(t, ast.SimpleType):
        return _TYPE_MAP.get(t.name, t.name)
    if isinstance(t, ast.GenericType):
        args = ", ".join(_format_type_for_parent(a) for a in t.type_args)
        return f"{t.name}[{args}]"
    if isinstance(t, ast.ArrayType):
        return f"[]{_format_type_for_parent(t.element_type)}"
    return "interface{}"


def _expr_as_name(e) -> str:
    if isinstance(e, ast.Ident):
        return e.name
    if isinstance(e, ast.SelectorExpr):
        return _expr_as_name(e.object) + "." + e.field
    return ""


def _string_value(raw: str) -> str:
    """Decode a string literal.  The lexer preserves the surrounding quotes
    and raw escapes; this function strips the quotes and decodes common
    escape sequences without touching `${…}` interpolation placeholders."""
    if raw.startswith('"""') and raw.endswith('"""'):
        return raw[3:-3]
    if raw.startswith("'''") and raw.endswith("'''"):
        return raw[3:-3]
    if raw.startswith('"') and raw.endswith('"'):
        body = raw[1:-1]
    elif raw.startswith("'") and raw.endswith("'"):
        body = raw[1:-1]
    else:
        return raw
    out = []
    i = 0
    while i < len(body):
        ch = body[i]
        if ch == "\\" and i + 1 < len(body):
            nxt = body[i + 1]
            mapping = {"n": "\n", "t": "\t", "r": "\r", '"': '"', "'": "'", "\\": "\\"}
            out.append(mapping.get(nxt, "\\" + nxt))
            i += 2
        else:
            out.append(ch)
            i += 1
    return "".join(out)


def _find_interp_ranges(s: str) -> list[tuple[int, int, str]]:
    """Return [(start, end, inner_expr_text), ...] for each ${…} in `s`.
    Hand-rolled scanner (replaces the previous regex) that:
      - tracks brace depth so nested `{}` inside the expression works
        (e.g. `${foo({a: 1})}`);
      - respects nested `"..."` and `'...'` and backtick strings so a
        `"-"` inside `${...}` doesn't prematurely close the outer.
    """
    out: list[tuple[int, int, str]] = []
    i, n = 0, len(s)
    while i < n:
        if s[i] == "$" and i + 1 < n and s[i + 1] == "{":
            start = i
            j = i + 2
            depth = 1
            while j < n and depth > 0:
                ch = s[j]
                if ch in ('"', "'", "`"):
                    # Skip past the nested string literal so its contents
                    # don't affect our brace counting.
                    quote = ch
                    j += 1
                    while j < n and s[j] != quote:
                        if s[j] == "\\" and j + 1 < n:
                            j += 2
                            continue
                        j += 1
                    if j < n:
                        j += 1
                    continue
                if ch == "{":
                    depth += 1
                elif ch == "}":
                    depth -= 1
                    if depth == 0:
                        out.append((start, j + 1, s[start + 2:j]))
                        j += 1
                        break
                j += 1
            i = j
        else:
            i += 1
    return out


def _parse_interpolation(s: str) -> ast.StringInterpLit:
    """Split a string with ${...} placeholders into alternating StringLit
    and (parsed) expression parts."""
    from zinc.parser import _get_parser

    parts: list[ast.Expr] = []
    i = 0
    for start, end, inner in _find_interp_ranges(s):
        pre = s[i:start]
        if pre:
            parts.append(ast.StringLit(value=pre))
        try:
            tree = _get_parser().parse(f"var _interp = {inner}")
            expr_ast = ZincTransformer().transform(tree)
            var_stmt = expr_ast.stmts[0] if expr_ast.stmts else expr_ast.decls[0]
            parts.append(var_stmt.value)
        except Exception:
            parts.append(ast.Ident(name=inner.strip()))
        i = end
    tail = s[i:]
    if tail:
        parts.append(ast.StringLit(value=tail))
    if not parts:
        parts.append(ast.StringLit(value=""))
    return ast.StringInterpLit(parts=parts)
