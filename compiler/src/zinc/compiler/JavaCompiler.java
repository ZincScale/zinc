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
 * Java compilation pipeline: .zn → Lexer → Parser → TypeChecker → Transformer → Emitter → .java
 */
public class JavaCompiler {

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

            String className = Main.capitalize(znFile.getFileName().toString().replace(".zn", ""));

            String pkg = null;
            var sourceRoot = Files.isDirectory(input) ? input : input.getParent();
            var relative = sourceRoot.toAbsolutePath().relativize(znFile.toAbsolutePath().getParent());
            if (relative.getNameCount() > 0 && !relative.toString().isEmpty()) {
                pkg = relative.toString().replace("/", ".").replace("\\", ".");
            }

            var lexResult = new Lexer(source).tokenize();
            if (lexResult.isErr()) return Result.err(((Result.Err<?>) lexResult).errors());

            var parser = new Parser(lexResult.unwrap());
            var parseResult = parser.parseResult();
            if (parseResult.isErr()) return Result.err(((Result.Err<?>) parseResult).errors());
            var program = parseResult.unwrap();

            if (program.pkg() == null && pkg != null) {
                program = new Ast.Program(program.sourceFile(), new Ast.PackageDecl(pkg),
                    program.imports(), program.decls(), program.stmts());
            }

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
        String className = Main.capitalize(fileName.replace(".zn", ""));

        var lexResult = new Lexer(source).tokenize();
        if (lexResult.isErr()) return Result.err(((Result.Err<?>) lexResult).errors());
        var tokens = lexResult.unwrap();

        var parser = new Parser(tokens);
        var parseResult = parser.parseResult();
        if (parseResult.isErr()) return Result.err(((Result.Err<?>) parseResult).errors());
        var program = parseResult.unwrap();

        var typeChecker = new TypeChecker();
        var typeResult = typeChecker.check(program);
        var resolvedTypes = typeResult.isOk() ? typeResult.unwrap() : Map.<String, TypeInfo>of();

        var transformer = new Transformer(className, resolvedTypes);
        var transformResult = transformer.transformAll(program);
        if (transformResult.isErr()) return Result.err(((Result.Err<?>) transformResult).errors());
        var units = transformResult.unwrap();

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

    static Result<Void> runJavac(List<Path> javaFiles, Path outDir) {
        var compiler = javax.tools.ToolProvider.getSystemJavaCompiler();
        if (compiler == null) {
            return Result.err("javac not available — jdk.compiler module missing");
        }

        try {
            Files.createDirectories(outDir);
        } catch (IOException e) {
            return Result.err("cannot create output dir: " + e.getMessage());
        }

        var fileManager = compiler.getStandardFileManager(null, null, null);
        var sourceFiles = fileManager.getJavaFileObjectsFromPaths(javaFiles);
        var options = List.of(
            "--enable-preview", "--source", "25",
            "-d", outDir.toString());

        var diagnostics = new javax.tools.DiagnosticCollector<javax.tools.JavaFileObject>();
        var task = compiler.getTask(null, fileManager, diagnostics, options, null, sourceFiles);
        boolean success = task.call();

        try { fileManager.close(); } catch (IOException e) { /* ignore */ }

        if (!success) {
            var errors = new StringBuilder("javac failed:\n");
            for (var d : diagnostics.getDiagnostics()) {
                if (d.getKind() == javax.tools.Diagnostic.Kind.ERROR) {
                    errors.append(d.toString()).append("\n");
                }
            }
            return Result.err(errors.toString());
        }

        return Result.ok(null);
    }

    // --- java -----------------------------------------------------------------

    static int runJava(String mainClass, Path classDir, List<String> args) {
        try {
            var classLoader = new java.net.URLClassLoader(
                new java.net.URL[]{classDir.toUri().toURL()},
                ClassLoader.getSystemClassLoader());
            var clazz = classLoader.loadClass(mainClass);
            var main = clazz.getMethod("main", String[].class);
            main.invoke(null, (Object) args.toArray(new String[0]));
            return 0;
        } catch (java.lang.reflect.InvocationTargetException e) {
            if (e.getCause() != null) {
                e.getCause().printStackTrace();
            }
            return 1;
        } catch (Exception e) {
            System.err.println("failed to run " + mainClass + ": " + e.getMessage());
            return 1;
        }
    }

    static String findMainClass(List<Path> javaFiles, Path outDir) {
        for (var f : javaFiles) {
            try {
                var content = Files.readString(f);
                if (content.contains("public static void main(")) {
                    var name = f.getFileName().toString().replace(".java", "");
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
}
