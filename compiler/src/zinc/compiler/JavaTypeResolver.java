// Copyright 2026 victorybhg
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package zinc.compiler;

import java.lang.reflect.Method;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;

/**
 * Resolves Java method return types using reflection.
 * Replaces the Go compiler's javap-based approach.
 */
public class JavaTypeResolver {

    private static final Map<String, String> ZINC_TO_JAVA = Map.ofEntries(
        Map.entry("String", "java.lang.String"),
        Map.entry("List", "java.util.List"),
        Map.entry("ArrayList", "java.util.ArrayList"),
        Map.entry("Map", "java.util.Map"),
        Map.entry("HashMap", "java.util.HashMap"),
        Map.entry("Set", "java.util.Set"),
        Map.entry("HashSet", "java.util.HashSet"),
        Map.entry("Channel", "java.util.concurrent.ArrayBlockingQueue"),
        Map.entry("Integer", "java.lang.Integer"),
        Map.entry("Long", "java.lang.Long"),
        Map.entry("Double", "java.lang.Double"),
        Map.entry("Boolean", "java.lang.Boolean"),
        Map.entry("Math", "java.lang.Math"),
        Map.entry("System", "java.lang.System"),
        Map.entry("Thread", "java.lang.Thread"),
        Map.entry("Object", "java.lang.Object"),
        Map.entry("Files", "java.nio.file.Files"),
        Map.entry("Path", "java.nio.file.Path"),
        Map.entry("UUID", "java.util.UUID")
    );

    private final Map<String, Class<?>> classCache = new HashMap<>();

    /**
     * Resolve the return type of a method call on a Java class.
     * zincType is the Zinc type name (e.g., "Integer", "String").
     * typeArgs are the generic args (e.g., ["FlowFile"] for Channel<FlowFile>).
     * methodName is the method being called.
     */
    public Optional<TypeInfo> resolveMethodReturn(String zincType, List<TypeInfo> typeArgs, String methodName) {
        String javaClassName = ZINC_TO_JAVA.getOrDefault(zincType, zincType);
        var clazz = loadClass(javaClassName);
        if (clazz == null) return Optional.empty();

        for (Method m : clazz.getMethods()) {
            if (m.getName().equals(methodName)) {
                var returnType = m.getReturnType();
                var resolved = classToTypeInfo(returnType);

                // Resolve generic type params: if return type is a type variable (like E),
                // substitute with the concrete type arg
                var typeParams = clazz.getTypeParameters();
                var genericReturn = m.getGenericReturnType();
                if (genericReturn instanceof java.lang.reflect.TypeVariable<?> tv) {
                    for (int i = 0; i < typeParams.length; i++) {
                        if (typeParams[i].getName().equals(tv.getName()) && i < typeArgs.size()) {
                            return Optional.of(typeArgs.get(i));
                        }
                    }
                }

                return Optional.of(resolved);
            }
        }
        return Optional.empty();
    }

    /**
     * Check if a method on a Java class is declared as throwing a checked exception.
     */
    public boolean methodThrows(String zincType, String methodName) {
        String javaClassName = ZINC_TO_JAVA.getOrDefault(zincType, zincType);
        var clazz = loadClass(javaClassName);
        if (clazz == null) return false;

        for (Method m : clazz.getMethods()) {
            if (m.getName().equals(methodName)) {
                return m.getExceptionTypes().length > 0;
            }
        }
        return false;
    }

    private Class<?> loadClass(String name) {
        return classCache.computeIfAbsent(name, n -> {
            try { return Class.forName(n); }
            catch (ClassNotFoundException e) { return null; }
        });
    }

    private TypeInfo classToTypeInfo(Class<?> clazz) {
        if (clazz == int.class) return TypeInfo.INT;
        if (clazz == long.class) return TypeInfo.LONG;
        if (clazz == double.class) return TypeInfo.DOUBLE;
        if (clazz == float.class) return TypeInfo.FLOAT;
        if (clazz == boolean.class) return TypeInfo.BOOLEAN;
        if (clazz == byte.class) return TypeInfo.BYTE;
        if (clazz == char.class) return TypeInfo.CHAR;
        if (clazz == short.class) return TypeInfo.SHORT;
        if (clazz == void.class) return TypeInfo.VOID;
        if (clazz == String.class) return TypeInfo.STRING;
        return new TypeInfo(clazz.getSimpleName());
    }
}
