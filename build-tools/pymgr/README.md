# pymgr

`pymgr` is an optional development coordinator for ordinary Python projects.
It keeps uv environment mutations transactional, explains the local module and
export graph, and applies conservative syntax-aware refactors. The application
does not import or depend on `pymgr`.

This directory contains the working Phase 1-3 foundation and the first static
slice of Phase 4 from
[`docs/python-development-tool-design.md`](../../docs/python-development-tool-design.md):

1. Environment foundation.
2. Modules and exports.
3. Safe refactoring.
4. Loop construct analysis.

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
pymgr imports fix
pymgr imports fix --apply
pymgr imports organize
pymgr imports organize --apply
pymgr move acme.old acme.new
pymgr move acme.old acme.new --apply
pymgr loops src/acme
pymgr loop explain src/acme/service.py:42
```

Refactors are previews unless `--apply` is supplied. Derived state is stored in
`.pymgr/` and can be deleted without changing application behavior. Loop advice
separates semantic and heuristic evidence and never makes an unmeasured speed
claim.
