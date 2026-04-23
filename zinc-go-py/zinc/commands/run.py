"""`zinc run` — build then exec the resulting binary."""
from __future__ import annotations

import os
import subprocess
import sys
from pathlib import Path

from zinc.commands.build import run as build


def run(args: list[str]) -> int:
    # Split args at `--` (everything after is passed to the built program).
    build_args: list[str] = []
    prog_args: list[str] = []
    if "--" in args:
        sep = args.index("--")
        build_args = args[:sep]; prog_args = args[sep + 1:]
    else:
        build_args = args

    if not build_args:
        print("zinc run: missing input", file=sys.stderr)
        return 2

    src = Path(build_args[0]).resolve()
    out_dir = src.parent / "zinc-out"
    if "-o" in build_args:
        idx = build_args.index("-o")
        out_dir = Path(build_args[idx + 1])

    rc = build(build_args)
    if rc != 0:
        return rc

    binary = out_dir / "zinc-app"
    if not binary.exists():
        print(f"zinc run: binary not found at {binary}", file=sys.stderr)
        return 1
    return subprocess.call([str(binary), *prog_args])
