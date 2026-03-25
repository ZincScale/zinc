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
import java.util.ArrayList;
import java.util.List;
import java.util.Map;

/**
 * Zinc compiler CLI.
 * Usage:
 *   zinc build <file.zn|dir> [-o outdir]
 *   zinc run <file.zn|dir> [args...]
 */
public class Main {

    public static void main(String[] args) {
        if (args.length == 0) {
            printUsage();
            System.exit(1);
        }

        String command = args[0];
        var rest = List.of(args).subList(1, args.length);

        switch (command) {
            case "build" -> cmdBuild(rest);
            case "run" -> cmdRun(rest);
            default -> {
                // No subcommand — treat as file to compile (backwards compat)
                if (command.endsWith(".zn")) {
                    cmdBuild(List.of(args));
                } else {
                    System.err.println("unknown command: " + command);
                    printUsage();
                    System.exit(1);
                }
            }
        }
    }

    private static void printUsage() {
        System.err.println("usage: zinc <command> [args]");
        System.err.println("commands:");
        System.err.println("  build <file.zn|dir> [-o outdir]   compile to Java");
        System.err.println("  run <file.zn|dir> [args...]       compile and run");
    }

    // --- build ---------------------------------------------------------------

    private static void cmdBuild(List<String> args) {
        String input = null;
        Path outDir = null;

        for (int i = 0; i < args.size(); i++) {
            if (args.get(i).equals("-o") && i + 1 < args.size()) {
                outDir = Path.of(args.get(++i));
            } else if (!args.get(i).startsWith("-")) {
                input = args.get(i);
            }
        }

        if (input == null) {
            System.err.println("error: no input file or directory");
            System.exit(1);
        }

        var inputPath = Path.of(input);
        if (outDir == null) {
            outDir = Path.of("/tmp/zinc-build-" + inputPath.getFileName().toString().replace(".zn", ""));
        }

        var result = compileProject(inputPath, outDir);
        switch (result) {
            case Result.Ok<List<Path>> ok -> {
                for (var f : ok.value()) System.out.println("compiled: " + f);
            }
            case Result.Err<List<Path>> err -> {
                for (var e : err.errors()) System.err.println("error: " + e);
                System.exit(1);
            }
        }
    }

    // --- run -----------------------------------------------------------------

    private static void cmdRun(List<String> args) {
        if (args.isEmpty()) {
            System.err.println("error: no input file");
            System.exit(1);
        }

        String input = args.getFirst();
        var runArgs = args.size() > 1 ? args.subList(1, args.size()) : List.<String>of();

        var inputPath = Path.of(input);
        var outDir = Path.of("/tmp/zinc-run-" + inputPath.getFileName().toString().replace(".zn", ""));

        // Step 1: Compile .zn → .java
        var compileResult = compileProject(inputPath, outDir);
        if (compileResult.isErr()) {
            for (var e : ((Result.Err<?>) compileResult).errors()) System.err.println("error: " + e);
            System.exit(1);
        }
        var javaFiles = compileResult.unwrap();

        // Step 2: javac .java → .class
        var javacResult = runJavac(javaFiles, outDir);
        if (javacResult.isErr()) {
            for (var e : ((Result.Err<?>) javacResult).errors()) System.err.println(e);
            System.exit(1);
        }

        // Step 3: Determine main class
        String mainClass = findMainClass(javaFiles, outDir);
        if (mainClass == null) {
            System.err.println("error: no main class found");
            System.exit(1);
        }

        // Step 4: java -cp outDir MainClass [args...]
        var exitCode = runJava(mainClass, outDir, runArgs);
        System.exit(exitCode);
    }

    // --- Compilation pipeline ------------------------------------------------

    /**
     * Compile a .zn file or directory of .zn files to .java files.
     * Multi-file: two-pass (collect signatures, then transform with cross-file knowledge).
     */
    public static Result<List<Path>> compileProject(Path input, Path outDir) {
        List<Path> znFiles = new ArrayList<>();

        if (Files.isDirectory(input)) {
            try (var stream = Files.walk(input)) {
                stream.filter(p -> p.toString().endsWith(".zn"))
                    .forEach(znFiles::add);
            } catch (IOException e) {
                return Result.err("cannot scan directory " + input + ": " + e.getMessage());
            }
        } else {
            znFiles.add(input);
        }

        if (znFiles.isEmpty()) {
            return Result.err("no .zn files found in " + input);
        }

        // Single file — simple path
        if (znFiles.size() == 1) {
            return compileSingleFile(znFiles.getFirst(), outDir);
        }

        // Multi-file: parse all, collect cross-file info, then transform
        record ParsedFile(Path path, String className, Ast.Program program) {}
        var parsed = new ArrayList<ParsedFile>();
        var allInterfaces = new java.util.HashSet<String>();
        var allClasses = new java.util.HashSet<String>();

        // Pass 1: parse all files, collect interface/class names
        for (var znFile : znFiles) {
            String source;
            try { source = Files.readString(znFile); }
            catch (IOException e) { return Result.err("cannot read " + znFile + ": " + e.getMessage()); }

            String className = capitalize(znFile.getFileName().toString().replace(".zn", ""));
            var lexResult = new Lexer(source).tokenize();
            if (lexResult.isErr()) return Result.err(((Result.Err<?>) lexResult).errors());

            var parser = new Parser(lexResult.unwrap());
            var parseResult = parser.parseResult();
            if (parseResult.isErr()) return Result.err(((Result.Err<?>) parseResult).errors());
            var program = parseResult.unwrap();

            parsed.add(new ParsedFile(znFile, className, program));

            for (var decl : program.decls()) {
                switch (decl) {
                    case Ast.InterfaceDecl iface -> allInterfaces.add(iface.name());
                    case Ast.ClassDecl cls -> allClasses.add(cls.name());
                    case Ast.DataClassDecl data -> allClasses.add(data.name());
                    case Ast.SealedClassDecl sealed -> {
                        allClasses.add(sealed.name());
                        for (var v : sealed.variants()) allClasses.add(v.name());
                    }
                    default -> {}
                }
            }
        }

        // Pass 2: transform each file with cross-file interface knowledge
        var allJavaFiles = new ArrayList<Path>();
        var emitter = new Emitter();

        for (var pf : parsed) {
            var typeChecker = new TypeChecker();
            var typeResult = typeChecker.check(pf.program());
            var resolvedTypes = typeResult.isOk() ? typeResult.unwrap() : Map.<String, TypeInfo>of();

            var transformer = new Transformer(pf.className(), resolvedTypes);
            // Register cross-file interfaces
            for (var ifaceName : allInterfaces) transformer.registerInterface(ifaceName);

            var transformResult = transformer.transformAll(pf.program());
            if (transformResult.isErr()) return Result.err(((Result.Err<?>) transformResult).errors());

            for (var cu : transformResult.unwrap()) {
                var emitResult = emitter.emit(cu, outDir);
                if (emitResult.isErr()) return Result.err(((Result.Err<?>) emitResult).errors());
                allJavaFiles.add(emitResult.unwrap());
            }
        }

        return Result.ok(allJavaFiles);
    }

    /**
     * Compile a single .zn file to one or more .java files.
     */
    public static Result<List<Path>> compileSingleFile(Path inputFile, Path outDir) {
        String source;
        try {
            source = Files.readString(inputFile);
        } catch (IOException e) {
            return Result.err("cannot read " + inputFile + ": " + e.getMessage());
        }

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
        var resolvedTypes = typeResult.isOk() ? typeResult.unwrap() : Map.<String, TypeInfo>of();

        // Transform
        var transformer = new Transformer(className, resolvedTypes);
        var transformResult = transformer.transformAll(program);
        if (transformResult.isErr()) return Result.err(((Result.Err<?>) transformResult).errors());
        var units = transformResult.unwrap();

        // Emit
        var emitter = new Emitter();
        var javaFiles = new ArrayList<Path>();
        for (var cu : units) {
            var emitResult = emitter.emit(cu, outDir);
            if (emitResult.isErr()) return Result.err(((Result.Err<?>) emitResult).errors());
            javaFiles.add(emitResult.unwrap());
        }

        return Result.ok(javaFiles);
    }

    // --- javac ----------------------------------------------------------------

    private static Result<Void> runJavac(List<Path> javaFiles, Path outDir) {
        var cmd = new ArrayList<String>();
        cmd.add("javac");
        cmd.add("--enable-preview");
        cmd.add("--source");
        cmd.add("25");
        cmd.add("-d");
        cmd.add(outDir.toString());
        for (var f : javaFiles) cmd.add(f.toString());

        try {
            var process = new ProcessBuilder(cmd)
                .redirectErrorStream(true)
                .start();
            var output = new String(process.getInputStream().readAllBytes());
            int exitCode = process.waitFor();
            if (exitCode != 0) {
                return Result.err("javac failed:\n" + output);
            }
            return Result.ok(null);
        } catch (Exception e) {
            return Result.err("failed to run javac: " + e.getMessage());
        }
    }

    // --- java ----------------------------------------------------------------

    private static int runJava(String mainClass, Path classDir, List<String> args) {
        var cmd = new ArrayList<String>();
        cmd.add("java");
        cmd.add("--enable-preview");
        cmd.add("-cp");
        cmd.add(classDir.toString());
        cmd.add(mainClass);
        cmd.addAll(args);

        try {
            var process = new ProcessBuilder(cmd)
                .inheritIO()
                .start();
            return process.waitFor();
        } catch (Exception e) {
            System.err.println("failed to run java: " + e.getMessage());
            return 1;
        }
    }

    // --- Helpers --------------------------------------------------------------

    private static String findMainClass(List<Path> javaFiles, Path outDir) {
        for (var f : javaFiles) {
            try {
                var content = Files.readString(f);
                if (content.contains("public static void main(")) {
                    // Extract class name from file
                    var name = f.getFileName().toString().replace(".java", "");
                    // Include package if present
                    var relative = outDir.relativize(f);
                    if (relative.getNameCount() > 1) {
                        var pkg = relative.getParent().toString().replace("/", ".").replace("\\", ".");
                        return pkg + "." + name;
                    }
                    return name;
                }
            } catch (IOException e) {
                // skip
            }
        }
        return null;
    }

    static String capitalize(String s) {
        if (s.isEmpty()) return s;
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
