# pymgr

`pymgr` is an optional development coordinator for ordinary Python projects.
It keeps uv environment mutations transactional, explains the local module and
export graph, and applies conservative syntax-aware refactors. The application
does not import or depend on `pymgr`.

This directory contains the initial vertical slice through the first three
delivery phases from
[`docs/python-development-tool-design.md`](../../docs/python-development-tool-design.md):

1. Environment foundation.
2. Modules and exports.
3. Safe refactoring.

The implementation is an isolated Python CLI because these phases rely on
Python's own AST and LibCST for semantics-preserving source edits. It is managed
separately from every target project by uv.

## Development

```text
uv sync
uv run pytest
uv run pymgr --help
```

From a target project:

```text
pymgr init
pymgr sync
pymgr doctor
pymgr modules
pymgr cycles
pymgr exports acme
pymgr move acme.old acme.new
pymgr move acme.old acme.new --apply
```

Refactors are previews unless `--apply` is supplied. Derived state is stored in
`.pymgr/` and can be deleted without changing application behavior.
