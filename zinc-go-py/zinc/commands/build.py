"""`zinc build` — parse → transform → codegen → `go build`.

Single-file path is working. Project-mode (zinc.toml with multi-file/subdirs)
is next.
"""
from __future__ import annotations

import shutil
import subprocess
import sys
from pathlib import Path

from zinc.parser import parse_source
from zinc.transformer import ZincTransformer
from zinc.codegen.go import emit


def run(args: list[str]) -> int:
    # Usage: zinc build [file.zn | dir] [-o outdir]
    source_arg: str | None = None
    out_arg: str | None = None
    i = 0
    while i < len(args):
        a = args[i]
        if a == "-o":
            out_arg = args[i + 1]; i += 2
        else:
            source_arg = a; i += 1

    if source_arg is None:
        # Look for a zinc.toml project in cwd
        if Path("zinc.toml").exists():
            print("zinc build: project mode not wired yet; pass a .zn file", file=sys.stderr)
            return 2
        print("zinc build: missing input (file or directory)", file=sys.stderr)
        return 2

    src_path = Path(source_arg).resolve()
    if not src_path.exists():
        print(f"zinc build: {src_path}: no such file or directory", file=sys.stderr)
        return 1

    if src_path.is_file():
        return _build_file(src_path, out_arg)
    print("zinc build: directory input not wired yet; pass a .zn file", file=sys.stderr)
    return 2


def _build_file(src: Path, out_arg: str | None) -> int:
    out_dir = Path(out_arg) if out_arg else (src.parent / "zinc-out")
    out_dir.mkdir(parents=True, exist_ok=True)

    source_text = src.read_text()
    try:
        tree = parse_source(source_text, origin=str(src))
    except Exception as e:
        print(f"parse error in {src}: {e}", file=sys.stderr)
        return 1

    try:
        program = ZincTransformer().transform(tree)
    except Exception as e:
        print(f"AST build error in {src}: {e}", file=sys.stderr)
        return 1

    program.source_file = str(src)
    try:
        go_source, imports = emit(program)
    except Exception as e:
        print(f"codegen error in {src}: {e}", file=sys.stderr)
        return 1

    out_go = out_dir / (src.stem + ".go")
    out_go.write_text(go_source)
    print(f"  {src} → {out_go}")

    # Write go.mod if missing
    mod_path = out_dir / "go.mod"
    if not mod_path.exists():
        mod_path.write_text("module zinc-app\n\ngo 1.26\n")

    # Shell out to `go build` — produce a single binary in out_dir.
    if shutil.which("go") is None:
        print("zinc build: 'go' not found on PATH", file=sys.stderr)
        return 1
    result = subprocess.run(
        ["go", "build", "-o", str(out_dir / "zinc-app"), "."],
        cwd=out_dir, capture_output=True, text=True,
    )
    if result.returncode != 0:
        print(result.stderr, file=sys.stderr, end="")
        return result.returncode
    print(f"  Built: {out_dir}/zinc-app")
    return 0
