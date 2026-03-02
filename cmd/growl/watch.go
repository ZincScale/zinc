package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"growl/internal/codegen"
	"growl/internal/lexer"
	"growl/internal/parser"
)

func runWatch(inFile, outFile string) {
	fmt.Printf("Watching %s for changes (Ctrl+C to stop)...\n", inFile)

	var lastMod time.Time

	for {
		info, err := os.Stat(inFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "watch: %v\n", err)
			time.Sleep(500 * time.Millisecond)
			continue
		}

		if info.ModTime().After(lastMod) {
			lastMod = info.ModTime()
			if !lastMod.IsZero() {
				watchTranspile(inFile, outFile)
			} else {
				// First iteration — just record mod time
			}
		}
		time.Sleep(300 * time.Millisecond)
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
