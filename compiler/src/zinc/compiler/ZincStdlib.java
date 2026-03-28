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

/**
 * Zinc standard library — bundled runtime modules for each target.
 *
 * Stdlib source files live in src/zinc/stdlib/{target}/ and are bundled
 * into the compiler jar as classpath resources. During compilation, the
 * needed files are copied to the output directory.
 *
 * To add a new stdlib module: drop a .py file in src/zinc/stdlib/python/
 * and add its name to PYTHON_MODULES. The Makefile copies the stdlib
 * directory into the jar automatically.
 */
public final class ZincStdlib {

    private ZincStdlib() {}

    /** All Python stdlib modules that get copied to the output directory. */
    private static final List<String> PYTHON_MODULES = List.of(
        "zinc_runtime.py"
    );

    private static final String PYTHON_RESOURCE_PREFIX = "/zinc/stdlib/python/";

    /**
     * Copy all Python stdlib modules to the output directory.
     * Reads from classpath resources (bundled in the jar).
     */
    public static void copyPythonStdlib(Path outDir) throws IOException {
        Files.createDirectories(outDir);
        for (var module : PYTHON_MODULES) {
            copyResource(PYTHON_RESOURCE_PREFIX + module, outDir.resolve(module));
        }
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
