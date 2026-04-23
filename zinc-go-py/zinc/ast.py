"""Zinc AST node types.

Dataclasses mirroring zinc-go/internal/parser/ast.go for easy cross-reference.
Fields carry only the information codegen needs — no Line/Pos plumbing yet.
"""
from __future__ import annotations

from dataclasses import dataclass, field
from typing import Optional, Union

# --- Top-level ---------------------------------------------------------------

@dataclass
class Program:
    package: Optional["PackageDecl"] = None
    imports: list["ImportDecl"] = field(default_factory=list)
    decls: list["TopLevelDecl"] = field(default_factory=list)
    stmts: list["Stmt"] = field(default_factory=list)      # script-mode top-level
    source_file: str = ""

@dataclass
class PackageDecl:
    path: str

@dataclass
class ImportDecl:
    path: str
    alias: str = ""

# --- Declarations ------------------------------------------------------------

@dataclass
class Annotation:
    name: str
    args: list[str] = field(default_factory=list)

@dataclass
class ParamDecl:
    name: str
    type: Optional["TypeExpr"] = None
    default: Optional["Expr"] = None
    variadic: bool = False
    is_const: bool = False

@dataclass
class FieldDecl:
    name: str
    type: Optional["TypeExpr"]
    default: Optional["Expr"] = None
    is_pub: bool = False
    is_readonly: bool = False
    is_const: bool = False
    is_init: bool = False
    is_static: bool = False
    annotations: list[Annotation] = field(default_factory=list)

@dataclass
class FnDecl:
    name: str
    params: list[ParamDecl] = field(default_factory=list)
    return_type: Optional["TypeExpr"] = None     # None = void
    body: Optional["BlockStmt"] = None
    expr_body: Optional["Expr"] = None           # single-expression body
    is_pub: bool = False
    type_params: list[str] = field(default_factory=list)
    annotations: list[Annotation] = field(default_factory=list)

@dataclass
class CtorDecl:
    params: list[ParamDecl]
    body: "BlockStmt"

@dataclass
class MethodDecl:
    name: str
    params: list[ParamDecl] = field(default_factory=list)
    return_type: Optional["TypeExpr"] = None
    body: Optional["BlockStmt"] = None
    expr_body: Optional["Expr"] = None
    is_pub: bool = False
    is_static: bool = False
    is_abstract: bool = False
    is_override: bool = False
    annotations: list[Annotation] = field(default_factory=list)

@dataclass
class MethodSig:
    name: str
    params: list[ParamDecl] = field(default_factory=list)
    return_type: Optional["TypeExpr"] = None
    is_pub: bool = False

@dataclass
class ClassDecl:
    name: str
    fields: list[FieldDecl] = field(default_factory=list)
    ctor: Optional[CtorDecl] = None
    ctors: list[CtorDecl] = field(default_factory=list)
    methods: list[MethodDecl] = field(default_factory=list)
    parents: list[str] = field(default_factory=list)
    type_params: list[str] = field(default_factory=list)
    is_abstract: bool = False
    is_sealed: bool = False
    variants: list["DataClassDecl"] = field(default_factory=list)
    annotations: list[Annotation] = field(default_factory=list)

@dataclass
class DataClassDecl:
    name: str
    params: list[FieldDecl] = field(default_factory=list)
    methods: list[MethodDecl] = field(default_factory=list)
    parents: list[str] = field(default_factory=list)
    type_params: list[str] = field(default_factory=list)

@dataclass
class InterfaceDecl:
    name: str
    methods: list[MethodSig] = field(default_factory=list)
    type_params: list[str] = field(default_factory=list)

@dataclass
class EnumDecl:
    name: str
    variants: list[str] = field(default_factory=list)

@dataclass
class ConstDecl:
    name: str
    value: "Expr"
    type: Optional["TypeExpr"] = None
    is_pub: bool = False

@dataclass
class TypeAliasDecl:
    name: str
    type: "TypeExpr"

@dataclass
class TestDecl:
    name: str
    body: "BlockStmt"

TopLevelDecl = Union[
    ClassDecl, DataClassDecl, InterfaceDecl, EnumDecl, FnDecl, ConstDecl,
    TypeAliasDecl, TestDecl
]

# --- Types -------------------------------------------------------------------

@dataclass
class SimpleType:
    name: str

@dataclass
class GenericType:
    name: str
    type_args: list["TypeExpr"] = field(default_factory=list)

@dataclass
class ArrayType:
    element_type: "TypeExpr"

@dataclass
class OptionalType:
    inner: "TypeExpr"

@dataclass
class FuncTypeExpr:
    params: list["TypeExpr"] = field(default_factory=list)
    return_type: Optional["TypeExpr"] = None

TypeExpr = Union[SimpleType, GenericType, ArrayType, OptionalType, FuncTypeExpr]

# --- Statements --------------------------------------------------------------

@dataclass
class BlockStmt:
    stmts: list["Stmt"] = field(default_factory=list)

@dataclass
class VarStmt:
    name: str
    value: Optional["Expr"] = None
    type: Optional[TypeExpr] = None
    is_const: bool = False
    or_handler: Optional["OrHandler"] = None

@dataclass
class TupleVarStmt:
    names: list[str]
    value: "Expr"
    or_handler: Optional["OrHandler"] = None

@dataclass
class AssignStmt:
    target: "Expr"
    op: str
    value: "Expr"
    or_handler: Optional["OrHandler"] = None

@dataclass
class ReturnStmt:
    value: Optional["Expr"] = None

@dataclass
class IfStmt:
    cond: "Expr"
    then: BlockStmt
    else_: Optional["Stmt"] = None       # BlockStmt or IfStmt

@dataclass
class ForStmt:
    body: BlockStmt
    # C-style:
    init: Optional["Stmt"] = None
    cond: Optional["Expr"] = None
    post: Optional["Stmt"] = None
    # Range-style:
    is_range: bool = False
    index_var: str = ""
    item: str = ""
    range_expr: Optional["Expr"] = None

@dataclass
class WhileStmt:
    cond: "Expr"
    body: BlockStmt

@dataclass
class PrintStmt:
    value: "Expr"

@dataclass
class ExprStmt:
    expr: "Expr"
    or_handler: Optional["OrHandler"] = None

@dataclass
class BreakStmt:
    pass

@dataclass
class ContinueStmt:
    pass

@dataclass
class ThrowStmt:
    value: "Expr"

@dataclass
class DeferStmt:
    expr: "Expr"

@dataclass
class AssertStmt:
    cond: "Expr"
    message: Optional["Expr"] = None

@dataclass
class CatchClause:
    exception_type: Optional[TypeExpr]
    var_name: str
    body: BlockStmt

@dataclass
class TryStmt:
    body: BlockStmt
    catches: list[CatchClause] = field(default_factory=list)
    finally_: Optional[BlockStmt] = None

@dataclass
class WithResource:
    name: str
    value: "Expr"
    or_handler: Optional["OrHandler"] = None

@dataclass
class WithStmt:
    resources: list[WithResource]
    body: BlockStmt

@dataclass
class MatchCase:
    pattern: Optional["Expr"]          # None = wildcard
    body: BlockStmt

@dataclass
class MatchStmt:
    subject: "Expr"
    cases: list[MatchCase] = field(default_factory=list)

@dataclass
class ParallelForStmt:
    item: str
    range_expr: "Expr"
    body: BlockStmt
    index_var: str = ""
    max: Optional["Expr"] = None
    or_handler: Optional["OrHandler"] = None

@dataclass
class ConcurrentStmt:
    tasks: list["Expr"]
    first_only: bool = False
    names: list[str] = field(default_factory=list)
    or_handler: Optional["OrHandler"] = None

@dataclass
class TimeoutStmt:
    duration: "Expr"
    body: BlockStmt
    or_handler: Optional["OrHandler"] = None

@dataclass
class SpawnStmt:
    body: BlockStmt
    or_handler: Optional["OrHandler"] = None

@dataclass
class OrHandler:
    body: Optional[BlockStmt] = None
    default_value: Optional["Expr"] = None
    match_var: str = ""
    match_cases: list["OrMatchCase"] = field(default_factory=list)

@dataclass
class OrMatchCase:
    type_name: str
    body: BlockStmt

Stmt = Union[
    BlockStmt, VarStmt, TupleVarStmt, AssignStmt, ReturnStmt, IfStmt, ForStmt,
    WhileStmt, PrintStmt, ExprStmt, BreakStmt, ContinueStmt, ThrowStmt,
    DeferStmt, AssertStmt, TryStmt, WithStmt, MatchStmt, ParallelForStmt,
    ConcurrentStmt, TimeoutStmt, SpawnStmt, FnDecl,
]

# --- Expressions -------------------------------------------------------------

@dataclass
class BinaryExpr:
    left: "Expr"
    op: str
    right: "Expr"

@dataclass
class UnaryExpr:
    op: str
    operand: "Expr"

@dataclass
class NamedArg:
    name: str
    value: "Expr"

@dataclass
class StructFieldArg:
    name: str
    value: "Expr"

@dataclass
class CallExpr:
    callee: "Expr"
    args: list["Expr"] = field(default_factory=list)
    named_args: list[NamedArg] = field(default_factory=list)
    struct_field_args: list[StructFieldArg] = field(default_factory=list)
    type_args: list[TypeExpr] = field(default_factory=list)
    is_new: bool = False

@dataclass
class SelectorExpr:
    object: "Expr"
    field: str

@dataclass
class SafeNavExpr:
    object: "Expr"
    field: str
    call: Optional[CallExpr] = None

@dataclass
class PropagateExpr:
    inner: "Expr"

@dataclass
class IndexExpr:
    object: "Expr"
    index: "Expr"

@dataclass
class SliceExpr:
    object: "Expr"
    low: Optional["Expr"]
    high: Optional["Expr"]

@dataclass
class Ident:
    name: str

@dataclass
class IntLit:
    value: str

@dataclass
class FloatLit:
    value: str

@dataclass
class StringLit:
    value: str

@dataclass
class StringInterpLit:
    parts: list["Expr"]                # alternating StringLit and Expr

@dataclass
class RawStringLit:
    value: str

@dataclass
class BoolLit:
    value: bool

@dataclass
class NullLit:
    pass

@dataclass
class ThisExpr:
    pass

@dataclass
class SuperCallExpr:
    args: list["Expr"] = field(default_factory=list)

@dataclass
class ListLit:
    elements: list["Expr"] = field(default_factory=list)
    explicit_type: Optional[TypeExpr] = None

@dataclass
class MapLit:
    keys: list["Expr"] = field(default_factory=list)
    values: list["Expr"] = field(default_factory=list)
    explicit_type: Optional[TypeExpr] = None

@dataclass
class TupleLit:
    elements: list["Expr"] = field(default_factory=list)

@dataclass
class TypeAssertExpr:
    object: "Expr"
    type_name: str
    is_check: bool                     # True for `is`, False for `as`

@dataclass
class SpawnExpr:
    body: BlockStmt
    or_handler: Optional[OrHandler] = None

@dataclass
class SpreadExpr:
    expr: "Expr"

@dataclass
class LambdaExpr:
    params: list[ParamDecl]
    body: Optional[BlockStmt] = None
    expr: Optional["Expr"] = None
    return_type: Optional[TypeExpr] = None

@dataclass
class IfExpr:
    cond: "Expr"
    then: "Expr"
    else_: "Expr"

@dataclass
class MatchExprCase:
    pattern: Optional["Expr"]
    value: "Expr"

@dataclass
class MatchExpr:
    subject: "Expr"
    cases: list[MatchExprCase] = field(default_factory=list)

@dataclass
class RangeExpr:
    start: "Expr"
    end: "Expr"
    inclusive: bool = False

@dataclass
class CapacityExpr:
    collection_type: GenericType
    capacity: "Expr"

@dataclass
class SizedArrayExpr:
    element_type: str
    size: "Expr"

Expr = Union[
    BinaryExpr, UnaryExpr, CallExpr, SelectorExpr, SafeNavExpr, PropagateExpr,
    IndexExpr, SliceExpr, Ident, IntLit, FloatLit, StringLit, StringInterpLit,
    RawStringLit, BoolLit, NullLit, ThisExpr, SuperCallExpr, ListLit, MapLit,
    TupleLit, TypeAssertExpr, SpawnExpr, SpreadExpr, LambdaExpr, IfExpr,
    MatchExpr, RangeExpr, CapacityExpr, SizedArrayExpr,
]
