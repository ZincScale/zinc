# zinc-go-py

Python implementation of the Zinc → Go transpiler. Parallel rewrite of `zinc-go/`.

The user-facing interface is identical — `zinc build`, `zinc run`, `zinc test`, `zinc.toml` projects — because the transpiler is an implementation detail. You run `zinc` the same way you ran it before; what changed is the language the transpiler itself is written in.

## Why Python

The Go transpiler hit a wall on Zinc's OO-over-structural-Go impedance: generated Go that a Go developer wouldn't want to maintain, and a long tail of case-by-case codegen bugs that were hard to fix at the design level without rewriting visitor-shaped code across many files.

Python trades the single-binary install for:

- A grammar file (Lark) instead of 3,900 lines of hand-rolled recursive descent.
- Dataclass ASTs and `match` statements for dispatch, which makes "fix the class of bug, not the one arm" easier to actually do.
- REPL iteration on codegen strategies.

The target is still Go. Generated Go must be small, fast, and idiomatic. The transpiler's own runtime characteristics don't matter — it's a dev tool, like `tsc` or `javac`.

## Layout

```
zinc/              Python package — CLI, parser, AST, codegen
  grammar.lark     Zinc grammar (canonical spec)
  parser.py        Lark wrapper + transformer → AST
  ast.py           AST node dataclasses
  codegen/go.py    Go backend (the only backend)
  commands/        zinc build / run / test / init / fmt / add / deps
  cli.py           Entry point
  project.py       zinc.toml loader
bin/zinc           Dev-mode launcher (no install needed)
examples/          Copied from zinc-go/ — the end-to-end test suite
examples-fail/     Inputs that must fail to compile
examples-test/     Project-mode `zinc test` fixtures
expected/          Expected stdout for each example
run_e2e.sh         Runs the full suite
```

## Dev setup

Managed with [uv](https://docs.astral.sh/uv/).

```bash
cd zinc-go-py
uv sync               # creates .venv, installs deps + zinc in editable mode
./bin/zinc version    # launcher auto-detects .venv/
# equivalent:
uv run zinc version
```

## Status

Scaffold only. Grammar extraction and hello-world end-to-end are next.

The sibling `zinc-go/` is the reference implementation and stays in tree until this one reaches parity on the full e2e suite + zinc-flow-go builds and tests cleanly through it.
