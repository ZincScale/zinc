// Copyright 2026 victorybhg
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package zinc.compiler;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;

/**
 * Reads zinc.toml project configuration.
 *
 * zinc.toml is the project config for Zinc — primarily for the Python target.
 * Java projects continue to use build.mill.yaml directly.
 *
 * <pre>
 * [project]
 * name = "my-app"
 * version = "0.1.0"
 * main = "main.zn"
 *
 * [python]
 * version = ">=3.10"
 * deps = ["pandas>=2.0", "numpy>=2.0"]
 * </pre>
 */
public final class ZincConfig {

    // [project]
    public String name = "";
    public String version = "0.1.0";
    public String main = "main.zn";

    // [python]
    public String pythonVersion = ">=3.14";
    public List<String> pythonDeps = List.of();

    private ZincConfig() {}

    /**
     * Find zinc.toml by walking up from the given path.
     * Returns null if not found.
     */
    public static Path findConfigFile(Path start) {
        var dir = Files.isDirectory(start) ? start : start.getParent();
        while (dir != null) {
            var config = dir.resolve("zinc.toml");
            if (Files.exists(config)) return config;
            dir = dir.getParent();
        }
        return null;
    }

    /**
     * Parse a zinc.toml file.
     */
    public static ZincConfig parse(Path file) throws IOException {
        var config = new ZincConfig();
        var lines = Files.readAllLines(file);
        String section = "";

        for (var line : lines) {
            line = line.trim();
            if (line.isEmpty() || line.startsWith("#")) continue;

            if (line.startsWith("[") && line.endsWith("]")) {
                section = line.substring(1, line.length() - 1).trim();
                continue;
            }

            int eq = line.indexOf('=');
            if (eq < 0) continue;
            String key = line.substring(0, eq).trim();
            String value = line.substring(eq + 1).trim();

            if (value.startsWith("\"") && value.endsWith("\"")) {
                value = value.substring(1, value.length() - 1);
            }

            switch (section) {
                case "project" -> {
                    switch (key) {
                        case "name" -> config.name = value;
                        case "version" -> config.version = value;
                        case "main" -> config.main = value;
                    }
                }
                case "python" -> {
                    switch (key) {
                        case "version" -> config.pythonVersion = value;
                        case "deps" -> config.pythonDeps = parseArray(value);
                    }
                }
            }
        }

        // Default project name from config file location
        if (config.name.isEmpty()) {
            config.name = file.getParent().getFileName().toString();
        }

        return config;
    }

    private static List<String> parseArray(String value) {
        if (!value.startsWith("[") || !value.endsWith("]")) return List.of();
        var inner = value.substring(1, value.length() - 1).trim();
        if (inner.isEmpty()) return List.of();

        var items = new ArrayList<String>();
        for (var item : inner.split(",")) {
            var s = item.trim();
            if (s.startsWith("\"") && s.endsWith("\"")) {
                s = s.substring(1, s.length() - 1);
            }
            if (!s.isEmpty()) items.add(s);
        }
        return items;
    }

    /**
     * Generate requirements.txt from Python deps.
     */
    public String toRequirementsTxt() {
        return String.join("\n", pythonDeps) + "\n";
    }
}
