package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"zinc/internal/codegen"
	"zinc/internal/errs"
	"zinc/internal/lexer"
	"zinc/internal/parser"
	"zinc/internal/typechecker"
)

func runWatch(inFile, outFile string) {
	fmt.Printf("Watching %s for changes (Ctrl+C to stop)...\n", inFile)

	// Record initial mod time so we only transpile on actual changes
	info, err := os.Stat(inFile)
	if err != nil {
		errs.Errorf("watch: %v", err)
		os.Exit(1)
	}
	lastMod := info.ModTime()

	for {
		time.Sleep(300 * time.Millisecond)

		info, err := os.Stat(inFile)
		if err != nil {
			ts := time.Now().Format("15:04:05")
			errs.WatchError(ts, fmt.Sprintf("stat: %v", err))
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
		errs.WatchError(ts, fmt.Sprintf("reading %s: %v", inFile, err))
		return
	}

	l := lexer.New(string(src))
	tokens := l.Tokenize()
	if len(l.Errors) > 0 {
		errs.WatchErrors(ts, inFile, "lex", l.Errors)
		return
	}

	p := parser.New(tokens)
	prog := p.Parse()
	if len(p.Errors) > 0 {
		errs.WatchErrors(ts, inFile, "parse", p.Errors)
		return
	}

	if tcErrs := typechecker.Check(prog); len(tcErrs) > 0 {
		strs := make([]string, len(tcErrs))
		for i, e := range tcErrs {
			strs[i] = e.String()
		}
		errs.WatchErrors(ts, inFile, "type", strs)
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
		errs.WatchError(ts, fmt.Sprintf("writing %s: %v", dest, err))
		return
	}

	exec.Command("gofmt", "-w", dest).Run() //nolint

	fmt.Printf("[%s] transpiled %s → %s\n", ts, inFile, dest)
}
