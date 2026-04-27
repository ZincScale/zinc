#!/usr/bin/env python3
# Copyright 2026 ZincScale
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""
zinc — braces Python CLI

Usage:
    zinc run <file.zn|dir> [args...]        transpile and run
    zinc build <file.zn|dir> [-o outdir]    transpile to .py project
    zinc build --native <file.zn|dir>       transpile + PyInstaller native binary
    zinc init <name>                        scaffold a new project
"""

import argparse
import shutil
import subprocess
import sys
from pathlib import Path

from transpiler import transpile

try:
    import tomllib
except ImportError:
    import tomli as tomllib


# --- Config -----------------------------------------------------------------

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


def _get_deps(config: dict | None) -> list[str]:
    """Extract dependencies from zinc.toml."""
    if not config:
        return []
    return config.get("python", {}).get("deps", [])


def _get_python_version(config: dict | None) -> str:
    if not config:
        return ">=3.14"
    ver = config.get("python", {}).get("version", ">=3.14")
    # Strip free-threading suffix for PEP 440 compatibility in pyproject.toml
    ver = ver.rstrip("t")
    if ver and ver[0].isdigit():
        ver = ">=" + ver
    return ver


# Default Python for uv — free-threading build
UV_PYTHON = "3.14t"


# --- Transpile --------------------------------------------------------------

def find_zn_files(path: Path) -> list[Path]:
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

    main_zn = "main.zn"
    if config and "project" in config:
        main_zn = config["project"].get("main", "main.zn")

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

    return py_files


def find_main(py_files: list[Path], config: dict | None) -> Path | None:
    if config and "project" in config:
        main = config["project"].get("main", "").replace(".zn", ".py")
        for f in py_files:
            if f.name == main:
                return f
    if len(py_files) == 1:
        return py_files[0]
    for f in py_files:
        if f.name == "main.py":
            return f
    return None


def _write_pyproject(out_dir: Path, config: dict | None):
    """Generate pyproject.toml for uv."""
    name = "zinc-app"
    version = "0.1.0"
    py_version = _get_python_version(config)
    deps = _get_deps(config)

    if config and "project" in config:
        name = config["project"].get("name", name)
        version = config["project"].get("version", version)

    lines = [
        "[project]",
        f'name = "{name}"',
        f'version = "{version}"',
        f'requires-python = "{py_version}"',
    ]
    if deps:
        lines.append("dependencies = [")
        for d in deps:
            lines.append(f'    "{d}",')
        lines.append("]")
    else:
        lines.append("dependencies = []")

    (out_dir / "pyproject.toml").write_text("\n".join(lines) + "\n")


def _find_uv() -> str | None:
    """Find uv on PATH."""
    return shutil.which("uv")


# --- Commands ---------------------------------------------------------------

def cmd_run(args):
    input_path = Path(args.input)
    config = load_config(input_path)

    # If a single file is given but config exists, transpile the whole project
    project_input = input_path
    aux_file = None  # `tests/foo.zn` — outside src/, must be transpiled separately
    if input_path.is_file() and config and "project" in config:
        config_dir = _find_config_dir(input_path)
        if config_dir:
            src_dir = config_dir / "src"
            if src_dir.is_dir():
                project_input = src_dir
                # If the user-specified file is OUTSIDE src/, remember it
                # so we can transpile it too and use it as the entry
                # point. Without this, `zinc-python run tests/foo.zn`
                # silently runs main.py (the project entry point) and
                # the user's intended file is never executed.
                try:
                    input_path.resolve().relative_to(src_dir.resolve())
                except ValueError:
                    aux_file = input_path

    tmp_dir = Path(f"/tmp/zinc-run-{project_input.stem}")
    if tmp_dir.exists():
        shutil.rmtree(tmp_dir)

    py_files = transpile_project(project_input, tmp_dir, config)
    main_py = find_main(py_files, config)

    # Transpile the aux file too (e.g., tests/foo.zn) and treat it as
    # the entry point. Output sits next to src/ inside tmp_dir, so its
    # `sys.path.insert(... "../src")` lookups resolve as expected.
    if aux_file is not None:
        config_dir = _find_config_dir(aux_file)
        rel = aux_file.relative_to(config_dir) if config_dir else Path(aux_file.name)
        aux_py = tmp_dir / rel.with_suffix(".py")
        aux_py.parent.mkdir(parents=True, exist_ok=True)
        aux_py.write_text(transpile(aux_file.read_text(), str(aux_file), entry_point=True))
        py_files.append(aux_py)
        main_py = aux_py

    # If running a specific file, use that as entry point
    if input_path.is_file() and aux_file is None:
        target_name = input_path.stem + ".py"
        for f in py_files:
            if f.name == target_name:
                main_py = f
                break

    if not main_py:
        print("error: could not determine entry point", file=sys.stderr)
        print("hint: add main = \"main.zn\" to zinc.toml [project]", file=sys.stderr)
        sys.exit(1)

    main_rel = str(main_py.relative_to(tmp_dir))
    uv = _find_uv()

    if uv:
        # Use uv — manages Python version + deps
        _write_pyproject(tmp_dir, config)
        cmd = [uv, "run", "--quiet", "--python", UV_PYTHON, "--project", str(tmp_dir),
               "python", main_rel] + args.run_args
        sys.exit(subprocess.call(cmd, cwd=tmp_dir))
    else:
        # Fallback — run directly with system python
        cmd = [sys.executable, main_rel] + args.run_args
        sys.exit(subprocess.call(cmd, cwd=tmp_dir))


def cmd_build(args):
    input_path = Path(args.input)
    config = load_config(input_path)
    out_dir = Path(args.output) if args.output else Path("build")

    py_files = transpile_project(input_path, out_dir, config)
    for f in py_files:
        print(f"compiled: {f}")

    # Always generate pyproject.toml for the build output
    _write_pyproject(out_dir, config)

    if args.native:
        main_py = find_main(py_files, config)
        if not main_py:
            print("error: could not determine entry point for native build", file=sys.stderr)
            sys.exit(1)

        uv = _find_uv()
        if not uv:
            print("error: uv is required for native builds (curl -LsSf https://astral.sh/uv/install.sh | sh)", file=sys.stderr)
            sys.exit(1)

        print("building native binary via pyinstaller...")
        binary_name = main_py.stem
        cmd = [uv, "run", "--python", UV_PYTHON, "--project", str(out_dir),
               "--with", "pyinstaller",
               "python", "-m", "PyInstaller", "--onefile",
               "--name", binary_name,
               "--distpath", str(out_dir / "dist"),
               "--workpath", str(out_dir / "build"),
               "--specpath", str(out_dir),
               str(main_py.relative_to(out_dir))]
        result = subprocess.call(cmd, cwd=out_dir)
        if result == 0:
            binary = out_dir / "dist" / binary_name
            print(f"native binary: {binary}")
        sys.exit(result)

    print(f"build complete: {out_dir}")


def cmd_init(args):
    project_dir = Path(args.name)
    project_name = project_dir.name  # just the basename, not full path
    src_dir = project_dir / "src"
    src_dir.mkdir(parents=True, exist_ok=True)

    (src_dir / "main.zn").write_text(f'''def main() {{
    print("Hello from {project_name}!")
}}
''')

    (project_dir / "zinc.toml").write_text(f'''[project]
name = "{project_name}"
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

    print(f"created project: {project_name}")
    print(f"  {src_dir}/main.zn")
    print(f"  {project_dir}/zinc.toml")
    print(f"\nrun: cd {project_dir} && zinc run src/")


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
    build_p.add_argument("--native", action="store_true", help="build native binary via PyInstaller")

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
