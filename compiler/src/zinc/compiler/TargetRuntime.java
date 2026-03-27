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
 * Target runtime configuration for Zinc compilation.
 *
 * Zinc source is target-agnostic for core language features. When a .zn file
 * imports ecosystem-specific libraries, the target runtime determines how
 * those imports resolve. Different zinc-flow workers can target different
 * runtimes — you don't mix Java and Python in the same compilation unit.
 *
 * Three import categories:
 * 1. Zinc stdlib — maps to the right library per target (e.g., zinc.http → java.net.http OR httpx)
 * 2. Target-native — passes through directly (e.g., import pandas, import io.javalin.Javalin)
 * 3. Java standard library — mapped to Python equivalents when targeting Python
 */
public sealed interface TargetRuntime permits TargetRuntime.Java, TargetRuntime.Python {

    /** Resolve a Zinc import to a target-native import string, or null to drop it. */
    String resolveImport(String zincImport);

    /** The file extension for output files. */
    String fileExtension();

    /** Name for display. */
    String name();

    // -------------------------------------------------------------------------

    record Java() implements TargetRuntime {
        @Override public String fileExtension() { return ".java"; }
        @Override public String name() { return "java"; }

        @Override
        public String resolveImport(String zincImport) {
            // Zinc stdlib → Java
            String stdlib = ZINC_TO_JAVA.get(zincImport);
            if (stdlib != null) return stdlib;

            // Zinc stdlib wildcard: zinc.* → mapped
            for (var entry : ZINC_TO_JAVA.entrySet()) {
                if (zincImport.startsWith(entry.getKey().replace(".*", ""))) {
                    return entry.getValue();
                }
            }

            // Java-native imports pass through
            if (zincImport.startsWith("java.") || zincImport.startsWith("javax.")
                || zincImport.startsWith("jakarta.")) {
                return zincImport;
            }

            // Maven ecosystem imports pass through (com.*, org.*, io.*, etc.)
            return zincImport;
        }
    }

    record Python() implements TargetRuntime {
        @Override public String fileExtension() { return ".py"; }
        @Override public String name() { return "python"; }

        @Override
        public String resolveImport(String zincImport) {
            // Zinc stdlib → Python
            String stdlib = ZINC_TO_PYTHON.get(zincImport);
            if (stdlib != null) return stdlib;

            // Zinc stdlib wildcard
            for (var entry : ZINC_TO_PYTHON.entrySet()) {
                if (zincImport.startsWith(entry.getKey().replace(".*", ""))) {
                    return entry.getValue();
                }
            }

            // Java standard library → Python equivalents
            String javaMapped = JAVA_TO_PYTHON.get(zincImport);
            if (javaMapped != null) return javaMapped;

            // Java stdlib wildcards
            for (var entry : JAVA_TO_PYTHON_PREFIX.entrySet()) {
                if (zincImport.startsWith(entry.getKey())) {
                    return entry.getValue();
                }
            }

            // Drop remaining java/javax imports — no Python equivalent
            if (zincImport.startsWith("java.") || zincImport.startsWith("javax.")
                || zincImport.startsWith("jakarta.")) {
                return null;
            }

            // Python-native imports pass through (pandas, numpy, httpx, etc.)
            // Convert dot-path to Python import: "models.User" → "from models import User"
            int lastDot = zincImport.lastIndexOf('.');
            if (lastDot > 0) {
                String module = zincImport.substring(0, lastDot);
                String name = zincImport.substring(lastDot + 1);
                if (name.equals("*")) {
                    return "from " + module + " import *";
                }
                return "from " + module + " import " + name;
            }
            return "import " + zincImport;
        }
    }

    // =========================================================================
    // Declarative mappings — add new mappings here, not in emitter code
    // =========================================================================

    /** Zinc stdlib → Java imports */
    Map<String, String> ZINC_TO_JAVA = Map.ofEntries(
        // Collections (auto-imported in Java, no explicit import needed)
        Map.entry("zinc.collections", "java.util.*"),
        Map.entry("zinc.collections.*", "java.util.*"),

        // I/O
        Map.entry("zinc.io", "java.nio.file.*"),
        Map.entry("zinc.io.*", "java.nio.file.*"),
        Map.entry("zinc.io.Path", "java.nio.file.Path"),

        // HTTP
        Map.entry("zinc.http", "java.net.http.*"),
        Map.entry("zinc.http.*", "java.net.http.*"),
        Map.entry("zinc.http.HttpClient", "java.net.http.HttpClient"),

        // JSON
        Map.entry("zinc.json", "com.fasterxml.jackson.databind.*"),
        Map.entry("zinc.json.*", "com.fasterxml.jackson.databind.*"),

        // Time
        Map.entry("zinc.time", "java.time.*"),
        Map.entry("zinc.time.*", "java.time.*"),

        // Math
        Map.entry("zinc.math", "java.lang.Math"),
        Map.entry("zinc.math.*", "java.lang.Math"),

        // Concurrency
        Map.entry("zinc.concurrent", "java.util.concurrent.*"),
        Map.entry("zinc.concurrent.*", "java.util.concurrent.*")
    );

    /** Zinc stdlib → Python imports */
    Map<String, String> ZINC_TO_PYTHON = Map.ofEntries(
        // Collections — builtins, no import needed
        Map.entry("zinc.collections", null),
        Map.entry("zinc.collections.*", null),

        // I/O
        Map.entry("zinc.io", "from pathlib import Path"),
        Map.entry("zinc.io.*", "from pathlib import Path"),
        Map.entry("zinc.io.Path", "from pathlib import Path"),

        // HTTP
        Map.entry("zinc.http", "import httpx"),
        Map.entry("zinc.http.*", "import httpx"),
        Map.entry("zinc.http.HttpClient", "import httpx"),

        // JSON
        Map.entry("zinc.json", "import msgspec"),
        Map.entry("zinc.json.*", "import msgspec"),

        // Time
        Map.entry("zinc.time", "from datetime import datetime, timedelta, date, time"),
        Map.entry("zinc.time.*", "from datetime import datetime, timedelta, date, time"),

        // Math
        Map.entry("zinc.math", "import math"),
        Map.entry("zinc.math.*", "import math"),

        // Concurrency
        Map.entry("zinc.concurrent", "from concurrent.futures import ThreadPoolExecutor, Future"),
        Map.entry("zinc.concurrent.*", "from concurrent.futures import ThreadPoolExecutor, Future")
    );

    /** Java standard library → Python equivalents (specific classes) */
    Map<String, String> JAVA_TO_PYTHON = Map.ofEntries(
        Map.entry("java.time.Instant", "from datetime import datetime"),
        Map.entry("java.time.LocalDate", "from datetime import date"),
        Map.entry("java.time.LocalTime", "from datetime import time"),
        Map.entry("java.time.Duration", "from datetime import timedelta"),
        Map.entry("java.math.BigDecimal", "from decimal import Decimal"),
        Map.entry("java.math.BigInteger", "from decimal import Decimal"),
        Map.entry("java.nio.file.Path", "from pathlib import Path"),
        Map.entry("java.nio.file.Files", "from pathlib import Path"),
        Map.entry("java.net.http.HttpClient", "import httpx"),
        Map.entry("java.net.http.HttpRequest", "import httpx"),
        Map.entry("java.net.http.HttpResponse", "import httpx"),
        Map.entry("java.util.regex.Pattern", "import re"),
        Map.entry("java.util.concurrent.CompletableFuture", "from concurrent.futures import Future"),
        Map.entry("java.util.concurrent.ExecutorService", "from concurrent.futures import ThreadPoolExecutor")
    );

    /** Java standard library → Python equivalents (package prefixes) */
    Map<String, String> JAVA_TO_PYTHON_PREFIX = Map.ofEntries(
        Map.entry("java.util.stream", null),       // Streams are implicit in Python
        Map.entry("java.util.function", null),      // Lambdas are native in Python
        Map.entry("java.util.concurrent", "from concurrent.futures import ThreadPoolExecutor, Future"),
        Map.entry("java.util", null),               // Collections are builtins
        Map.entry("java.io", null),                 // Use pathlib
        Map.entry("java.nio.file", "from pathlib import Path"),
        Map.entry("java.nio", null),                // Rarely needed
        Map.entry("java.time", "from datetime import datetime, timedelta, date, time"),
        Map.entry("java.math", "import math"),
        Map.entry("java.net.http", "import httpx"),
        Map.entry("java.net", null),                // Low-level, rarely used directly
        Map.entry("java.lang", null)                // Builtins
    );
}
