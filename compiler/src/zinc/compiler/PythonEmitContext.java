// Copyright 2026 victorybhg
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package zinc.compiler;

import java.util.*;

/**
 * Shared state for all Python emitter components.
 * Holds output buffer, indentation, imports, and class tracking.
 */
public class PythonEmitContext {

    StringBuilder sb = new StringBuilder();
    int indent = 0;
    final Set<String> imports = new LinkedHashSet<>();
    final Set<String> fromImports = new LinkedHashSet<>();
    final String className;
    final TargetRuntime target = new TargetRuntime.Python();
    String currentClass = null;
    Set<String> currentClassFields = new HashSet<>();
    Set<String> currentClassMethods = new HashSet<>();
    boolean insideMethod = false;
    final Map<String, String> renamedVars = new HashMap<>();
    Set<String> projectModules = Set.of();
    String modulePackage = "";
    int lambdaCounter = 0;
    int spawnCounter = 0;

    public PythonEmitContext(String className) {
        this.className = className;
    }

    // --- Output helpers -------------------------------------------------------

    void line(String s) {
        sb.append("    ".repeat(indent)).append(s).append("\n");
    }

    /** Like line() but doesn't add a newline prefix — used for elif/else continuation */
    void raw(String s) {
        sb.append("    ".repeat(indent)).append(s).append("\n");
    }

    void blank() {
        sb.append("\n");
    }

    void addImport(String module) {
        imports.add(module);
    }

    void addFromImport(String stmt) {
        fromImports.add(stmt);
    }

    // --- Naming helpers -------------------------------------------------------

    // Python builtins that Zinc variable names might shadow
    static final Set<String> PYTHON_BUILTINS = Set.of(
        "sum", "sorted", "min", "max", "len", "list", "dict", "set", "map",
        "filter", "range", "type", "id", "input", "open", "print", "hash",
        "abs", "all", "any", "int", "float", "str", "bool", "bytes", "next",
        "iter", "zip", "enumerate", "reversed", "object", "super", "vars",
        "format", "round", "pow", "dir", "help", "hex", "oct", "bin"
    );

    /** Map a Zinc variable name, avoiding Python builtin shadowing. */
    String safeVarName(String name) {
        if (PYTHON_BUILTINS.contains(name)) return name + "_";
        return name;
    }

    /** Relative import prefix to reach the app root from this module's package. */
    String runtimeImportPrefix() {
        if (modulePackage.isEmpty()) return ".";
        return ".".repeat(modulePackage.split("\\.").length + 1);
    }

    static String escapeString(String s) {
        return s.replace("\\", "\\\\")
            .replace("\"", "\\\"")
            .replace("\n", "\\n")
            .replace("\t", "\\t");
    }

    /** Map Zinc operator to Python operator. */
    static String mapOp(String op) {
        return switch (op) {
            case "&&" -> "and";
            case "||" -> "or";
            case "===" -> "is";
            case "!==" -> "is not";
            case "/" -> "//";
            default -> op;
        };
    }
}
