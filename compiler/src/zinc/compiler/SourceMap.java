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
import java.util.*;
import java.util.regex.Pattern;

/**
 * Maps generated Java/Python line numbers back to Zinc source line numbers.
 * Scans generated files for {@code @zn:N} markers embedded as comments during emission.
 */
public class SourceMap {

    private static final Pattern ZN_MARKER = Pattern.compile("@zn:(\\d+)");
    private static final Pattern PYTHON_FRAME = Pattern.compile(
        "(\\s*File \")([^\"]+)(\", line )(\\d+)(.*)");

    private final String sourceFile;   // e.g., "hello.zn"
    private final TreeMap<Integer, Integer> lineMap; // generated_line → zinc_line (sorted)

    public SourceMap(String sourceFile, TreeMap<Integer, Integer> lineMap) {
        this.sourceFile = sourceFile;
        this.lineMap = lineMap;
    }

    /** Build a SourceMap by scanning a generated file for @zn:N markers. */
    public static SourceMap fromGeneratedFile(Path generatedFile, String sourceFile) {
        var map = new TreeMap<Integer, Integer>();
        try {
            var lines = Files.readAllLines(generatedFile);
            for (int i = 0; i < lines.size(); i++) {
                var matcher = ZN_MARKER.matcher(lines.get(i));
                if (matcher.find()) {
                    int znLine = Integer.parseInt(matcher.group(1));
                    // The marker comment is on line i+1 (1-based), the actual statement follows
                    // Map the NEXT line (the statement) to the zinc line
                    map.put(i + 2, znLine);
                    // Also map the comment line itself
                    map.put(i + 1, znLine);
                }
            }
        } catch (IOException e) {
            // Return empty map on read failure
        }
        return new SourceMap(sourceFile, map);
    }

    /** Build a SourceMap from an already-rendered source string. */
    public static SourceMap fromRendered(String source, String sourceFile) {
        var map = new TreeMap<Integer, Integer>();
        var lines = source.split("\n", -1);
        for (int i = 0; i < lines.length; i++) {
            var matcher = ZN_MARKER.matcher(lines[i]);
            if (matcher.find()) {
                int znLine = Integer.parseInt(matcher.group(1));
                map.put(i + 2, znLine);
                map.put(i + 1, znLine);
            }
        }
        return new SourceMap(sourceFile, map);
    }

    /**
     * Look up the Zinc source line for a given generated line number.
     * Returns the closest mapping at or before the given line, or -1 if none.
     */
    public int lookupSourceLine(int generatedLine) {
        var entry = lineMap.floorEntry(generatedLine);
        return entry != null ? entry.getValue() : -1;
    }

    public String sourceFile() { return sourceFile; }
    public boolean isEmpty() { return lineMap.isEmpty(); }

    /**
     * Compact string encoding: "file.zn:15=3,17=5,19=7"
     * Used for embedding in generated code as a static constant.
     */
    public String toCompactString() {
        var sb = new StringBuilder(sourceFile).append(":");
        // Deduplicate: only keep the first (comment line) mapping for each zinc line
        var seen = new HashSet<String>();
        boolean first = true;
        for (var entry : lineMap.entrySet()) {
            String key = entry.getKey() + "=" + entry.getValue();
            if (seen.add(key)) {
                if (!first) sb.append(",");
                sb.append(entry.getKey()).append("=").append(entry.getValue());
                first = false;
            }
        }
        return sb.toString();
    }

    /**
     * Build a Python dict literal from the line map: {10: 3, 12: 5, 14: 7}
     */
    public String toPythonDict() {
        var sb = new StringBuilder("{");
        boolean first = true;
        for (var entry : lineMap.entrySet()) {
            if (!first) sb.append(", ");
            sb.append(entry.getKey()).append(": ").append(entry.getValue());
            first = false;
        }
        sb.append("}");
        return sb.toString();
    }

    // --- Java trace rewriting ---

    /**
     * Rewrite a Java exception's stack trace to show Zinc source locations.
     * @param ex the thrown exception
     * @param maps class name → SourceMap (e.g., "Main" → map for main.zn)
     * @return formatted error string with rewritten trace
     */
    public static String rewriteJavaTrace(Throwable ex, Map<String, SourceMap> maps) {
        var sb = new StringBuilder();
        sb.append(ex.getClass().getName());
        if (ex.getMessage() != null) {
            sb.append(": ").append(ex.getMessage());
        }
        sb.append("\n");

        for (var frame : ex.getStackTrace()) {
            String className = frame.getClassName();
            // Strip package prefix to get simple class name
            String simpleName = className.contains(".")
                ? className.substring(className.lastIndexOf('.') + 1)
                : className;

            var map = maps.get(simpleName);
            if (map != null && frame.getLineNumber() > 0) {
                int znLine = map.lookupSourceLine(frame.getLineNumber());
                if (znLine > 0) {
                    sb.append("    at ").append(map.sourceFile).append(":").append(znLine);
                    if (frame.getMethodName() != null) {
                        String method = frame.getMethodName().equals("main") ? "main" : frame.getMethodName();
                        sb.append(" (").append(method).append(")");
                    }
                    sb.append("\n");
                    continue;
                }
            }
            // Skip internal frames for cleaner output
            if (className.startsWith("java.") || className.startsWith("jdk.")
                || className.startsWith("sun.") || className.startsWith("com.github.javaparser")
                || className.startsWith("zinc.compiler.")) {
                continue;
            }
            sb.append("    at ").append(frame).append("\n");
        }
        return sb.toString();
    }

    // --- Python trace rewriting ---

    /**
     * Rewrite Python traceback stderr to show Zinc source locations.
     * @param stderr the raw stderr output from the Python process
     * @param maps generated .py filename (e.g., "hello.py") → SourceMap
     * @return rewritten stderr with Zinc line numbers
     */
    public static String rewritePythonTrace(String stderr, Map<String, SourceMap> maps) {
        var sb = new StringBuilder();
        for (var line : stderr.split("\n", -1)) {
            var matcher = PYTHON_FRAME.matcher(line);
            if (matcher.matches()) {
                String filePath = matcher.group(2);
                int pyLine = Integer.parseInt(matcher.group(4));
                String rest = matcher.group(5);

                // Extract just the filename from the path
                String fileName = filePath;
                int lastSlash = filePath.lastIndexOf('/');
                if (lastSlash >= 0) fileName = filePath.substring(lastSlash + 1);

                var map = maps.get(fileName);
                if (map != null) {
                    int znLine = map.lookupSourceLine(pyLine);
                    if (znLine > 0) {
                        sb.append(matcher.group(1))
                            .append(map.sourceFile)
                            .append(matcher.group(3))
                            .append(znLine)
                            .append(rest)
                            .append("\n");
                        continue;
                    }
                }
            }
            sb.append(line).append("\n");
        }
        // Remove trailing newline added by split
        if (sb.length() > 0 && sb.charAt(sb.length() - 1) == '\n'
            && !stderr.isEmpty() && stderr.charAt(stderr.length() - 1) != '\n') {
            sb.setLength(sb.length() - 1);
        }
        return sb.toString();
    }
}
