package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"growler/internal/codegen"
	"growler/internal/lexer"
	"growler/internal/parser"
	"growler/internal/typechecker"
)

func runWatch(inFile, outFile string) {
	fmt.Printf("Watching %s for changes (Ctrl+C to stop)...\n", inFile)

	// Record initial mod time so we only transpile on actual changes
	info, err := os.Stat(inFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "watch: %v\n", err)
		os.Exit(1)
	}
	lastMod := info.ModTime()

	for {
		time.Sleep(300 * time.Millisecond)

		info, err := os.Stat(inFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "watch: %v\n", err)
			continue
		}

		if info.ModTime().After(lastMod) {
			lastMod = info.ModTime()
			watchTranspile(inFile, outFile)
		}
	}
}

func watchTranspile(inFile, outFile string) {
	ts := time.Now().Format("15:04:05")

	src, err := os.ReadFile(inFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[%s] error reading %s: %v\n", ts, inFile, err)
		return
	}

	l := lexer.New(string(src))
	tokens := l.Tokenize()
	if len(l.Errors) > 0 {
		fmt.Fprintf(os.Stderr, "[%s] lex errors:\n", ts)
		for _, e := range l.Errors {
			fmt.Fprintf(os.Stderr, "  %s:%s\n", inFile, e)
		}
		return
	}

	p := parser.New(tokens)
	prog := p.Parse()
	if len(p.Errors) > 0 {
		fmt.Fprintf(os.Stderr, "[%s] parse errors:\n", ts)
		for _, e := range p.Errors {
			fmt.Fprintf(os.Stderr, "  %s:%s\n", inFile, e)
		}
		return
	}

	if errs := typechecker.Check(prog); len(errs) > 0 {
		fmt.Fprintf(os.Stderr, "[%s] type errors:\n", ts)
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "  %s\n", e)
		}
		return
	}

	gen := codegen.New()
	goSrc := gen.Generate(prog)

	dest := outFile
	if dest == "" {
		base := filepath.Base(inFile)
		base = strings.TrimSuffix(base, filepath.Ext(base))
		dest = base + ".go"
	}

	if err := os.WriteFile(dest, []byte(goSrc), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "[%s] error writing %s: %v\n", ts, dest, err)
		return
	}

	exec.Command("gofmt", "-w", dest).Run() //nolint

	fmt.Printf("[%s] transpiled %s → %s\n", ts, inFile, dest)
}
