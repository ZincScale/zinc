// Copyright 2026 victorybhg
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package zinc.compiler;

import java.util.HashMap;
import java.util.Map;
import java.util.Optional;

/**
 * Lexical scope for type resolution.
 * Links to parent scope for nested lookups.
 */
public class Scope {
    private final Scope parent;
    private final Map<String, TypeInfo> symbols = new HashMap<>();

    public Scope() { this.parent = null; }
    public Scope(Scope parent) { this.parent = parent; }

    public void set(String name, TypeInfo type) {
        symbols.put(name, type);
    }

    public Optional<TypeInfo> lookup(String name) {
        var type = symbols.get(name);
        if (type != null) return Optional.of(type);
        if (parent != null) return parent.lookup(name);
        return Optional.empty();
    }

    public Scope child() {
        return new Scope(this);
    }
}
