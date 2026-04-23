"""Zinc CLI entry point.

Preserves the `zinc <subcommand>` surface from the Go reference implementation.
"""
from __future__ import annotations

import sys
from pathlib import Path

from zinc import __version__

USAGE = """\
Usage: zinc <command> [args]

Commands:
  zinc run <file.zn|dir> [-- args...]           Transpile and run
  zinc build [dir] [-o outdir] [--cross os/arch] Transpile and build
  zinc test [dir] [-- go-test-args]             Transpile *_test.zn and run go test
  zinc init <name>                              Create a new Zinc project
  zinc fmt <file.zn|dir>                        Format Zinc source code
  zinc add <module@version>                     Add a Go dependency
  zinc deps                                     List dependencies
  zinc <file.zn> [-- args...]                   Shorthand for zinc run
  zinc version                                  Show version

Project mode: when a zinc.toml is present, build/run use the project config.
"""


def cmd_version(_args: list[str]) -> int:
    print(f"zinc {__version__} (python)")
    return 0


def cmd_build(args: list[str]) -> int:
    from zinc.commands import build as build_cmd
    return build_cmd.run(args)


def cmd_run(args: list[str]) -> int:
    from zinc.commands import run as run_cmd
    return run_cmd.run(args)


def cmd_test(args: list[str]) -> int:
    from zinc.commands import test as test_cmd
    return test_cmd.run(args)


def cmd_init(args: list[str]) -> int:
    from zinc.commands import init as init_cmd
    return init_cmd.run(args)


def cmd_fmt(args: list[str]) -> int:
    from zinc.commands import fmt as fmt_cmd
    return fmt_cmd.run(args)


def cmd_add(args: list[str]) -> int:
    from zinc.commands import deps as deps_cmd
    return deps_cmd.add(args)


def cmd_deps(args: list[str]) -> int:
    from zinc.commands import deps as deps_cmd
    return deps_cmd.list_(args)


COMMANDS = {
    "version": cmd_version,
    "build": cmd_build,
    "run": cmd_run,
    "test": cmd_test,
    "init": cmd_init,
    "fmt": cmd_fmt,
    "add": cmd_add,
    "deps": cmd_deps,
}


def main(argv: list[str] | None = None) -> int:
    argv = list(sys.argv[1:] if argv is None else argv)
    if not argv or argv[0] in ("-h", "--help", "help"):
        print(USAGE)
        return 0

    cmd = argv[0]
    rest = argv[1:]

    if cmd in COMMANDS:
        return COMMANDS[cmd](rest)

    if cmd.endswith(".zn") or Path(cmd).is_file():
        return cmd_run([cmd, *rest])

    print(f"zinc: unknown command `{cmd}`\n", file=sys.stderr)
    print(USAGE, file=sys.stderr)
    return 2
