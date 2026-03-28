// Copyright 2026 victorybhg
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package zinc.compiler;

import com.github.javaparser.ast.CompilationUnit;
import com.github.javaparser.printer.DefaultPrettyPrinter;
import com.github.javaparser.printer.configuration.DefaultPrinterConfiguration;
import com.github.javaparser.printer.configuration.DefaultConfigurationOption;
import com.github.javaparser.printer.configuration.Indentation;
import com.github.javaparser.printer.configuration.Indentation.IndentType;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;

/**
 * Writes JavaParser CompilationUnits to .java files.
 */
public class Emitter {

    /** Source maps built during emission, keyed by class name. */
    private final java.util.Map<String, SourceMap> sourceMaps = new java.util.HashMap<>();

    /** Get source maps built during emission. Call after emit(). */
    public java.util.Map<String, SourceMap> sourceMaps() { return sourceMaps; }

    /**
     * Writes a CompilationUnit to a .java file in the given output directory.
     * Creates subdirectories for packages.
     */
    public Result<Path> emit(CompilationUnit cu, Path outDir) {
        // Determine class name from the first type declaration
        var types = cu.getTypes();
        if (types.isEmpty()) return Result.err("no types in compilation unit");

        String className = types.get(0).getNameAsString();

        // Package → directory
        Path dir = outDir;
        if (cu.getPackageDeclaration().isPresent()) {
            String pkg = cu.getPackageDeclaration().get().getNameAsString();
            for (String part : pkg.split("\\.")) {
                dir = dir.resolve(part);
            }
        }

        try {
            Files.createDirectories(dir);
        } catch (IOException e) {
            return Result.err("failed to create directory " + dir + ": " + e.getMessage());
        }

        Path file = dir.resolve(className + ".java");
        String source = cu.toString();

        // Build source map from @zn markers in rendered output
        String znFile = className.toLowerCase() + ".zn";
        var sourceMap = SourceMap.fromRendered(source, znFile);
        if (!sourceMap.isEmpty()) {
            sourceMaps.put(className, sourceMap);
            // Replace placeholder with actual map data for embedded runtime trace rewriting
            source = source.replace("\"__ZN_PLACEHOLDER__\"", "\"" + sourceMap.toCompactString() + "\"");
        }

        try {
            Files.writeString(file, source);
        } catch (IOException e) {
            return Result.err("failed to write " + file + ": " + e.getMessage());
        }

        return Result.ok(file);
    }

    /**
     * Returns the Java source as a string without writing to disk.
     */
    public String render(CompilationUnit cu) {
        return cu.toString();
    }
}
