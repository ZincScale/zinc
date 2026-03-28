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
 * Declarative mappings from Zinc/Java static calls and field accesses to Python equivalents.
 *
 * All Python-side stdlib mappings live here. The emitter looks up calls/fields
 * in these tables rather than hardcoding case-by-case logic.
 *
 * Each mapping specifies:
 *   - The Python expression template ({0}, {1}, ... are arg placeholders)
 *   - An optional import that gets auto-added when the mapping is used
 */
public final class PythonStdlibMapping {

    private PythonStdlibMapping() {}

    /**
     * A resolved Python call: the expression to emit and any import it requires.
     */
    public record Resolved(String expr, String importStmt) {
        /** No import needed (builtins). */
        static Resolved of(String expr) { return new Resolved(expr, null); }
        /** With an `import X` statement. */
        static Resolved withImport(String expr, String imp) { return new Resolved(expr, imp); }
    }

    // --- Static call mappings: "Class.method" → resolver ---

    /**
     * Resolve a static call like Math.sqrt(args) or Set.of(args).
     * Returns null if no mapping exists.
     */
    public static Resolved resolveStaticCall(String className, String method, List<String> args) {
        String key = className + "." + method;

        // Math module — methods that delegate to Python's math module
        if (className.equals("Math")) {
            return resolveMathCall(method, args);
        }

        // Collection factories
        return switch (key) {
            case "Set.of" -> Resolved.of("{" + String.join(", ", args) + "}");
            case "List.of" -> Resolved.of("[" + String.join(", ", args) + "]");
            case "Map.of" -> {
                var entries = new ArrayList<String>();
                for (int i = 0; i + 1 < args.size(); i += 2) {
                    entries.add(args.get(i) + ": " + args.get(i + 1));
                }
                yield Resolved.of("{" + String.join(", ", entries) + "}");
            }
            // Type conversions
            case "String.valueOf" -> Resolved.of("str(" + args.getFirst() + ")");
            case "Integer.parseInt" -> Resolved.of("int(" + args.getFirst() + ")");
            case "Double.parseDouble" -> Resolved.of("float(" + args.getFirst() + ")");
            case "java.util.Arrays.toString" -> Resolved.of("str(" + args.getFirst() + ")");
            default -> null;
        };
    }

    private static Resolved resolveMathCall(String method, List<String> args) {
        // Methods that map to Python builtins (no import needed)
        return switch (method) {
            case "abs" -> Resolved.of("abs(" + args.getFirst() + ")");
            case "max" -> Resolved.of("max(" + args.get(0) + ", " + args.get(1) + ")");
            case "min" -> Resolved.of("min(" + args.get(0) + ", " + args.get(1) + ")");
            // Methods that need the math module
            default -> Resolved.withImport("math." + method + "(" + String.join(", ", args) + ")", "math");
        };
    }

    // --- Static field mappings: "Class.field" → resolved ---

    /**
     * Resolve a static field access like Math.PI or Math.E.
     * Returns null if no mapping exists.
     */
    public static Resolved resolveStaticField(String className, String field) {
        if (className.equals("Math")) {
            return switch (field) {
                case "PI" -> Resolved.withImport("math.pi", "math");
                case "E" -> Resolved.withImport("math.e", "math");
                // Any other Math constant — delegate to math module
                default -> Resolved.withImport("math." + field.toLowerCase(), "math");
            };
        }
        return null;
    }

    // --- Top-level function mappings: "funcName" → resolved ---

    /**
     * Resolve a top-level function call like sleep(ms).
     * Returns null if no mapping exists. runtimePrefix is for zinc_runtime imports.
     */
    public static Resolved resolveTopLevelCall(String name, List<String> args, String runtimePrefix) {
        return switch (name) {
            case "sleep" -> Resolved.withImport(
                "zinc_sleep(" + args.getFirst() + ")",
                "from " + runtimePrefix + "zinc_runtime import zinc_sleep");
            default -> null;
        };
    }
}
