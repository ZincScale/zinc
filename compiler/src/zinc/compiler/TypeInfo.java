// Copyright 2026 victorybhg
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package zinc.compiler;

import java.util.List;

/**
 * Represents a resolved type in the Zinc type system.
 */
public record TypeInfo(String name, List<TypeInfo> args, boolean nullable) {

    public static final TypeInfo INT = new TypeInfo("int", List.of(), false);
    public static final TypeInfo LONG = new TypeInfo("long", List.of(), false);
    public static final TypeInfo DOUBLE = new TypeInfo("double", List.of(), false);
    public static final TypeInfo FLOAT = new TypeInfo("float", List.of(), false);
    public static final TypeInfo BOOLEAN = new TypeInfo("boolean", List.of(), false);
    public static final TypeInfo BYTE = new TypeInfo("byte", List.of(), false);
    public static final TypeInfo CHAR = new TypeInfo("char", List.of(), false);
    public static final TypeInfo SHORT = new TypeInfo("short", List.of(), false);
    public static final TypeInfo STRING = new TypeInfo("String", List.of(), false);
    public static final TypeInfo VOID = new TypeInfo("void", List.of(), false);
    public static final TypeInfo OBJECT = new TypeInfo("Object", List.of(), false);
    public static final TypeInfo NULL = new TypeInfo("null", List.of(), true);
    public static final TypeInfo ANY = new TypeInfo("any", List.of(), false);

    public TypeInfo(String name) {
        this(name, List.of(), false);
    }

    public TypeInfo(String name, List<TypeInfo> args) {
        this(name, args, false);
    }

    public boolean isPrimitive() {
        return switch (name) {
            case "int", "long", "double", "float", "boolean", "byte", "char", "short" -> true;
            default -> false;
        };
    }

    public String boxed() {
        return switch (name) {
            case "int" -> "Integer";
            case "long" -> "Long";
            case "double" -> "Double";
            case "float" -> "Float";
            case "boolean" -> "Boolean";
            case "byte" -> "Byte";
            case "char" -> "Character";
            case "short" -> "Short";
            default -> name;
        };
    }

    /** Convert Zinc type name to Java fully-qualified class. */
    public String javaClass() {
        return switch (name) {
            case "String" -> "java.lang.String";
            case "List" -> "java.util.List";
            case "ArrayList" -> "java.util.ArrayList";
            case "Map" -> "java.util.Map";
            case "HashMap" -> "java.util.HashMap";
            case "Set" -> "java.util.Set";
            case "Channel" -> "java.util.concurrent.ArrayBlockingQueue";
            default -> name;
        };
    }

    @Override
    public String toString() {
        if (args.isEmpty()) return nullable ? name + "?" : name;
        return name + "<" + String.join(", ", args.stream().map(TypeInfo::toString).toList()) + ">" + (nullable ? "?" : "");
    }
}
