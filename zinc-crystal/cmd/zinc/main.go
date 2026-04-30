// SKETCH — zinc-crystal CLI. Mirrors zinc-go's cmd/zinc/main.go,
// with subcommands `build`, `run`, `init`, etc.
//
// Status: only `build` is wired (and only does project-file emit, no
// .zn → .cr transpilation yet). Real codegen lives in
// internal/codegen_cr/ and is empty for now.

package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "build":
		opts := buildOptions{}
		dir := "."
		for _, a := range args {
			switch a {
			case "--emit-only":
				opts.EmitOnly = true
			default:
				dir = a
			}
		}
		if err := buildProject(dir, opts); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "init":
		if len(args) < 1 {
			fmt.Fprintln(os.Stderr, "usage: zinc init <name>")
			os.Exit(2)
		}
		if err := initProject(args[0]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "run":
		dir := "."
		var progArgs []string
		if len(args) > 0 {
			dir = args[0]
			if len(args) > 1 {
				progArgs = args[1:]
			}
		}
		if err := runProject(dir, progArgs); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `zinc-crystal — Zinc → Crystal transpiler (sketch)

Usage:
  zinc-crystal build [--emit-only] [dir]
                              Emit project files alongside zinc.toml
                              (shard.yml, Makefile, Dockerfile,
                              scripts/build-static.sh, .dockerignore)
                              and run "make build-static" to produce
                              bin/<name> via Docker.
                              --emit-only skips the docker build.
  zinc-crystal init <name>    Scaffold a new zinc-crystal project at
                              ./<name>/ (zinc.toml + src/<name>.zn).
  zinc-crystal run [dir] [args...]
                              Build (if needed) then exec bin/<name>.
                              Anything after [dir] is passed as argv
                              to the produced binary.`)
}
