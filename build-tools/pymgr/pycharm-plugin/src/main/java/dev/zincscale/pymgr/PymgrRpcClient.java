package dev.zincscale.pymgr;

import com.google.gson.Gson;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import com.intellij.openapi.Disposable;
import com.intellij.openapi.project.Project;
import com.intellij.util.concurrency.AppExecutorUtil;
import org.jetbrains.annotations.NotNull;

import java.io.BufferedReader;
import java.io.BufferedWriter;
import java.io.IOException;
import java.io.InputStreamReader;
import java.io.OutputStreamWriter;
import java.nio.charset.StandardCharsets;
import java.util.Map;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.atomic.AtomicLong;

final class PymgrRpcClient implements Disposable {
    private final Project project;
    private final Gson gson = new Gson();
    private final AtomicLong requestIds = new AtomicLong();
    private final Map<Long, CompletableFuture<JsonObject>> pending = new ConcurrentHashMap<>();
    private Process process;
    private BufferedWriter writer;

    PymgrRpcClient(@NotNull Project project) {
        this.project = project;
    }

    synchronized void start() throws IOException {
        if (process != null && process.isAlive()) {
            return;
        }
        String root = project.getBasePath();
        if (root == null) {
            throw new IOException("Project has no local base path");
        }
        String executable = System.getenv().getOrDefault("PYMGR_EXECUTABLE", "pymgr");
        process = new ProcessBuilder(executable, "--root", root, "serve", "--stdio")
                .directory(new java.io.File(root))
                .start();
        writer = new BufferedWriter(new OutputStreamWriter(process.getOutputStream(), StandardCharsets.UTF_8));
        AppExecutorUtil.getAppExecutorService().execute(this::readResponses);
        AppExecutorUtil.getAppExecutorService().execute(this::drainErrors);
    }

    CompletableFuture<JsonObject> request(@NotNull String method, @NotNull JsonObject params) {
        long id = requestIds.incrementAndGet();
        CompletableFuture<JsonObject> future = new CompletableFuture<>();
        pending.put(id, future);
        JsonObject message = new JsonObject();
        message.addProperty("jsonrpc", "2.0");
        message.addProperty("id", id);
        message.addProperty("method", method);
        message.add("params", params);
        try {
            synchronized (this) {
                start();
                writer.write(gson.toJson(message));
                writer.newLine();
                writer.flush();
            }
        } catch (IOException error) {
            pending.remove(id);
            future.completeExceptionally(error);
        }
        return future;
    }

    private void readResponses() {
        try (BufferedReader reader = new BufferedReader(
                new InputStreamReader(process.getInputStream(), StandardCharsets.UTF_8))) {
            String line;
            while ((line = reader.readLine()) != null) {
                JsonObject response = JsonParser.parseString(line).getAsJsonObject();
                if (!response.has("id") || response.get("id").isJsonNull()) {
                    continue;
                }
                long id = response.get("id").getAsLong();
                CompletableFuture<JsonObject> future = pending.remove(id);
                if (future == null) {
                    continue;
                }
                if (response.has("error")) {
                    future.completeExceptionally(
                            new IOException(response.getAsJsonObject("error").get("message").getAsString()));
                } else {
                    future.complete(response.getAsJsonObject("result"));
                }
            }
        } catch (Exception error) {
            failPending(error);
        }
    }

    private void drainErrors() {
        try (BufferedReader reader = new BufferedReader(
                new InputStreamReader(process.getErrorStream(), StandardCharsets.UTF_8))) {
            while (reader.readLine() != null) {
                // stderr is drained so the child cannot block; RPC errors arrive as JSON.
            }
        } catch (IOException ignored) {
        }
    }

    private void failPending(Exception error) {
        pending.values().forEach(future -> future.completeExceptionally(error));
        pending.clear();
    }

    @Override
    public synchronized void dispose() {
        if (process != null) {
            process.destroy();
        }
        failPending(new IOException("pymgr RPC service stopped"));
    }
}
