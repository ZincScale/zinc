// Copyright 2026 victorybhg
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package zinc.compiler;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;

/**
 * Zinc compiler CLI entry point.
 * Usage: zinc-compiler <file.zn> [-o outdir]
 */
public class Main {

    public static void main(String[] args) {
        if (args.length == 0) {
            System.err.println("usage: zinc-compiler <file.zn> [-o outdir]");
            System.exit(1);
        }

        String inputFile = null;
        Path outDir = Path.of("out");

        for (int i = 0; i < args.length; i++) {
            if (args[i].equals("-o") && i + 1 < args.length) {
                outDir = Path.of(args[++i]);
            } else if (!args[i].startsWith("-")) {
                inputFile = args[i];
            }
        }

        if (inputFile == null) {
            System.err.println("error: no input file");
            System.exit(1);
        }

        var result = compile(Path.of(inputFile), outDir);
        switch (result) {
            case Result.Ok<Path> ok -> System.out.println("compiled: " + ok.value());
            case Result.Err<Path> err -> {
                for (var e : err.errors()) System.err.println("error: " + e);
                System.exit(1);
            }
        }
    }

    /**
     * Full compilation pipeline: .zn → lex → parse → transform → emit .java
     */
    public static Result<Path> compile(Path inputFile, Path outDir) {
        // Read source
        String source;
        try {
            source = Files.readString(inputFile);
        } catch (IOException e) {
            return Result.err("cannot read " + inputFile + ": " + e.getMessage());
        }

        // Derive class name from filename
        String fileName = inputFile.getFileName().toString();
        String className = capitalize(fileName.replace(".zn", ""));

        // Lex
        var lexResult = new Lexer(source).tokenize();
        if (lexResult.isErr()) return Result.err(((Result.Err<?>) lexResult).errors());
        var tokens = lexResult.unwrap();

        // Parse
        var parser = new Parser(tokens);
        var parseResult = parser.parseResult();
        if (parseResult.isErr()) return Result.err(((Result.Err<?>) parseResult).errors());
        var program = parseResult.unwrap();

        // Typecheck
        var typeChecker = new TypeChecker();
        var typeResult = typeChecker.check(program);
        // Type errors are warnings, not fatal — continue with partial info
        var resolvedTypes = typeResult.isOk() ? typeResult.unwrap() : java.util.Map.<String, TypeInfo>of();

        // Transform
        var transformer = new Transformer(className, resolvedTypes);
        var transformResult = transformer.transformAll(program);
        if (transformResult.isErr()) return Result.err(((Result.Err<?>) transformResult).errors());
        var units = transformResult.unwrap();

        // Emit all compilation units
        var emitter = new Emitter();
        Path lastFile = null;
        for (var cu : units) {
            var emitResult = emitter.emit(cu, outDir);
            if (emitResult.isErr()) return emitResult;
            lastFile = emitResult.unwrap();
        }
        return lastFile != null ? Result.ok(lastFile) : Result.err("no output generated");
    }

    private static String capitalize(String s) {
        if (s.isEmpty()) return s;
        // Convert snake_case to PascalCase: error_test → ErrorTest
        var sb = new StringBuilder();
        boolean upper = true;
        for (char c : s.toCharArray()) {
            if (c == '_' || c == '-') { upper = true; continue; }
            sb.append(upper ? Character.toUpperCase(c) : c);
            upper = false;
        }
        return sb.toString();
    }
}
