// Copyright 2026 victorybhg
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package zinc.compiler;

import com.github.javaparser.ast.NodeList;
import com.github.javaparser.ast.expr.*;

import java.util.Set;

/**
 * Declarative mappings from Zinc static class calls to Java equivalents.
 * Prevents collision between static library methods (Math.max) and
 * stream terminal operations (.max()).
 */
public final class JavaStdlibMapping {

    private JavaStdlibMapping() {}

    /** Classes whose methods should never enter the stream chain. */
    private static final Set<String> STATIC_CLASSES = Set.of(
        "Math", "Integer", "Double", "Float", "Long", "Short", "Byte",
        "String", "Character", "Boolean", "Arrays", "System", "Thread"
    );

    /** Returns true if this identifier names a known static class. */
    public static boolean isStaticClass(String name) {
        return STATIC_CLASSES.contains(name);
    }

    /**
     * Resolve a static method call like Math.max(a, b) or String.valueOf(x).
     * Returns the JavaParser Expression, or null if no special mapping exists
     * (in which case the caller should emit a normal static method call).
     */
    public static Expression resolveStaticCall(String className, String method, NodeList<Expression> args) {
        return switch (className) {
            case "Math" -> resolveMathCall(method, args);
            case "Integer" -> resolveIntegerCall(method, args);
            case "Double" -> resolveDoubleCall(method, args);
            case "String" -> resolveStringCall(method, args);
            default -> defaultStaticCall(className, method, args);
        };
    }

    private static Expression resolveMathCall(String method, NodeList<Expression> args) {
        // All Math methods are straightforward static calls
        return new MethodCallExpr(new NameExpr("Math"), method, args);
    }

    private static Expression resolveIntegerCall(String method, NodeList<Expression> args) {
        return new MethodCallExpr(new NameExpr("Integer"), method, args);
    }

    private static Expression resolveDoubleCall(String method, NodeList<Expression> args) {
        return new MethodCallExpr(new NameExpr("Double"), method, args);
    }

    private static Expression resolveStringCall(String method, NodeList<Expression> args) {
        return new MethodCallExpr(new NameExpr("String"), method, args);
    }

    private static Expression defaultStaticCall(String className, String method, NodeList<Expression> args) {
        return new MethodCallExpr(new NameExpr(className), method, args);
    }

    /**
     * Resolve a top-level function call to its Java equivalent.
     * Returns null if no mapping exists.
     */
    public static Expression resolveTopLevelCall(String name, NodeList<Expression> args) {
        return switch (name) {
            case "print" -> new MethodCallExpr(new NameExpr("System.out"), "println", args);
            case "len" -> args.isEmpty() ? null : new MethodCallExpr(args.get(0), "size");
            case "sleep" -> new MethodCallExpr(new NameExpr("Thread"), "sleep", args);
            case "parseInt" -> new MethodCallExpr(new NameExpr("Integer"), "parseInt", args);
            default -> null;
        };
    }
}
