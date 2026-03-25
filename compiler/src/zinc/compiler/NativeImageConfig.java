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
import java.net.URL;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.StandardCopyOption;
import java.util.ArrayList;
import java.util.List;

/**
 * Manages GraalVM reachability metadata for native-image builds.
 *
 * 1. Downloads the Oracle GraalVM reachability metadata repo (cached)
 * 2. Matches dependency jars to metadata entries
 * 3. Runs tracing agent as fallback for uncovered libraries
 */
public class NativeImageConfig {

    private static final String METADATA_REPO_URL =
        "https://github.com/oracle/graalvm-reachability-metadata/archive/refs/heads/master.tar.gz";
    private static final Path CACHE_DIR = Path.of(System.getProperty("user.home"), ".cache", "zinc", "graalvm-metadata");
    private static final Path METADATA_DIR = CACHE_DIR.resolve("graalvm-reachability-metadata-master/metadata");

    /**
     * Ensure the metadata repo is downloaded and cached.
     */
    public static void ensureMetadataRepo() {
        if (Files.exists(METADATA_DIR)) return;

        System.out.println("downloading GraalVM reachability metadata...");
        try {
            Files.createDirectories(CACHE_DIR);
            var tarball = CACHE_DIR.resolve("metadata.tar.gz");

            // Download
            try (InputStream in = new URL(METADATA_REPO_URL).openStream()) {
                Files.copy(in, tarball, StandardCopyOption.REPLACE_EXISTING);
            }

            // Extract
            var process = new ProcessBuilder("tar", "xzf", tarball.toString(), "-C", CACHE_DIR.toString())
                .inheritIO().start();
            process.waitFor();

            Files.deleteIfExists(tarball);
            System.out.println("metadata cached at: " + METADATA_DIR);
        } catch (Exception e) {
            System.err.println("warning: could not download metadata repo: " + e.getMessage());
        }
    }

    /**
     * Find metadata config directories for a list of dependency jars.
     * Matches jar names to metadata entries in the repo.
     */
    public static List<Path> findMetadataForDeps(List<String> classpathEntries) {
        var configs = new ArrayList<Path>();
        if (!Files.exists(METADATA_DIR)) return configs;

        for (var entry : classpathEntries) {
            var jar = Path.of(entry).getFileName().toString();
            // Extract group/artifact/version from jar name or path
            // Coursier path: ~/.cache/coursier/v1/.../group/artifact/version/artifact-version.jar
            var parts = extractMavenCoords(Path.of(entry));
            if (parts == null) continue;

            var groupId = parts[0];
            var artifactId = parts[1];
            var version = parts[2];

            // Look for metadata: metadata/groupId/artifactId/version/
            var metadataPath = METADATA_DIR.resolve(groupId).resolve(artifactId);
            if (!Files.exists(metadataPath)) continue;

            // Try exact version, then any available version
            var versionDir = metadataPath.resolve(version);
            if (Files.exists(versionDir)) {
                configs.add(versionDir);
            } else {
                // Use latest available version
                try (var versions = Files.list(metadataPath)) {
                    var latest = versions
                        .filter(Files::isDirectory)
                        .filter(d -> !d.getFileName().toString().equals("index.json"))
                        .sorted()
                        .reduce((a, b) -> b); // last = latest
                    latest.ifPresent(configs::add);
                } catch (IOException e) {
                    // skip
                }
            }
        }

        return configs;
    }

    /**
     * Build native-image args for metadata configuration.
     */
    public static List<String> buildNativeImageArgs(List<String> classpathEntries) {
        ensureMetadataRepo();
        var metadataDirs = findMetadataForDeps(classpathEntries);
        if (metadataDirs.isEmpty()) return List.of();

        var args = new ArrayList<String>();
        for (var dir : metadataDirs) {
            // Check for reachability-metadata.json (new format)
            if (Files.exists(dir.resolve("reachability-metadata.json"))) {
                args.add("-H:ConfigurationFileDirectories=" + dir);
            }
            // Check for individual config files (old format)
            if (Files.exists(dir.resolve("reflect-config.json"))) {
                args.add("-H:ReflectionConfigurationFiles=" + dir.resolve("reflect-config.json"));
            }
            if (Files.exists(dir.resolve("resource-config.json"))) {
                args.add("-H:ResourceConfigurationFiles=" + dir.resolve("resource-config.json"));
            }
            if (Files.exists(dir.resolve("jni-config.json"))) {
                args.add("-H:JNIConfigurationFiles=" + dir.resolve("jni-config.json"));
            }
        }

        if (!args.isEmpty()) {
            System.out.println("found metadata for " + metadataDirs.size() + " dependencies");
        }

        return args;
    }

    /**
     * Run the tracing agent to generate native-image config for uncovered libraries.
     * Runs the app on JVM with the agent, captures reflection/resource access.
     */
    public static Path runTracingAgent(String mainClass, String classpath, Path outputDir) {
        var configDir = outputDir.resolve("native-image-config");
        try {
            Files.createDirectories(configDir);
            System.out.println("running tracing agent...");
            var process = new ProcessBuilder(
                "java", "--enable-preview",
                "-agentlib:native-image-agent=config-output-dir=" + configDir,
                "-cp", classpath,
                mainClass)
                .inheritIO()
                .start();

            // Give it a few seconds to start up, then gracefully stop
            Thread.sleep(5000);
            process.destroy(); // SIGTERM — lets agent flush
            process.waitFor();

            // Remove lock file if leftover
            Files.deleteIfExists(configDir.resolve(".lock"));

            System.out.println("tracing config generated at: " + configDir);
            return configDir;
        } catch (Exception e) {
            System.err.println("warning: tracing agent failed: " + e.getMessage());
            return null;
        }
    }

    /**
     * Extract Maven coordinates from a Coursier cache path.
     * ~/.cache/coursier/v1/.../io/javalin/javalin/6.6.0/javalin-6.6.0.jar
     * → ["io.javalin", "javalin", "6.6.0"]
     */
    private static String[] extractMavenCoords(Path jarPath) {
        var pathStr = jarPath.toAbsolutePath().toString();

        // Coursier cache layout: .../group/parts/artifactId/version/artifact-version.jar
        if (pathStr.contains("/coursier/") || pathStr.contains("/maven2/")) {
            var parts = jarPath.toAbsolutePath();
            var nameCount = parts.getNameCount();
            if (nameCount >= 4) {
                var version = parts.getName(nameCount - 2).toString();
                var artifactId = parts.getName(nameCount - 3).toString();

                // Walk backwards to find the maven2/ marker, group is between maven2/ and artifactId
                var fullPath = parts.toString();
                var maven2Idx = fullPath.indexOf("/maven2/");
                if (maven2Idx >= 0) {
                    var afterMaven = fullPath.substring(maven2Idx + "/maven2/".length());
                    var segments = afterMaven.split("/");
                    // Last 3 segments are: artifactId/version/filename
                    // Everything before that is the groupId
                    if (segments.length >= 4) {
                        var groupParts = new ArrayList<String>();
                        for (int i = 0; i < segments.length - 3; i++) {
                            groupParts.add(segments[i]);
                        }
                        var groupId = String.join(".", groupParts);
                        return new String[]{groupId, artifactId, version};
                    }
                }
            }
        }

        return null;
    }
}
