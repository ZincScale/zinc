// Copyright 2026 victorybhg
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package zinc.compiler;

import java.io.IOException;
import java.io.InputStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.StandardCopyOption;
import java.util.List;
import java.util.Set;

/**
 * Zinc standard library — bundled runtime modules for each target.
 *
 * Stdlib source files live in src/zinc/stdlib/{target}/ and are bundled
 * into the compiler jar as classpath resources. During compilation, only
 * the modules actually used by the application are copied to the output.
 *
 * To add a new stdlib module: drop a .py file in src/zinc/stdlib/python/
 * and add its name to PYTHON_MODULES.
 */
public final class ZincStdlib {

    private ZincStdlib() {}

    /** All Python stdlib modules available. */
    private static final List<String> PYTHON_MODULES = List.of(
        "zinc_runtime.py"
    );

    private static final String PYTHON_RESOURCE_PREFIX = "/zinc/stdlib/python/";

    /** Check if a module name (without .py) is a known Zinc stdlib module. */
    public static boolean isPythonStdlibModule(String name) {
        return PYTHON_MODULES.contains(name + ".py");
    }

    /**
     * Copy only the used Python stdlib modules to the output directory.
     * Tree-shakes: modules not referenced by any emitted file are skipped.
     */
    public static void copyPythonStdlib(Path outDir, Set<String> usedModules) throws IOException {
        if (usedModules.isEmpty()) return;
        Files.createDirectories(outDir);
        for (var module : usedModules) {
            if (PYTHON_MODULES.contains(module)) {
                copyResource(PYTHON_RESOURCE_PREFIX + module, outDir.resolve(module));
            }
        }
    }

    /**
     * Copy all Python stdlib modules to the output directory (no tree shaking).
     */
    public static void copyPythonStdlib(Path outDir) throws IOException {
        copyPythonStdlib(outDir, Set.copyOf(PYTHON_MODULES));
    }

    private static void copyResource(String resourcePath, Path dest) throws IOException {
        try (InputStream in = ZincStdlib.class.getResourceAsStream(resourcePath)) {
            if (in == null) {
                throw new IOException("stdlib resource not found: " + resourcePath);
            }
            Files.copy(in, dest, StandardCopyOption.REPLACE_EXISTING);
        }
    }
}
