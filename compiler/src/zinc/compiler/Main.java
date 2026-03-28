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
            case "init" -> cmdInit(rest);
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
        System.err.println("  build <file.zn|dir> [-o outdir]        compile to Java");
        System.err.println("  build --python <file.zn|dir> [-o dir]  transpile to Python");
        System.err.println("  run <file.zn|dir> [args...]            compile and run (Java)");
        System.err.println("  run --python <file.zn> [args...]        transpile and run (Python)");
        System.err.println("  run --python --main <file.zn>          transpile src/, run entry point");
        System.err.println("  init <name>                            create a new project");
    }

    // --- build ---------------------------------------------------------------

    private static void cmdBuild(List<String> args) {
        String input = null;
        Path outDir = null;
        boolean nativeImage = false;
        boolean fatJar = false;
        boolean packageApp = true;
        boolean docker = false;
        boolean targetPython = false;

        for (int i = 0; i < args.size(); i++) {
            switch (args.get(i)) {
                case "-o" -> { if (i + 1 < args.size()) outDir = Path.of(args.get(++i)); }
                case "--native" -> { nativeImage = true; packageApp = false; }
                case "--fat-jar" -> { fatJar = true; packageApp = false; }
                case "--package" -> { packageApp = true; nativeImage = false; }
                case "--docker" -> { docker = true; packageApp = false; }
                case "--no-package" -> packageApp = false;
                case "--python", "--target-python" -> { targetPython = true; packageApp = false; }
                default -> { if (!args.get(i).startsWith("-")) input = args.get(i); }
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

        // Check for Mill project
        Path projectDir = null;
        if (Files.isDirectory(inputPath)) {
            projectDir = BuildTools.findProjectDir(inputPath);
        }

        var result = targetPython
            ? PythonCompiler.compileToPython(inputPath, outDir)
            : JavaCompiler.compileProject(inputPath, outDir);
        switch (result) {
            case Result.Ok<List<Path>> ok -> {
                for (var f : ok.value()) System.out.println("compiled: " + f);
            }
            case Result.Err<List<Path>> err -> {
                for (var e : err.errors()) System.err.println("error: " + e);
                System.exit(1);
            }
        }

        // Python target — generate project files, done (stdlib already tree-shaken by compileToPython)
        if (targetPython) {

            ZincConfig config = null;
            var configFile = ZincConfig.findConfigFile(inputPath);
            if (configFile != null) {
                try { config = ZincConfig.parse(configFile); }
                catch (IOException e) { /* non-fatal */ }
            }
            if (config == null) {
                var cwdConfig = Path.of("zinc.toml");
                if (Files.exists(cwdConfig)) {
                    try { config = ZincConfig.parse(cwdConfig); }
                    catch (IOException e) { /* non-fatal */ }
                }
            }

            if (config != null && !config.pythonDeps.isEmpty()) {
                try {
                    Files.writeString(outDir.resolve("requirements.txt"), config.toRequirementsTxt());
                } catch (IOException e) { /* non-fatal */ }
            }

            try {
                var pyproject = PythonCompiler.buildPyProjectToml(config, outDir);
                Files.writeString(outDir.resolve("pyproject.toml"), pyproject);
            } catch (IOException e) { /* non-fatal */ }

            System.out.println("python build complete: " + outDir);
            System.out.println("  run:     cd " + outDir + " && uv run --python 3.14t -m app."
                + (config != null ? config.main.replace(".zn", "").toLowerCase() : "main") + "");
            System.out.println("  install: cd " + outDir + " && pip install .");
            return;
        }

        // If Mill project, run mill compile after transpilation
        if (projectDir != null) {
            System.out.println("mill compile");
            int exitCode = BuildTools.runMill(projectDir, "compile");
            if (exitCode != 0) System.exit(exitCode);

            if (nativeImage) {
                System.out.println("building native image...");
                exitCode = BuildTools.runNativeImage(projectDir, outDir);
                if (exitCode != 0) System.exit(exitCode);
            }

            if (fatJar) {
                System.out.println("building fat jar...");
                BuildTools.runMill(projectDir, "assembly");
            }

            if (packageApp) {
                System.out.println("building packaged app...");
                int pkgExit = BuildTools.buildPackagedApp(projectDir);
                if (pkgExit != 0) System.exit(pkgExit);
            }

            if (docker) {
                System.out.println("building Docker image...");
                int dkrExit = BuildTools.buildDocker(projectDir);
                if (dkrExit != 0) System.exit(dkrExit);
            }

            System.out.println("build complete: " + projectDir + " (Mill project)");
        } else if (nativeImage) {
            var javacResult = JavaCompiler.runJavac(result.unwrap(), outDir);
            if (javacResult.isErr()) {
                for (var e : ((Result.Err<?>) javacResult).errors()) System.err.println(e);
                System.exit(1);
            }
            String mainClass = JavaCompiler.findMainClass(result.unwrap(), outDir);
            if (mainClass != null) {
                System.out.println("building native image...");
                int exitCode = BuildTools.runNativeImage(mainClass, outDir);
                if (exitCode != 0) System.exit(exitCode);
            }
        }
    }

    // --- run -----------------------------------------------------------------

    private static void cmdRun(List<String> args) {
        if (args.isEmpty()) {
            System.err.println("error: no input file");
            System.exit(1);
        }

        boolean targetPython = false;
        String input = null;
        String mainFile = null;
        var runArgs = new ArrayList<String>();

        for (int i = 0; i < args.size(); i++) {
            var arg = args.get(i);
            if (arg.equals("--python") || arg.equals("--target-python")) {
                targetPython = true;
            } else if (arg.equals("--main") && i + 1 < args.size()) {
                mainFile = args.get(++i);
            } else if (!arg.startsWith("-") && input == null) {
                input = arg;
            } else if (input != null) {
                runArgs.add(arg);
            }
        }

        if (input == null && targetPython) {
            if (Files.isDirectory(Path.of("src"))) {
                input = "src";
            } else {
                System.err.println("error: no input file (no src/ directory found)");
                System.exit(1);
            }
        } else if (input == null) {
            System.err.println("error: no input file");
            System.exit(1);
        }

        var inputPath = Path.of(input);

        // Python target
        if (targetPython) {
            ZincConfig config = null;
            var configFile = ZincConfig.findConfigFile(inputPath);
            if (configFile != null) {
                try {
                    config = ZincConfig.parse(configFile);
                    if (input.equals("src") || Files.isDirectory(inputPath)) {
                        var projectRoot = configFile.getParent();
                        inputPath = projectRoot.resolve("src");
                    }
                    if (mainFile == null) mainFile = config.main;
                } catch (IOException e) {
                    System.err.println("warning: could not read zinc.toml: " + e.getMessage());
                }
            }

            var outDir = Path.of("/tmp/zinc-run-py-" + inputPath.getFileName().toString().replace(".zn", ""));
            var compileResult = PythonCompiler.compileToPython(inputPath, outDir);
            if (compileResult.isErr()) {
                for (var e : ((Result.Err<?>) compileResult).errors()) System.err.println("error: " + e);
                System.exit(1);
            }
            if (config != null && !config.pythonDeps.isEmpty()) {
                try {
                    Files.writeString(outDir.resolve("requirements.txt"), config.toRequirementsTxt());
                } catch (IOException e) { /* non-fatal */ }
            }

            var pyFiles = compileResult.unwrap();
            Path mainPy = PythonCompiler.findMainPy(pyFiles, inputPath, mainFile);
            if (mainPy == null) {
                System.err.println("error: could not determine entry point");
                if (Files.isDirectory(inputPath)) {
                    System.err.println("hint: use --main <file.zn> to specify the entry point, or add main to zinc.toml");
                }
                System.exit(1);
            }

            String moduleName = "app." + mainPy.getFileName().toString().replace(".py", "");
            System.exit(PythonCompiler.runPythonProject(outDir, moduleName, config, runArgs));
            return;
        }

        // Java target — check for Mill project
        if (Files.isDirectory(inputPath)) {
            var projectDir = BuildTools.findProjectDir(inputPath);
            if (projectDir != null) {
                var outDir = inputPath;
                var compileResult = JavaCompiler.compileProject(inputPath, outDir);
                if (compileResult.isErr()) {
                    for (var e : ((Result.Err<?>) compileResult).errors()) System.err.println("error: " + e);
                    System.exit(1);
                }
                System.out.println("mill run");
                System.exit(BuildTools.runMill(projectDir, "run"));
                return;
            }
        }

        // Script/single-file mode
        var outDir = Path.of("/tmp/zinc-run-" + inputPath.getFileName().toString().replace(".zn", ""));

        var compileResult = JavaCompiler.compileProject(inputPath, outDir);
        if (compileResult.isErr()) {
            for (var e : ((Result.Err<?>) compileResult).errors()) System.err.println("error: " + e);
            System.exit(1);
        }
        var javaFiles = compileResult.unwrap();

        var javacResult = JavaCompiler.runJavac(javaFiles, outDir);
        if (javacResult.isErr()) {
            for (var e : ((Result.Err<?>) javacResult).errors()) System.err.println(e);
            System.exit(1);
        }

        String mainClass = JavaCompiler.findMainClass(javaFiles, outDir);
        if (mainClass == null) {
            System.err.println("error: no main class found");
            System.exit(1);
        }

        System.exit(JavaCompiler.runJava(mainClass, outDir, runArgs));
    }

    // --- init ----------------------------------------------------------------

    private static void cmdInit(List<String> args) {
        if (args.isEmpty()) {
            System.err.println("usage: zinc init <project-name> [--python]");
            System.exit(1);
        }

        boolean python = args.contains("--python");
        String name = args.stream().filter(a -> !a.startsWith("-")).findFirst().orElse("app");
        var dir = Path.of(name);

        try {
            Files.createDirectories(dir.resolve("src"));

            Files.writeString(dir.resolve("src/main.zn"), """
                fn main() {
                    print("Hello from %s!")
                }
                """.formatted(name).stripIndent());

            if (python) {
                Files.writeString(dir.resolve("zinc.toml"), """
                    [project]
                    name = "%s"
                    version = "0.1.0"
                    main = "main.zn"

                    [python]
                    version = ">=3.14"
                    deps = []
                    """.formatted(name).stripIndent());

                Files.writeString(dir.resolve(".gitignore"), """
                    out/
                    __pycache__/
                    *.pyc
                    .venv/
                    """.stripIndent());

                System.out.println("created project: " + name);
                System.out.println("  " + dir + "/src/main.zn");
                System.out.println("  " + dir + "/zinc.toml");
                System.out.println("\nrun: cd " + name + " && zinc run --python");
            } else {
                Files.createDirectories(dir.resolve("test"));

                Files.writeString(dir.resolve("build.mill.yaml"), """
                    # %s — Zinc project
                    extends: JavaModule
                    jvmVersion: 25

                    javacOptions:
                      - --enable-preview
                      - --release
                      - "25"

                    forkArgs:
                      - --enable-preview

                    mainClass: Main

                    mvnDeps: []
                    """.formatted(name).stripIndent());

                Files.writeString(dir.resolve(".gitignore"), """
                    out/
                    *.class
                    .mill-*
                    """.stripIndent());

                System.out.println("created project: " + name);
                System.out.println("  " + dir + "/src/main.zn");
                System.out.println("  " + dir + "/build.mill.yaml");
                System.out.println("\nrun: zinc run " + name + "/src");
            }

        } catch (IOException e) {
            System.err.println("error: " + e.getMessage());
            System.exit(1);
        }
    }

    // --- Helpers --------------------------------------------------------------

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
