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

/**
 * Python compilation pipeline: .zn → Lexer → Parser → PythonEmitter → .py
 * Bypasses TypeChecker and Transformer entirely.
 */
public class PythonCompiler {

    /** Maps generated .py filename → source .zn filename, from the most recent compilation. */
    static java.util.Map<String, String> lastPyToZnMap = java.util.Map.of();
    /** Output directory from most recent compilation (for reading generated files). */
    static Path lastOutDir = null;

    /**
     * Compile .zn files to Python (.py) instead of Java.
     */
    public static Result<List<Path>> compileToPython(Path input, Path outDir) {
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

        var appDir = outDir.resolve("app");
        var pyFiles = new ArrayList<Path>();
        var sourceRoot = Files.isDirectory(input) ? input.toAbsolutePath() : input.toAbsolutePath().getParent();

        // Collect project module paths for relative imports
        var projectModules = new java.util.HashSet<String>();
        for (var znFile : znFiles) {
            var rel = sourceRoot.relativize(znFile.toAbsolutePath());
            String modulePath = rel.toString()
                .replace(".zn", "").replace("/", ".").replace("\\", ".").toLowerCase();
            projectModules.add(modulePath);
            if (rel.getNameCount() > 1) {
                projectModules.add(rel.getParent().toString()
                    .replace("/", ".").replace("\\", ".").toLowerCase());
            }
        }

        var packageDirs = new java.util.HashSet<Path>();
        packageDirs.add(appDir);
        var usedStdlib = new java.util.HashSet<String>();

        // Pass 1: parse all files and build cross-file type registry
        record ParsedFile(Path path, String fileName, String className, String modulePkg, Path targetDir, Ast.Program program) {}
        var parsed = new ArrayList<ParsedFile>();
        var typeRegistry = new TypeRegistry();

        for (var znFile : znFiles) {
            String source;
            try { source = Files.readString(znFile); }
            catch (IOException e) { return Result.err("cannot read " + znFile + ": " + e.getMessage()); }

            String fileName = znFile.getFileName().toString();
            String className = Main.capitalize(fileName.replace(".zn", ""));

            var relPath = sourceRoot.relativize(znFile.toAbsolutePath().getParent());
            var targetDir = appDir.resolve(relPath);
            packageDirs.add(targetDir);
            var parent = targetDir;
            while (!parent.equals(appDir) && parent.startsWith(appDir)) {
                packageDirs.add(parent);
                parent = parent.getParent();
            }

            var lexResult = new Lexer(source).tokenize();
            if (lexResult.isErr()) return Result.err(JavaCompiler.prefixErrors(fileName, ((Result.Err<?>) lexResult).errors()));

            var parser = new Parser(lexResult.unwrap());
            var parseResult = parser.parseResult();
            if (parseResult.isErr()) return Result.err(JavaCompiler.prefixErrors(fileName, ((Result.Err<?>) parseResult).errors()));
            var program = parseResult.unwrap();

            String modulePkg = relPath.toString().replace("/", ".").replace("\\", ".");
            if (modulePkg.equals(".")) modulePkg = "";

            // Register types from this file
            String modulePath = modulePkg.isEmpty() ? className.toLowerCase() : modulePkg + "." + className.toLowerCase();
            typeRegistry.register(modulePath, program);

            parsed.add(new ParsedFile(znFile, fileName, className, modulePkg, targetDir, program));
        }

        // Pass 2: emit each file with cross-file type knowledge
        var pyToZn = new java.util.HashMap<String, String>();
        for (var pf : parsed) {
            var emitter = new PythonEmitter(pf.className());
            emitter.setProjectModules(projectModules);
            emitter.setModulePackage(pf.modulePkg());
            emitter.setTypeRegistry(typeRegistry);
            var emitResult = emitter.emit(pf.program(), pf.targetDir());
            if (emitResult.isErr()) return Result.err(((Result.Err<?>) emitResult).errors());
            pyFiles.add(emitResult.unwrap());
            usedStdlib.addAll(emitter.usedStdlibModules());
            // Track .py → .zn mapping for source map trace rewriting
            pyToZn.put(pf.className().toLowerCase() + ".py", pf.fileName());
        }
        lastPyToZnMap = pyToZn;
        lastOutDir = outDir;

        // Tree-shake stdlib: only copy modules actually referenced
        try {
            ZincStdlib.copyPythonStdlib(appDir, usedStdlib);
        } catch (IOException e) {
            return Result.err("failed to copy stdlib: " + e.getMessage());
        }

        // Create __init__.py in every package directory
        for (var dir : packageDirs) {
            try {
                Files.createDirectories(dir);
                Files.writeString(dir.resolve("__init__.py"), "");
            } catch (IOException e) {
                return Result.err("failed to write __init__.py in " + dir + ": " + e.getMessage());
            }
        }

        return Result.ok(pyFiles);
    }

    // --- Python execution -----------------------------------------------------

    /**
     * Run a Python project via uv. uv handles venv, deps, and Python version.
     */
    static int runPythonProject(Path outDir, String moduleName, ZincConfig config, List<String> args) {
        String uv = findBundledTool("uv");
        try {
            var pyproject = new StringBuilder();
            pyproject.append("[project]\n");
            pyproject.append("name = \"zinc-app\"\n");
            pyproject.append("version = \"0.0.1\"\n");
            pyproject.append("requires-python = \"").append(config != null ? config.pythonVersion : ">=3.14").append("\"\n");
            if (config != null && !config.pythonDeps.isEmpty()) {
                pyproject.append("dependencies = [\n");
                for (var dep : config.pythonDeps) {
                    pyproject.append("    \"").append(dep).append("\",\n");
                }
                pyproject.append("]\n");
            } else {
                pyproject.append("dependencies = []\n");
            }
            Files.writeString(outDir.resolve("pyproject.toml"), pyproject.toString());

            var cmd = new ArrayList<>(List.of(uv, "run", "--python", "3.14t", "--project", outDir.toString(), "-m", moduleName));
            cmd.addAll(args);

            var proc = new ProcessBuilder(cmd)
                .redirectOutput(ProcessBuilder.Redirect.INHERIT)
                .directory(outDir.toFile())
                .start();

            // Read stderr in a separate thread to avoid deadlock
            var stderrReader = new Thread(() -> {
                try {
                    var stderr = new String(proc.getErrorStream().readAllBytes());
                    if (!stderr.isEmpty()) {
                        if (!lastPyToZnMap.isEmpty() && lastOutDir != null) {
                            var maps = new java.util.HashMap<String, SourceMap>();
                            var appDir = lastOutDir.resolve("app");
                            for (var entry : lastPyToZnMap.entrySet()) {
                                var pyFile = appDir.resolve(entry.getKey());
                                if (Files.exists(pyFile)) {
                                    maps.put(entry.getKey(), SourceMap.fromGeneratedFile(pyFile, entry.getValue()));
                                }
                            }
                            System.err.print(SourceMap.rewritePythonTrace(stderr, maps));
                        } else {
                            System.err.print(stderr);
                        }
                    }
                } catch (IOException e) { /* ignore */ }
            });
            stderrReader.setDaemon(true);
            stderrReader.start();

            int exitCode = proc.waitFor();
            stderrReader.join(5000);
            return exitCode;
        } catch (Exception e) {
            System.err.println("uv run failed: " + e.getMessage());
            System.err.println("uv is bundled with zinc — check lib/uv exists");
            return 1;
        }
    }

    static String buildPyProjectToml(ZincConfig config, Path outDir) {
        String name = config != null ? config.name : outDir.getFileName().toString();
        String version = config != null ? config.version : "0.1.0";
        String pyVersion = config != null ? config.pythonVersion : ">=3.14";
        String mainModule = config != null ? config.main.replace(".zn", "").toLowerCase() : "main";
        var deps = config != null ? config.pythonDeps : List.<String>of();

        var sb = new StringBuilder();
        sb.append("[project]\n");
        sb.append("name = \"").append(name).append("\"\n");
        sb.append("version = \"").append(version).append("\"\n");
        sb.append("requires-python = \"").append(pyVersion).append("\"\n");
        if (!deps.isEmpty()) {
            sb.append("dependencies = [\n");
            for (var dep : deps) sb.append("    \"").append(dep).append("\",\n");
            sb.append("]\n");
        } else {
            sb.append("dependencies = []\n");
        }
        sb.append("\n[project.scripts]\n");
        sb.append(name).append(" = \"app.").append(mainModule).append(":main\"\n");
        return sb.toString();
    }

    static Path findMainPy(List<Path> pyFiles, Path inputPath, String mainFile) {
        if (pyFiles.isEmpty()) return null;

        if (mainFile != null) {
            String target = mainFile.replace(".zn", ".py").toLowerCase();
            for (var f : pyFiles) {
                if (f.getFileName().toString().equals(target)) return f;
            }
            System.err.println("error: --main " + mainFile + " not found in transpiled output");
            return null;
        }

        if (pyFiles.size() == 1) return pyFiles.getFirst();

        if (!Files.isDirectory(inputPath)) {
            String target = inputPath.getFileName().toString().replace(".zn", ".py").toLowerCase();
            for (var f : pyFiles) {
                if (f.getFileName().toString().equals(target)) return f;
            }
        }

        for (var f : pyFiles) {
            if (f.getFileName().toString().equals("main.py")) return f;
        }

        return null;
    }

    // --- Tool discovery -------------------------------------------------------

    /**
     * Find a tool bundled alongside the zinc binary (e.g. uv, mill).
     */
    static String findBundledTool(String name) {
        try {
            var jarPath = Main.class.getProtectionDomain().getCodeSource().getLocation().toURI();
            var jarDir = Path.of(jarPath).getParent();
            if (jarDir != null) {
                var bundled = jarDir.resolve(name);
                if (Files.isExecutable(bundled)) return bundled.toString();
                var libBundled = jarDir.resolve("../lib/app/" + name);
                if (Files.isExecutable(libBundled)) return libBundled.toRealPath().toString();
            }
        } catch (Exception e) { /* fall through */ }

        var local = Path.of("lib", name);
        if (Files.isExecutable(local)) return local.toAbsolutePath().toString();

        return name;
    }

    static boolean hasCommand(String cmd) {
        String resolved = cmd.equals("uv") ? findBundledTool("uv") : cmd;
        try {
            var proc = new ProcessBuilder(resolved, "--version")
                .redirectErrorStream(true)
                .start();
            proc.getInputStream().readAllBytes();
            return proc.waitFor() == 0;
        } catch (Exception e) {
            return false;
        }
    }
}
