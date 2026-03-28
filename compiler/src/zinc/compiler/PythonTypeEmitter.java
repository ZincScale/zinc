// Copyright 2026 victorybhg
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package zinc.compiler;

import java.util.stream.Collectors;

/**
 * Emits Python type annotations from Zinc AST type expressions.
 */
public class PythonTypeEmitter {

    private final PythonEmitContext ctx;

    public PythonTypeEmitter(PythonEmitContext ctx) {
        this.ctx = ctx;
    }

    String emitType(Ast.TypeExpr type) {
        return switch (type) {
            case Ast.SimpleType s -> mapTypeName(s.name());
            case Ast.GenericType g -> {
                String name = mapTypeName(g.name());
                var args = g.typeArgs().stream()
                    .map(this::emitType)
                    .collect(Collectors.joining(", "));
                yield name + "[" + args + "]";
            }
            case Ast.OptionalType o -> emitType(o.inner()) + " | None";
            case Ast.ArrayType a -> "list[" + emitType(a.elementType()) + "]";
            case Ast.FuncType f -> {
                ctx.addFromImport("from collections.abc import Callable");
                var params = f.params().stream()
                    .map(this::emitType)
                    .collect(Collectors.joining(", "));
                String ret = f.returnType() != null ? emitType(f.returnType()) : "None";
                yield "Callable[[" + params + "], " + ret + "]";
            }
        };
    }

    String mapTypeName(String name) {
        return switch (name) {
            case "int", "Integer" -> "int";
            case "double", "Double", "float", "Float" -> "float";
            case "boolean", "Boolean" -> "bool";
            case "String" -> "str";
            case "byte[]" -> "bytes";
            case "void" -> "None";
            case "any", "Object" -> "object";
            case "List" -> "list";
            case "Map" -> "dict";
            case "Set" -> "set";
            case "Lock" -> {
                ctx.addImport("threading");
                yield "threading.Lock";
            }
            case "Channel" -> {
                ctx.addFromImport("from " + ctx.runtimeImportPrefix() + "zinc_runtime import ZincChannel");
                yield "ZincChannel";
            }
            default -> name;
        };
    }
}
