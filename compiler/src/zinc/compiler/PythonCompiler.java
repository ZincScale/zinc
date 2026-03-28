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
            if (lexResult.isErr()) return Result.err(((Result.Err<?>) lexResult).errors());

            var parser = new Parser(lexResult.unwrap());
            var parseResult = parser.parseResult();
            if (parseResult.isErr()) return Result.err(((Result.Err<?>) parseResult).errors());
            var program = parseResult.unwrap();

            String modulePkg = relPath.toString().replace("/", ".").replace("\\", ".");
            if (modulePkg.equals(".")) modulePkg = "";

            var emitter = new PythonEmitter(className);
            emitter.setProjectModules(projectModules);
            emitter.setModulePackage(modulePkg);
            var emitResult = emitter.emit(program, targetDir);
            if (emitResult.isErr()) return Result.err(((Result.Err<?>) emitResult).errors());
            pyFiles.add(emitResult.unwrap());
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

    static int runPythonProject(Path outDir, String moduleName, ZincConfig config, List<String> args) {
        boolean hasUv = hasCommand("uv");
        if (hasUv) {
            return runWithUv(outDir, moduleName, config, args);
        } else {
            return runWithPip(outDir, moduleName, config, args);
        }
    }

    private static int runWithUv(Path outDir, String moduleName, ZincConfig config, List<String> args) {
        String uv = findBundledTool("uv");
        try {
            var pyproject = new StringBuilder();
            pyproject.append("[project]\n");
            pyproject.append("name = \"zinc-app\"\n");
            pyproject.append("version = \"0.0.1\"\n");
            pyproject.append("requires-python = \"").append(config != null ? config.pythonVersion : ">=3.10").append("\"\n");
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
                .inheritIO()
                .directory(outDir.toFile())
                .start();
            return proc.waitFor();
        } catch (Exception e) {
            System.err.println("uv run failed: " + e.getMessage());
            return 1;
        }
    }

    private static int runWithPip(Path outDir, String moduleName, ZincConfig config, List<String> args) {
        String python = findPython();
        if (python == null) {
            System.err.println("error: python3 not found. Install Python 3.10+ or uv to use --python");
            return 1;
        }

        var reqs = outDir.resolve("requirements.txt");
        if (Files.exists(reqs)) {
            try {
                var pip = new ProcessBuilder(python, "-m", "pip", "install", "-q", "-r", reqs.toString())
                    .inheritIO().start();
                int exit = pip.waitFor();
                if (exit != 0) System.err.println("warning: pip install failed");
            } catch (Exception e) {
                System.err.println("warning: could not install deps: " + e.getMessage());
            }
        }

        try {
            var cmd = new ArrayList<>(List.of(python, "-m", moduleName));
            cmd.addAll(args);

            var proc = new ProcessBuilder(cmd)
                .inheritIO()
                .directory(outDir.toFile())
                .start();
            return proc.waitFor();
        } catch (Exception e) {
            System.err.println("failed to run python: " + e.getMessage());
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

    static String findPython() {
        for (var candidate : List.of("python3.14t", "python3.14", "python3.13", "python3.12", "python3.11", "python3.10", "python3", "python")) {
            if (hasCommand(candidate)) return candidate;
        }
        return null;
    }

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
