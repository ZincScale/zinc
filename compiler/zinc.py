#!/usr/bin/env python3
"""
zinc — braces Python CLI

Usage:
    zinc run <file.zn|dir> [args...]        transpile and run
    zinc build <file.zn|dir> [-o outdir]    transpile to .py
    zinc build --native <file.zn|dir>       transpile + Nuitka native binary
    zinc init <name>                        scaffold a new project
"""

import argparse
import os
import shutil
import subprocess
import sys
from pathlib import Path

from transpiler import transpile

try:
    import tomllib
except ImportError:
    import tomli as tomllib


def load_config(start: Path) -> dict | None:
    """Find and load zinc.toml walking up from start."""
    p = start if start.is_dir() else start.parent
    for _ in range(10):
        cfg = p / "zinc.toml"
        if cfg.exists():
            with open(cfg, "rb") as f:
                return tomllib.load(f)
        if p.parent == p:
            break
        p = p.parent
    return None


def _find_config_dir(start: Path) -> Path | None:
    """Find the directory containing zinc.toml."""
    p = start if start.is_dir() else start.parent
    for _ in range(10):
        if (p / "zinc.toml").exists():
            return p
        if p.parent == p:
            break
        p = p.parent
    return None


def find_zn_files(path: Path) -> list[Path]:
    """Collect all .zn files from a file or directory."""
    if path.is_file():
        return [path]
    return sorted(path.rglob("*.zn"))


def transpile_project(input_path: Path, out_dir: Path, config: dict | None = None) -> list[Path]:
    """Transpile all .zn files, preserving directory structure."""
    zn_files = find_zn_files(input_path)
    if not zn_files:
        print(f"error: no .zn files found in {input_path}", file=sys.stderr)
        sys.exit(1)

    source_root = input_path if input_path.is_dir() else input_path.parent

    # Determine which file is the entry point
    main_zn = None
    if config and "project" in config:
        main_zn = config["project"].get("main", "main.zn")
    if main_zn is None:
        main_zn = "main.zn"

    py_files = []

    for zn in zn_files:
        source = zn.read_text()
        is_entry = (zn.name == main_zn) or (len(zn_files) == 1)
        py_source = transpile(source, str(zn), entry_point=is_entry)

        rel = zn.relative_to(source_root)
        py_path = out_dir / rel.with_suffix(".py")
        py_path.parent.mkdir(parents=True, exist_ok=True)
        py_path.write_text(py_source)
        py_files.append(py_path)

        # Create __init__.py in each package directory
        pkg_dir = py_path.parent
        while pkg_dir != out_dir:
            init = pkg_dir / "__init__.py"
            if not init.exists():
                init.write_text("")
            pkg_dir = pkg_dir.parent

    return py_files


def find_main(py_files: list[Path], config: dict | None) -> Path | None:
    """Determine the entry point .py file."""
    if config and "project" in config:
        main = config["project"].get("main", "").replace(".zn", ".py")
        for f in py_files:
            if f.name == main:
                return f

    # Convention: main.py or single file
    if len(py_files) == 1:
        return py_files[0]
    for f in py_files:
        if f.name == "main.py":
            return f
    return None


# --- Commands ---------------------------------------------------------------

def cmd_run(args):
    input_path = Path(args.input)
    config = load_config(input_path)

    # If a single file is given but config exists, transpile the whole project
    project_input = input_path
    if input_path.is_file() and config and "project" in config:
        config_dir = _find_config_dir(input_path)
        if config_dir:
            src_dir = config_dir / "src"
            if src_dir.is_dir():
                project_input = src_dir

    tmp_dir = Path(f"/tmp/zinc-run-{project_input.stem}")
    if tmp_dir.exists():
        shutil.rmtree(tmp_dir)

    py_files = transpile_project(project_input, tmp_dir, config)
    main_py = find_main(py_files, config)
    # If running a specific file, use that as entry point
    if input_path.is_file():
        target_name = input_path.stem + ".py"
        for f in py_files:
            if f.name == target_name:
                main_py = f
                break
    if not main_py:
        print("error: could not determine entry point", file=sys.stderr)
        print("hint: add main = \"main.zn\" to zinc.toml [project]", file=sys.stderr)
        sys.exit(1)

    # Run with python directly, cwd=tmp_dir so imports resolve
    cmd = [sys.executable, str(main_py.relative_to(tmp_dir))] + args.run_args
    sys.exit(subprocess.call(cmd, cwd=tmp_dir))


def cmd_build(args):
    input_path = Path(args.input)
    config = load_config(input_path)
    out_dir = Path(args.output) if args.output else Path("build")

    py_files = transpile_project(input_path, out_dir, config)
    for f in py_files:
        print(f"compiled: {f}")

    if args.native:
        main_py = find_main(py_files, config)
        if not main_py:
            print("error: could not determine entry point for native build", file=sys.stderr)
            sys.exit(1)
        print(f"building native binary via nuitka...")
        cmd = ["python3", "-m", "nuitka", "--standalone", "--onefile", str(main_py)]
        sys.exit(subprocess.call(cmd))

    print(f"build complete: {out_dir}")


def cmd_init(args):
    name = args.name
    project_dir = Path(name)
    src_dir = project_dir / "src"
    src_dir.mkdir(parents=True, exist_ok=True)

    (src_dir / "main.zn").write_text(f'''def main() {{
    print("Hello from {name}!")
}}

if __name__ == "__main__" {{
    main()
}}
''')

    (project_dir / "zinc.toml").write_text(f'''[project]
name = "{name}"
version = "0.1.0"
main = "main.zn"

[python]
version = ">=3.12"
deps = []
''')

    (project_dir / ".gitignore").write_text("""build/
__pycache__/
*.pyc
.venv/
""")

    print(f"created project: {name}")
    print(f"  {src_dir}/main.zn")
    print(f"  {project_dir}/zinc.toml")
    print(f"\nrun: cd {name} && zinc run src/main.zn")


# --- Main -------------------------------------------------------------------

def main():
    parser = argparse.ArgumentParser(prog="zinc", description="braces Python")
    sub = parser.add_subparsers(dest="command")

    run_p = sub.add_parser("run", help="transpile and run")
    run_p.add_argument("input", help=".zn file or directory")
    run_p.add_argument("run_args", nargs="*", help="args passed to the program")

    build_p = sub.add_parser("build", help="transpile to .py")
    build_p.add_argument("input", help=".zn file or directory")
    build_p.add_argument("-o", "--output", help="output directory")
    build_p.add_argument("--native", action="store_true", help="build native binary via Nuitka")

    init_p = sub.add_parser("init", help="scaffold a new project")
    init_p.add_argument("name", help="project name")

    args = parser.parse_args()

    if args.command == "run":
        cmd_run(args)
    elif args.command == "build":
        cmd_build(args)
    elif args.command == "init":
        cmd_init(args)
    else:
        parser.print_help()
        sys.exit(1)


if __name__ == "__main__":
    main()
