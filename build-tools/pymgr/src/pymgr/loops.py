from __future__ import annotations

import ast
from dataclasses import dataclass
from pathlib import Path
from typing import Iterable, Sequence

from pymgr.workspace import Workspace, WorkspaceError


@dataclass(frozen=True)
class LoopFinding:
    path: str
    line: int
    end_line: int
    construct: str
    intent: str
    evidence: str
    recommendation: str
    reason: str
    alternatives: tuple[str, ...] = ()
    blockers: tuple[str, ...] = ()
    performance: str = "unmeasured"
    suggested_code: str = ""

    @property
    def location(self) -> str:
        return f"{self.path}:{self.line}"


class LoopAnalyzer:
    def __init__(self, workspace: Workspace) -> None:
        self.workspace = workspace

    def scan(self, paths: Sequence[str] = ()) -> list[LoopFinding]:
        targets = (
            [self._resolve_path(path) for path in paths]
            if paths
            else self.workspace.source_roots()
        )
        findings: list[LoopFinding] = []
        for path in _python_files(targets):
            findings.extend(self._analyze_file(path))
        return sorted(findings, key=lambda item: (item.path, item.line, item.construct))

    def explain(self, location: str) -> LoopFinding:
        value, separator, raw_line = location.rpartition(":")
        if not separator or not raw_line.isdigit():
            raise WorkspaceError("loop location must use file.py:line syntax")
        path = self._resolve_path(value)
        if not path.is_file():
            raise WorkspaceError(f"loop location is not a file: {value}")
        line = int(raw_line)
        findings = self._analyze_file(path)
        exact = [item for item in findings if item.line == line]
        containing = [item for item in findings if item.line <= line <= item.end_line]
        matches = exact or containing
        if not matches:
            raise WorkspaceError(f"no loop construct found at {location}")
        return min(matches, key=lambda item: (item.end_line - item.line, item.line))

    def _resolve_path(self, value: str) -> Path:
        path = Path(value)
        if not path.is_absolute():
            path = self.workspace.root / path
        path = path.resolve()
        if path != self.workspace.root and self.workspace.root not in path.parents:
            raise WorkspaceError(f"loop analysis path escapes the workspace: {value}")
        if not path.exists():
            raise WorkspaceError(f"loop analysis path does not exist: {value}")
        return path

    def _analyze_file(self, path: Path) -> list[LoopFinding]:
        try:
            source = path.read_text(encoding="utf-8")
            tree = ast.parse(source, filename=str(path))
        except (OSError, SyntaxError) as error:
            raise WorkspaceError(f"cannot analyze loops in {path}: {error}") from error
        relative = str(path.relative_to(self.workspace.root))
        contexts = _statement_contexts(tree)
        parents = _parents(tree)
        findings: list[LoopFinding] = []
        for node in ast.walk(tree):
            if isinstance(node, ast.AsyncFor):
                findings.append(self._async_for(relative, node))
            elif isinstance(node, ast.For):
                siblings, index = contexts.get(id(node), ([], -1))
                findings.append(self._for_loop(relative, node, siblings, index))
            elif isinstance(node, ast.While):
                findings.append(self._while_loop(relative, node))
            elif isinstance(node, (ast.ListComp, ast.SetComp, ast.DictComp)):
                findings.append(self._comprehension(relative, node, parents))
            elif isinstance(node, ast.GeneratorExp):
                findings.append(self._generator(relative, node))
            elif isinstance(node, ast.Call):
                finding = self._call_construct(relative, node)
                if finding:
                    findings.append(finding)
        return findings

    def _for_loop(
        self,
        path: str,
        node: ast.For,
        siblings: list[ast.stmt],
        index: int,
    ) -> LoopFinding:
        previous = siblings[index - 1] if index > 0 else None
        following = siblings[index + 1 :] if index >= 0 else []
        leaked_names = _target_names(node.target).intersection(_loaded_names(following))
        leak_blocker = ("loop target is read after the loop",) if leaked_names else ()

        collection = _collection_candidate(previous, node)
        if collection and not leak_blocker:
            kind, filtered = collection
            construct = {
                "list": "list comprehension",
                "set": "set comprehension",
                "dict": "dict comprehension",
            }[kind]
            detail = "transform and filter" if filtered else "simple transform"
            return _finding(
                path,
                node,
                "for",
                f"eager {kind} construction",
                "semantic",
                f"Consider a {construct}",
                f"The loop is a {detail} into one pre-initialized {kind}; preserve the explicit loop if its intermediate state is intentionally observable.",
                ("generator expression if the result has a single lazy consumer",),
                suggested_code=_collection_example(previous, node, kind, filtered),
            )

        reduction = _reduction_candidate(previous, node)
        if reduction and not leak_blocker:
            return _finding(
                path,
                node,
                "for",
                "reduction",
                "heuristic",
                "Consider sum(...) with a generator expression",
                "The loop only adds one expression into an accumulator. Confirm numeric edge cases and equivalence with tests.",
                (
                    "keep the explicit accumulator for non-additive or stateful reductions",
                ),
                suggested_code=_reduction_example(previous, node),
            )

        predicate = _predicate_candidate(previous, node)
        if predicate and not leak_blocker:
            return _finding(
                path,
                node,
                "for",
                "short-circuit search",
                "semantic",
                "Consider any(...) with a generator expression",
                "The loop sets a Boolean and breaks on the first match, which is the short-circuit behavior expressed by any(...).",
                (
                    "next(...) when the matching element, rather than a Boolean, is needed",
                ),
                suggested_code=_predicate_example(previous, node),
            )

        indexed = _indexed_candidate(node)
        if indexed:
            collections, index_used_directly = indexed
            if len(collections) == 1:
                if not index_used_directly:
                    return _finding(
                        path,
                        node,
                        "for",
                        "element traversal through an index",
                        "semantic",
                        "Iterate over the collection elements directly",
                        f"The loop uses the numeric index only to read {collections[0]}[index].",
                        ("enumerate(...) when the numeric index is also needed",),
                        leak_blocker,
                        suggested_code=f"for item in {collections[0]}:\n    ...",
                    )
                return _finding(
                    path,
                    node,
                    "for",
                    "indexed traversal",
                    "semantic",
                    "Prefer enumerate(...) when both index and element are needed",
                    f"The loop ranges over len({collections[0]}) and indexes that same collection.",
                    ("direct element iteration when the index is unused",),
                    leak_blocker,
                    suggested_code=f"for {ast.unparse(node.target)}, item in enumerate({collections[0]}):\n    ...",
                )
            return _finding(
                path,
                node,
                "for",
                "parallel indexed traversal",
                "heuristic",
                "Consider zip(..., strict=True) after confirming equal-length semantics",
                "The index selects from multiple collections; strict zip states the parallel traversal directly but may change failure timing.",
                ("enumerate(...) if the numeric index is also part of the result",),
                leak_blocker,
                suggested_code=f"for values in zip({', '.join(collections)}, strict=True):\n    ...",
            )

        control = _control_features(node)
        blockers = list(leak_blocker)
        if node.orelse:
            blockers.append("loop else clause")
        blockers.extend(control)
        if blockers:
            return _finding(
                path,
                node,
                "for",
                "stateful or branching iteration",
                "semantic",
                "Keep the explicit for loop",
                "Its control flow or externally visible loop variable does not map cleanly to a declarative construct.",
                (),
                tuple(dict.fromkeys(blockers)),
            )
        return _finding(
            path,
            node,
            "for",
            "general traversal",
            "semantic",
            "Keep the explicit for loop unless it produces one simple result",
            "A normal for loop is the clearest general-purpose construct for iteration with statements or side effects.",
            (
                "comprehension for one eager collection",
                "generator expression for one lazy consumer",
                "built-in reduction for one accumulated result",
            ),
        )

    def _while_loop(self, path: str, node: ast.While) -> LoopFinding:
        blockers = tuple(_control_features(node))
        return _finding(
            path,
            node,
            "while",
            "condition-driven repetition",
            "semantic",
            "Keep the while loop for state, retry, sentinel, or unbounded iteration",
            "while expresses repetition controlled by a changing condition rather than traversal of an iterable.",
            ("iter(callable, sentinel) for a simple synchronous sentinel reader",),
            blockers,
        )

    def _async_for(self, path: str, node: ast.AsyncFor) -> LoopFinding:
        return _finding(
            path,
            node,
            "async for",
            "asynchronous iteration",
            "semantic",
            "Keep async for unless building one simple asynchronous comprehension",
            "The construct is selected for the asynchronous iterator protocol, not as a general performance optimization.",
            ("asynchronous comprehension for one eager result",),
            tuple(_control_features(node)),
        )

    def _comprehension(
        self,
        path: str,
        node: ast.ListComp | ast.SetComp | ast.DictComp,
        parents: dict[int, ast.AST],
    ) -> LoopFinding:
        kind = {
            ast.ListComp: "list comprehension",
            ast.SetComp: "set comprehension",
            ast.DictComp: "dict comprehension",
        }[type(node)]
        parent = parents.get(id(node))
        consumer = _call_name(parent) if isinstance(parent, ast.Call) else ""
        if isinstance(node, ast.ListComp) and consumer in {
            "sum",
            "min",
            "max",
            "any",
            "all",
        }:
            return _finding(
                path,
                node,
                kind,
                "eager temporary consumed by a reduction",
                "semantic",
                "Use a generator expression to avoid the temporary list",
                f"{consumer}(...) consumes the iterable once and does not require a pre-built list.",
                (),
            )
        return _finding(
            path,
            node,
            kind,
            "eager collection construction",
            "semantic",
            f"Keep the {kind} when the complete collection is required",
            "Comprehensions express one eager transformation or filter and materialize the result immediately.",
            ("generator expression for lazy, single-pass consumption",),
        )

    def _generator(self, path: str, node: ast.GeneratorExp) -> LoopFinding:
        return _finding(
            path,
            node,
            "generator expression",
            "lazy single-pass transformation",
            "semantic",
            "Keep the generator expression for a lazy single consumer",
            "It defers work and avoids materializing the complete result, but it can only be consumed once.",
            ("comprehension when repeated access or eager validation is required",),
        )

    def _call_construct(self, path: str, node: ast.Call) -> LoopFinding | None:
        name = _call_name(node)
        if name in {"sum", "min", "max", "any", "all"}:
            if name in {"min", "max", "any", "all"} and len(node.args) != 1:
                return None
            if name == "sum" and len(node.args) not in {1, 2}:
                return None
            intent = (
                "short-circuit predicate" if name in {"any", "all"} else "reduction"
            )
            return _finding(
                path,
                node,
                f"{name} built-in",
                intent,
                "semantic",
                f"Keep {name}(...) when one aggregate result is intended",
                "The built-in directly states the reduction or predicate and accepts lazy iterables.",
            )
        if name in {"enumerate", "zip", "map", "filter", "reversed"}:
            return _finding(
                path,
                node,
                f"{name} iterator",
                "iterator adaptation",
                "semantic",
                f"Use {name}(...) when its iterator semantics match the traversal",
                "This construct is lazy; choose it for its meaning rather than an assumed speed advantage.",
            )
        if name == "sorted":
            return _finding(
                path,
                node,
                "sorted built-in",
                "ordered materialization",
                "semantic",
                "Use sorted(...) when a new fully ordered list is required",
                "sorted consumes the iterable and materializes a list; use it for ordering semantics rather than as a traversal shortcut.",
            )
        if name.startswith("itertools."):
            return _finding(
                path,
                node,
                name,
                "specialized iterator pipeline",
                "semantic",
                "Keep the itertools operation when it directly expresses the iterator mechanics",
                "itertools avoids hand-maintained iterator state and normally remains lazy.",
            )
        return None


def _finding(
    path: str,
    node: ast.AST,
    construct: str,
    intent: str,
    evidence: str,
    recommendation: str,
    reason: str,
    alternatives: tuple[str, ...] = (),
    blockers: tuple[str, ...] = (),
    suggested_code: str = "",
) -> LoopFinding:
    return LoopFinding(
        path,
        node.lineno,
        getattr(node, "end_lineno", node.lineno),
        construct,
        intent,
        evidence,
        recommendation,
        reason,
        alternatives,
        blockers,
        suggested_code=suggested_code,
    )


def _statement_contexts(tree: ast.AST) -> dict[int, tuple[list[ast.stmt], int]]:
    contexts: dict[int, tuple[list[ast.stmt], int]] = {}
    for parent in ast.walk(tree):
        for _field, value in ast.iter_fields(parent):
            if isinstance(value, list) and all(
                isinstance(item, ast.stmt) for item in value
            ):
                statements = value
                for index, statement in enumerate(statements):
                    contexts[id(statement)] = (statements, index)
    return contexts


def _parents(tree: ast.AST) -> dict[int, ast.AST]:
    result: dict[int, ast.AST] = {}
    for parent in ast.walk(tree):
        for child in ast.iter_child_nodes(parent):
            result[id(child)] = parent
    return result


def _assigned_name(node: ast.stmt | None, value_type: type[ast.AST]) -> str | None:
    if not isinstance(node, ast.Assign) or len(node.targets) != 1:
        return None
    target = node.targets[0]
    if isinstance(target, ast.Name) and isinstance(node.value, value_type):
        return target.id
    return None


def _assigned_empty_set(node: ast.stmt | None) -> str | None:
    if not isinstance(node, ast.Assign) or len(node.targets) != 1:
        return None
    target = node.targets[0]
    if (
        isinstance(target, ast.Name)
        and isinstance(node.value, ast.Call)
        and _call_name(node.value) == "set"
        and not node.value.args
        and not node.value.keywords
    ):
        return target.id
    return None


def _collection_candidate(
    previous: ast.stmt | None, node: ast.For
) -> tuple[str, bool] | None:
    candidates = (
        ("list", _assigned_name(previous, ast.List), "append"),
        (
            "set",
            _assigned_name(previous, ast.Set) or _assigned_empty_set(previous),
            "add",
        ),
    )
    for kind, target, method in candidates:
        if not target:
            continue
        call = _single_collection_call(node.body, target, method)
        if call is not None:
            return kind, call
    target = _assigned_name(previous, ast.Dict)
    if target and len(node.body) == 1 and isinstance(node.body[0], ast.Assign):
        assignment = node.body[0]
        if (
            len(assignment.targets) == 1
            and isinstance(assignment.targets[0], ast.Subscript)
            and isinstance(assignment.targets[0].value, ast.Name)
            and assignment.targets[0].value.id == target
        ):
            return "dict", False
    return None


def _single_collection_call(
    body: list[ast.stmt], target: str, method: str
) -> bool | None:
    filtered = False
    statements = body
    if len(body) == 1 and isinstance(body[0], ast.If) and not body[0].orelse:
        filtered = True
        statements = body[0].body
    if len(statements) != 1 or not isinstance(statements[0], ast.Expr):
        return None
    call = statements[0].value
    if (
        isinstance(call, ast.Call)
        and isinstance(call.func, ast.Attribute)
        and isinstance(call.func.value, ast.Name)
        and call.func.value.id == target
        and call.func.attr == method
        and len(call.args) == 1
        and not call.keywords
    ):
        return filtered
    return None


def _collection_example(
    previous: ast.stmt | None, node: ast.For, kind: str, filtered: bool
) -> str:
    if not isinstance(previous, ast.Assign) or not isinstance(
        previous.targets[0], ast.Name
    ):
        return ""
    name = previous.targets[0].id
    statement: ast.stmt = node.body[0]
    condition = ""
    if filtered and isinstance(statement, ast.If):
        condition = f" if {ast.unparse(statement.test)}"
        statement = statement.body[0]
    prefix = f" for {ast.unparse(node.target)} in {ast.unparse(node.iter)}{condition}"
    if kind in {"list", "set"} and isinstance(statement, ast.Expr):
        call = statement.value
        if isinstance(call, ast.Call):
            opening, closing = ("[", "]") if kind == "list" else ("{", "}")
            return f"{name} = {opening}{ast.unparse(call.args[0])}{prefix}{closing}"
    if kind == "dict" and isinstance(statement, ast.Assign):
        target = statement.targets[0]
        if isinstance(target, ast.Subscript):
            return (
                f"{name} = {{{ast.unparse(target.slice)}: {ast.unparse(statement.value)}"
                f"{prefix}}}"
            )
    return ""


def _reduction_example(previous: ast.stmt | None, node: ast.For) -> str:
    if not (
        isinstance(previous, ast.Assign)
        and isinstance(previous.targets[0], ast.Name)
        and isinstance(node.body[0], ast.AugAssign)
    ):
        return ""
    return (
        f"{previous.targets[0].id} = sum({ast.unparse(node.body[0].value)} "
        f"for {ast.unparse(node.target)} in {ast.unparse(node.iter)})"
    )


def _predicate_example(previous: ast.stmt | None, node: ast.For) -> str:
    if not (
        isinstance(previous, ast.Assign)
        and isinstance(previous.targets[0], ast.Name)
        and isinstance(node.body[0], ast.If)
    ):
        return ""
    return (
        f"{previous.targets[0].id} = any({ast.unparse(node.body[0].test)} "
        f"for {ast.unparse(node.target)} in {ast.unparse(node.iter)})"
    )


def _reduction_candidate(previous: ast.stmt | None, node: ast.For) -> bool:
    if not isinstance(previous, ast.Assign) or len(previous.targets) != 1:
        return False
    target = previous.targets[0]
    if not (
        isinstance(target, ast.Name)
        and isinstance(previous.value, ast.Constant)
        and previous.value.value == 0
        and len(node.body) == 1
        and isinstance(node.body[0], ast.AugAssign)
    ):
        return False
    update = node.body[0]
    return (
        isinstance(update.target, ast.Name)
        and update.target.id == target.id
        and isinstance(update.op, ast.Add)
    )


def _predicate_candidate(previous: ast.stmt | None, node: ast.For) -> bool:
    if not isinstance(previous, ast.Assign) or len(previous.targets) != 1:
        return False
    target = previous.targets[0]
    if not (
        isinstance(target, ast.Name)
        and isinstance(previous.value, ast.Constant)
        and previous.value.value is False
        and len(node.body) == 1
        and isinstance(node.body[0], ast.If)
    ):
        return False
    branch = node.body[0]
    if branch.orelse or len(branch.body) != 2:
        return False
    assignment, stop = branch.body
    return (
        isinstance(assignment, ast.Assign)
        and len(assignment.targets) == 1
        and isinstance(assignment.targets[0], ast.Name)
        and assignment.targets[0].id == target.id
        and isinstance(assignment.value, ast.Constant)
        and assignment.value.value is True
        and isinstance(stop, ast.Break)
    )


def _indexed_candidate(node: ast.For) -> tuple[tuple[str, ...], bool] | None:
    if not (
        isinstance(node.target, ast.Name)
        and isinstance(node.iter, ast.Call)
        and _call_name(node.iter) == "range"
        and len(node.iter.args) == 1
        and isinstance(node.iter.args[0], ast.Call)
        and _call_name(node.iter.args[0]) == "len"
        and len(node.iter.args[0].args) == 1
        and isinstance(node.iter.args[0].args[0], ast.Name)
    ):
        return None
    index = node.target.id
    primary = node.iter.args[0].args[0].id
    collections: list[str] = []
    indexed_name_nodes: set[int] = set()
    for item in ast.walk(node):
        if (
            isinstance(item, ast.Subscript)
            and isinstance(item.value, ast.Name)
            and isinstance(item.slice, ast.Name)
            and item.slice.id == index
            and item.value.id not in collections
        ):
            collections.append(item.value.id)
            indexed_name_nodes.add(id(item.slice))
    if primary not in collections:
        return None
    index_used_directly = any(
        isinstance(item, ast.Name)
        and item.id == index
        and isinstance(item.ctx, ast.Load)
        and id(item) not in indexed_name_nodes
        for item in ast.walk(node)
    )
    return tuple(collections), index_used_directly


def _target_names(target: ast.AST) -> set[str]:
    return {node.id for node in ast.walk(target) if isinstance(node, ast.Name)}


def _loaded_names(nodes: Iterable[ast.AST]) -> set[str]:
    return {
        item.id
        for node in nodes
        for item in ast.walk(node)
        if isinstance(item, ast.Name) and isinstance(item.ctx, ast.Load)
    }


def _control_features(node: ast.For | ast.AsyncFor | ast.While) -> list[str]:
    features: list[str] = []

    def visit(item: ast.AST) -> None:
        if isinstance(
            item, (ast.FunctionDef, ast.AsyncFunctionDef, ast.ClassDef, ast.Lambda)
        ):
            return
        if isinstance(item, (ast.For, ast.AsyncFor, ast.While)):
            return
        if isinstance(item, ast.Break):
            features.append("break")
        elif isinstance(item, ast.Continue):
            features.append("continue")
        elif isinstance(item, (ast.Return, ast.Raise, ast.Yield, ast.YieldFrom)):
            features.append("non-local control flow")
        elif isinstance(item, ast.Try):
            features.append("exception handling")
        for child in ast.iter_child_nodes(item):
            visit(child)

    for statement in node.body:
        visit(statement)
    return list(dict.fromkeys(features))


def _call_name(node: ast.AST | None) -> str:
    if not isinstance(node, ast.Call):
        return ""
    function = node.func
    if isinstance(function, ast.Name):
        return function.id
    if isinstance(function, ast.Attribute):
        parts = [function.attr]
        value = function.value
        while isinstance(value, ast.Attribute):
            parts.append(value.attr)
            value = value.value
        if isinstance(value, ast.Name):
            parts.append(value.id)
            return ".".join(reversed(parts))
    return ""


def _python_files(paths: Iterable[Path]) -> Iterable[Path]:
    ignored = {".venv", ".pymgr", "__pycache__", ".git"}
    seen: set[Path] = set()
    for path in paths:
        candidates = [path] if path.is_file() else sorted(path.rglob("*.py"))
        for candidate in candidates:
            if any(part in ignored for part in candidate.parts):
                continue
            if candidate.suffix == ".py" and candidate not in seen:
                seen.add(candidate)
                yield candidate
