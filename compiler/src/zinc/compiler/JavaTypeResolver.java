// Copyright 2026 victorybhg
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package zinc.compiler;

import java.util.List;
import java.util.Map;
import java.util.Optional;

/**
 * Resolves Java method return types from a bundled type signature database.
 * No reflection — fully static, works in native-image.
 */
public class JavaTypeResolver {

    // Class → method → return type
    // Type parameters: E, K, V, T are resolved by substituting typeArgs
    private static final Map<String, Map<String, String>> TYPE_DB = Map.ofEntries(
        // java.lang.String
        Map.entry("java.lang.String", Map.ofEntries(
            Map.entry("length", "int"), Map.entry("charAt", "char"),
            Map.entry("substring", "String"), Map.entry("indexOf", "int"),
            Map.entry("contains", "boolean"), Map.entry("startsWith", "boolean"),
            Map.entry("endsWith", "boolean"), Map.entry("isEmpty", "boolean"),
            Map.entry("toUpperCase", "String"), Map.entry("toLowerCase", "String"),
            Map.entry("trim", "String"), Map.entry("strip", "String"),
            Map.entry("replace", "String"), Map.entry("split", "String[]"),
            Map.entry("toCharArray", "char[]"), Map.entry("repeat", "String"),
            Map.entry("getBytes", "byte[]"), Map.entry("toString", "String")
        )),
        // java.lang.Integer
        Map.entry("java.lang.Integer", Map.of(
            "parseInt", "int", "valueOf", "Integer", "toString", "String",
            "intValue", "int", "compareTo", "int"
        )),
        // java.lang.Long
        Map.entry("java.lang.Long", Map.of(
            "parseLong", "long", "valueOf", "Long", "longValue", "long"
        )),
        // java.lang.Double
        Map.entry("java.lang.Double", Map.of(
            "parseDouble", "double", "valueOf", "Double", "doubleValue", "double"
        )),
        // java.lang.Math
        Map.entry("java.lang.Math", Map.of(
            "pow", "double", "sqrt", "double", "abs", "double",
            "min", "int", "max", "int", "round", "long",
            "random", "double", "floor", "double", "ceil", "double"
        )),
        // java.lang.System
        Map.entry("java.lang.System", Map.of(
            "currentTimeMillis", "long", "nanoTime", "long", "exit", "void",
            "getenv", "String"
        )),
        // java.lang.Thread
        Map.entry("java.lang.Thread", Map.of(
            "sleep", "void", "currentThread", "Thread", "getName", "String",
            "isAlive", "boolean", "join", "void", "interrupt", "void",
            "startVirtualThread", "Thread"
        )),
        // java.util.List (E = type arg 0)
        Map.entry("java.util.List", Map.ofEntries(
            Map.entry("get", "E"), Map.entry("size", "int"), Map.entry("isEmpty", "boolean"),
            Map.entry("add", "boolean"), Map.entry("remove", "E"), Map.entry("contains", "boolean"),
            Map.entry("indexOf", "int"), Map.entry("toArray", "Object[]"), Map.entry("stream", "Stream"),
            Map.entry("of", "List"), Map.entry("subList", "List"), Map.entry("iterator", "Iterator")
        )),
        // java.util.ArrayList
        Map.entry("java.util.ArrayList", Map.of(
            "get", "E", "size", "int", "isEmpty", "boolean",
            "add", "boolean", "remove", "E", "contains", "boolean",
            "stream", "Stream", "iterator", "Iterator"
        )),
        // java.util.Map (K = type arg 0, V = type arg 1)
        Map.entry("java.util.Map", Map.ofEntries(
            Map.entry("get", "V"), Map.entry("put", "V"), Map.entry("size", "int"),
            Map.entry("isEmpty", "boolean"), Map.entry("containsKey", "boolean"),
            Map.entry("containsValue", "boolean"), Map.entry("keySet", "Set"),
            Map.entry("values", "Collection"), Map.entry("entrySet", "Set"),
            Map.entry("of", "Map"), Map.entry("remove", "V")
        )),
        // java.util.Map.Entry
        Map.entry("java.util.Map.Entry", Map.of(
            "getKey", "K", "getValue", "V"
        )),
        // java.util.Set
        Map.entry("java.util.Set", Map.of(
            "size", "int", "contains", "boolean", "add", "boolean",
            "of", "Set", "isEmpty", "boolean", "stream", "Stream"
        )),
        // java.util.UUID
        Map.entry("java.util.UUID", Map.of(
            "randomUUID", "UUID", "toString", "String"
        )),
        // java.nio.file.Files
        Map.entry("java.nio.file.Files", Map.of(
            "readString", "String", "writeString", "Path",
            "write", "Path", "createDirectories", "Path",
            "exists", "boolean", "isDirectory", "boolean",
            "readAllBytes", "byte[]", "readAllLines", "List"
        )),
        // java.nio.file.Path
        Map.entry("java.nio.file.Path", Map.of(
            "of", "Path", "resolve", "Path", "getFileName", "Path",
            "toString", "String", "toAbsolutePath", "Path"
        )),
        // java.util.concurrent.ArrayBlockingQueue (E = type arg 0)
        Map.entry("java.util.concurrent.ArrayBlockingQueue", Map.of(
            "take", "E", "put", "void", "poll", "E", "offer", "boolean",
            "size", "int", "isEmpty", "boolean", "peek", "E"
        )),
        // java.util.concurrent.CompletableFuture
        Map.entry("java.util.concurrent.CompletableFuture", Map.of(
            "join", "void", "get", "T", "isDone", "boolean",
            "isCompletedExceptionally", "boolean",
            "complete", "boolean", "completeExceptionally", "boolean"
        )),
        // java.util.Optional
        Map.entry("java.util.Optional", Map.of(
            "get", "T", "orElse", "T", "isPresent", "boolean",
            "isEmpty", "boolean", "map", "Optional"
        )),
        // AtomicInteger
        Map.entry("java.util.concurrent.atomic.AtomicInteger", Map.of(
            "get", "int", "set", "void", "addAndGet", "int",
            "incrementAndGet", "int", "decrementAndGet", "int",
            "compareAndSet", "boolean"
        ))
    );

    // Zinc type → Java class name
    private static final Map<String, String> ZINC_TO_JAVA = Map.ofEntries(
        Map.entry("String", "java.lang.String"),
        Map.entry("Integer", "java.lang.Integer"),
        Map.entry("Long", "java.lang.Long"),
        Map.entry("Double", "java.lang.Double"),
        Map.entry("Math", "java.lang.Math"),
        Map.entry("System", "java.lang.System"),
        Map.entry("Thread", "java.lang.Thread"),
        Map.entry("List", "java.util.List"),
        Map.entry("ArrayList", "java.util.ArrayList"),
        Map.entry("Map", "java.util.Map"),
        Map.entry("HashMap", "java.util.HashMap"),
        Map.entry("Set", "java.util.Set"),
        Map.entry("HashSet", "java.util.HashSet"),
        Map.entry("Channel", "java.util.concurrent.ArrayBlockingQueue"),
        Map.entry("UUID", "java.util.UUID"),
        Map.entry("Files", "java.nio.file.Files"),
        Map.entry("Path", "java.nio.file.Path"),
        Map.entry("AtomicInteger", "java.util.concurrent.atomic.AtomicInteger"),
        Map.entry("CompletableFuture", "java.util.concurrent.CompletableFuture"),
        Map.entry("Optional", "java.util.Optional"),
        Map.entry("Object", "java.lang.Object")
    );

    // Type parameter mapping: class → [param names]
    private static final Map<String, List<String>> TYPE_PARAMS = Map.of(
        "java.util.List", List.of("E"),
        "java.util.ArrayList", List.of("E"),
        "java.util.Map", List.of("K", "V"),
        "java.util.Set", List.of("E"),
        "java.util.concurrent.ArrayBlockingQueue", List.of("E"),
        "java.util.concurrent.CompletableFuture", List.of("T"),
        "java.util.Optional", List.of("T")
    );

    /**
     * Resolve the return type of a method call on a Java class.
     * No reflection — uses static type database.
     */
    public Optional<TypeInfo> resolveMethodReturn(String zincType, List<TypeInfo> typeArgs, String methodName) {
        String javaClass = ZINC_TO_JAVA.getOrDefault(zincType, zincType);
        var methods = TYPE_DB.get(javaClass);
        if (methods == null) return Optional.empty();

        String returnType = methods.get(methodName);
        if (returnType == null) return Optional.empty();

        // Resolve generic type parameters
        var params = TYPE_PARAMS.get(javaClass);
        if (params != null) {
            for (int i = 0; i < params.size() && i < typeArgs.size(); i++) {
                if (returnType.equals(params.get(i))) {
                    return Optional.of(typeArgs.get(i));
                }
            }
        }

        return Optional.of(toTypeInfo(returnType));
    }

    /**
     * Check if a method returns Optional.
     */
    public boolean returnsOptional(String zincType, String methodName) {
        String javaClass = ZINC_TO_JAVA.getOrDefault(zincType, zincType);
        var methods = TYPE_DB.get(javaClass);
        if (methods == null) return false;
        String ret = methods.get(methodName);
        return ret != null && ret.equals("Optional");
    }

    private TypeInfo toTypeInfo(String name) {
        return switch (name) {
            case "int" -> TypeInfo.INT;
            case "long" -> TypeInfo.LONG;
            case "double" -> TypeInfo.DOUBLE;
            case "float" -> TypeInfo.FLOAT;
            case "boolean" -> TypeInfo.BOOLEAN;
            case "byte" -> TypeInfo.BYTE;
            case "char" -> TypeInfo.CHAR;
            case "short" -> TypeInfo.SHORT;
            case "void" -> TypeInfo.VOID;
            case "String" -> TypeInfo.STRING;
            default -> new TypeInfo(name);
        };
    }
}
