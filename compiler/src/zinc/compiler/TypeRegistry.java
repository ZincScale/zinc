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
 * Cross-file type registry — collects type signatures from all parsed .zn files.
 * Used by both Java and Python backends to resolve types defined in other files.
 */
public class TypeRegistry {

    public enum TypeKind { CLASS, INTERFACE, DATA_CLASS, ENUM, SEALED }

    public record TypeSig(
        String name,
        String module,       // e.g., "models.user" (dotted module path)
        TypeKind kind,
        List<String> parents,
        List<FieldSig> fields,
        List<MethodSig> methods
    ) {}

    public record FieldSig(String name, Ast.TypeExpr type, boolean isPub, boolean isReadonly) {}
    public record MethodSig(String name, boolean isPub, boolean isStatic, List<Ast.ParamDecl> params, Ast.TypeExpr returnType) {}

    // name → signature (last wins if multiple files define same name)
    private final Map<String, TypeSig> byName = new LinkedHashMap<>();
    // module → list of types defined in that module
    private final Map<String, List<TypeSig>> byModule = new LinkedHashMap<>();
    // all interface names (fast lookup)
    private final Set<String> interfaceNames = new HashSet<>();
    // all class names (fast lookup)
    private final Set<String> classNames = new HashSet<>();

    /**
     * Register all types declared in a parsed program.
     * @param module the dotted module path (e.g., "models.user"), or "" for root
     */
    public void register(String module, Ast.Program program) {
        for (var decl : program.decls()) {
            var sig = switch (decl) {
                case Ast.ClassDecl cls -> new TypeSig(
                    cls.name(), module, TypeKind.CLASS, cls.parents(),
                    cls.fields().stream().map(f -> new FieldSig(f.name(), f.type(), f.isPub(), f.isReadonly())).toList(),
                    cls.methods().stream().map(m -> new MethodSig(m.name(), m.isPub(), m.isStatic(), m.params(), m.returnType())).toList()
                );
                case Ast.InterfaceDecl iface -> new TypeSig(
                    iface.name(), module, TypeKind.INTERFACE, List.of(),
                    List.of(),
                    iface.methods().stream().map(m -> new MethodSig(m.name(), m.isPub(), false, m.params(), m.returnType())).toList()
                );
                case Ast.DataClassDecl data -> new TypeSig(
                    data.name(), module, TypeKind.DATA_CLASS, data.parents(),
                    data.params().stream().map(p -> new FieldSig(p.name(), p.type(), true, true)).toList(),
                    data.methods().stream().map(m -> new MethodSig(m.name(), m.isPub(), m.isStatic(), m.params(), m.returnType())).toList()
                );
                case Ast.EnumDecl en -> new TypeSig(
                    en.name(), module, TypeKind.ENUM, List.of(), List.of(), List.of()
                );
                case Ast.SealedClassDecl sealed -> {
                    // Register the sealed class itself
                    var sealedSig = new TypeSig(
                        sealed.name(), module, TypeKind.SEALED, sealed.parents(),
                        sealed.fields().stream().map(f -> new FieldSig(f.name(), f.type(), f.isPub(), f.isReadonly())).toList(),
                        sealed.methods().stream().map(m -> new MethodSig(m.name(), m.isPub(), m.isStatic(), m.params(), m.returnType())).toList()
                    );
                    // Also register each variant as a data class
                    for (var v : sealed.variants()) {
                        var variantSig = new TypeSig(
                            v.name(), module, TypeKind.DATA_CLASS, List.of(sealed.name()),
                            v.params().stream().map(p -> new FieldSig(p.name(), p.type(), true, true)).toList(),
                            v.methods().stream().map(m -> new MethodSig(m.name(), m.isPub(), m.isStatic(), m.params(), m.returnType())).toList()
                        );
                        put(variantSig);
                        classNames.add(v.name());
                    }
                    yield sealedSig;
                }
                case Ast.FnDecl fn -> null; // functions aren't types
                case Ast.ConstDecl con -> null; // constants aren't types
            };

            if (sig != null) {
                put(sig);
                switch (sig.kind()) {
                    case INTERFACE -> interfaceNames.add(sig.name());
                    case CLASS, DATA_CLASS, SEALED, ENUM -> classNames.add(sig.name());
                }
            }
        }
    }

    private void put(TypeSig sig) {
        byName.put(sig.name(), sig);
        byModule.computeIfAbsent(sig.module(), k -> new ArrayList<>()).add(sig);
    }

    // --- Lookups ---

    /** Look up a type by name. Returns null if not found. */
    public TypeSig lookup(String name) {
        return byName.get(name);
    }

    /** Get all types defined in a module. Returns empty list if module unknown. */
    public List<TypeSig> lookupModule(String module) {
        return byModule.getOrDefault(module, List.of());
    }

    /** Check if a type name is a known interface. */
    public boolean isInterface(String name) {
        return interfaceNames.contains(name);
    }

    /** Check if a type name is a known class (class, data class, sealed, enum). */
    public boolean isClass(String name) {
        return classNames.contains(name);
    }

    /** Check if a type name exists in the registry. */
    public boolean contains(String name) {
        return byName.containsKey(name);
    }

    /** Get all interface names. */
    public Set<String> allInterfaces() {
        return Collections.unmodifiableSet(interfaceNames);
    }

    /** Get all class names. */
    public Set<String> allClasses() {
        return Collections.unmodifiableSet(classNames);
    }

    /** Get all registered type names. */
    public Set<String> allTypeNames() {
        return Collections.unmodifiableSet(byName.keySet());
    }
}
