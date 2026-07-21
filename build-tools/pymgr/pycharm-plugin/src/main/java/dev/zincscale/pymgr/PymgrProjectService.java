package dev.zincscale.pymgr;

import com.google.gson.JsonArray;
import com.google.gson.JsonObject;
import com.intellij.codeInsight.daemon.DaemonCodeAnalyzer;
import com.intellij.notification.NotificationGroupManager;
import com.intellij.notification.NotificationType;
import com.intellij.openapi.Disposable;
import com.intellij.openapi.application.ApplicationManager;
import com.intellij.openapi.application.WriteAction;
import com.intellij.openapi.components.Service;
import com.intellij.openapi.project.Project;
import com.intellij.openapi.projectRoots.ProjectJdkTable;
import com.intellij.openapi.projectRoots.Sdk;
import com.intellij.openapi.roots.ProjectRootManager;
import com.intellij.openapi.ui.Messages;
import com.intellij.openapi.vfs.VirtualFileManager;
import com.intellij.util.concurrency.AppExecutorUtil;
import org.jetbrains.annotations.NotNull;

import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.TimeUnit;
import java.util.function.Consumer;

@Service(Service.Level.PROJECT)
public final class PymgrProjectService implements Disposable {
    private final Project project;
    private final PymgrRpcClient client;
    private final List<Runnable> listeners = new CopyOnWriteArrayList<>();
    private volatile String status = "pymgr is starting";
    private volatile long generation = -1;
    private volatile String interpreter = "";
    private ScheduledFuture<?> watcher;

    public PymgrProjectService(@NotNull Project project) {
        this.project = project;
        this.client = new PymgrRpcClient(project);
    }

    public void start() {
        String root = project.getBasePath();
        if (root == null || !Files.isRegularFile(Path.of(root, "pyproject.toml"))) {
            status = "No pyproject.toml was found for this project";
            changed();
            return;
        }
        JsonObject params = new JsonObject();
        params.addProperty("protocolVersion", "1.0");
        request("pymgr/initialize", params, this::acceptWorkspaceState);
        watcher = AppExecutorUtil.getAppScheduledExecutorService().scheduleWithFixedDelay(
                this::refreshStatus, 2, 2, TimeUnit.SECONDS);
    }

    public String getStatus() {
        return status;
    }

    public void addListener(@NotNull Runnable listener) {
        listeners.add(listener);
    }

    public void refreshStatus() {
        request("workspace/status", new JsonObject(), this::acceptWorkspaceState);
    }

    public void refreshProjectModel() {
        VirtualFileManager.getInstance().asyncRefresh(null);
        ApplicationManager.getApplication().invokeLater(
                () -> DaemonCodeAnalyzer.getInstance(project).restart("pymgr workspace refresh"));
        status = "Project files and Python analysis refresh requested";
        changed();
    }

    public void selectSynchronizedInterpreter() {
        if (interpreter.isBlank()) {
            notifyUser("No synchronized interpreter is recorded. Run pymgr sync first.", NotificationType.WARNING);
            return;
        }
        for (Sdk sdk : ProjectJdkTable.getInstance().getAllJdks()) {
            if (interpreter.equals(sdk.getHomePath())) {
                WriteAction.run(() -> ProjectRootManager.getInstance(project).setProjectSdk(sdk));
                status = "Selected interpreter " + interpreter;
                refreshProjectModel();
                return;
            }
        }
        notifyUser(
                "The synchronized interpreter is not configured in PyCharm: " + interpreter,
                NotificationType.WARNING);
    }

    public void sync() {
        JsonObject params = new JsonObject();
        params.addProperty("operation", "sync");
        request("dependencies/mutate", params, result -> {
            acceptWorkspaceState(result);
            refreshProjectModel();
        });
    }

    public void mutateDependency(String operation, String packageName) {
        JsonObject params = new JsonObject();
        params.addProperty("operation", operation);
        JsonArray packages = new JsonArray();
        if (packageName != null && !packageName.isBlank()) {
            packages.add(packageName.trim());
        }
        params.add("packages", packages);
        if ("update".equals(operation) && packages.isEmpty()) {
            params.addProperty("all", true);
        }
        request("dependencies/mutate", params, result -> {
            acceptWorkspaceState(result);
            refreshProjectModel();
        });
    }

    public void requestView(String method, JsonObject params, Consumer<String> display) {
        request(method, params, result -> display.accept(result.toString()));
    }

    public void requestRefactor(String method, JsonObject params, Consumer<String> display) {
        params.addProperty("apply", false);
        request(method, params, preview -> {
            display.accept(preview.toString());
            ApplicationManager.getApplication().invokeLater(() -> {
                int choice = Messages.showYesNoDialog(
                        project,
                        "Apply this pymgr refactor preview?\n\n" + preview,
                        "pymgr Refactor",
                        Messages.getQuestionIcon());
                if (choice == Messages.YES) {
                    params.addProperty("apply", true);
                    request(method, params, applied -> {
                        display.accept(applied.toString());
                        refreshProjectModel();
                    });
                }
            });
        });
    }

    private void request(String method, JsonObject params, Consumer<JsonObject> success) {
        client.request(method, params).whenComplete((result, error) -> {
            if (error != null) {
                status = "pymgr: " + error.getMessage();
                notifyUser(status, NotificationType.ERROR);
            } else {
                success.accept(result);
            }
            changed();
        });
    }

    private void acceptWorkspaceState(JsonObject result) {
        long nextGeneration = result.has("generation") ? result.get("generation").getAsLong() : generation;
        interpreter = result.has("python") ? result.get("python").getAsString() : interpreter;
        String workspaceStatus = result.has("workspaceStatus")
                ? result.get("workspaceStatus").getAsString()
                : result.has("status") ? result.get("status").getAsString() : "unknown";
        boolean synchronizedState = result.has("synchronized") && result.get("synchronized").getAsBoolean();
        status = "generation " + nextGeneration + " · " + workspaceStatus
                + (synchronizedState ? " · synchronized" : " · refresh required")
                + interpreterStatus();
        if (generation >= 0 && nextGeneration != generation) {
            refreshProjectModel();
        }
        generation = nextGeneration;
    }

    private String interpreterStatus() {
        Sdk sdk = ProjectRootManager.getInstance(project).getProjectSdk();
        if (interpreter.isBlank()) {
            return " · no synchronized interpreter";
        }
        if (sdk == null || !interpreter.equals(sdk.getHomePath())) {
            return " · PyCharm interpreter differs";
        }
        return " · interpreter verified";
    }

    private void notifyUser(String message, NotificationType type) {
        ApplicationManager.getApplication().invokeLater(() ->
                NotificationGroupManager.getInstance()
                        .getNotificationGroup("pymgr")
                        .createNotification(message, type)
                        .notify(project));
    }

    private void changed() {
        ApplicationManager.getApplication().invokeLater(() -> listeners.forEach(Runnable::run));
    }

    @Override
    public void dispose() {
        if (watcher != null) {
            watcher.cancel(false);
        }
        client.dispose();
    }
}
