"""Zinc parser.

Grammar lives in `grammar.lark`. Lark builds a parse tree; a Transformer
(to be added) lowers it into the typed AST nodes in `zinc.ast`.

For now the parser just exposes the raw Lark tree so we can iterate on
grammar coverage against the example suite.
"""
from __future__ import annotations

from pathlib import Path

from lark import Lark, Tree

GRAMMAR_PATH = Path(__file__).parent / "grammar.lark"

_parser: Lark | None = None


def _get_parser() -> Lark:
    global _parser
    if _parser is None:
        _parser = Lark(
            GRAMMAR_PATH.read_text(),
            parser="earley",
            ambiguity="resolve",
            propagate_positions=True,
        )
    return _parser


def parse_file(path: Path) -> Tree:
    return parse_source(path.read_text(), origin=str(path))


def parse_source(src: str, origin: str = "<string>") -> Tree:
    return _get_parser().parse(src)
