package dev.zincscale.pymgr;

import com.google.gson.JsonObject;
import com.intellij.openapi.project.Project;
import com.intellij.openapi.ui.Messages;
import com.intellij.openapi.wm.ToolWindow;
import com.intellij.openapi.wm.ToolWindowFactory;
import com.intellij.ui.components.JBScrollPane;
import com.intellij.ui.components.JBTextArea;
import com.intellij.ui.content.Content;
import com.intellij.ui.content.ContentFactory;
import org.jetbrains.annotations.NotNull;

import javax.swing.JButton;
import javax.swing.JLabel;
import javax.swing.JPanel;
import java.awt.BorderLayout;
import java.awt.FlowLayout;

public final class PymgrToolWindowFactory implements ToolWindowFactory {
    @Override
    public void createToolWindowContent(@NotNull Project project, @NotNull ToolWindow toolWindow) {
        PymgrProjectService service = project.getService(PymgrProjectService.class);
        JPanel panel = new JPanel(new BorderLayout(8, 8));
        JLabel status = new JLabel(service.getStatus());
        JBTextArea output = new JBTextArea();
        output.setEditable(false);
        output.setLineWrap(true);
        output.setWrapStyleWord(true);
        JPanel actions = new JPanel(new FlowLayout(FlowLayout.LEFT));

        button(actions, "Refresh", service::refreshStatus);
        button(actions, "Sync", service::sync);
        button(actions, "Use Interpreter", service::selectSynchronizedInterpreter);
        button(actions, "Doctor", () -> service.requestView("workspace/doctor", object(), output::setText));
        button(actions, "Modules", () -> service.requestView("modules/list", object(), output::setText));
        button(actions, "Cycles", () -> service.requestView("imports/cycles", object(), output::setText));
        button(actions, "Trace Report", () -> service.requestView("trace/report", object(), output::setText));
        button(actions, "Loop Result", () -> service.requestView("loops/comparison", object(), output::setText));
        button(actions, "Add", () -> dependency(project, service, "add"));
        button(actions, "Remove", () -> dependency(project, service, "remove"));
        button(actions, "Update All", () -> service.mutateDependency("update", null));
        button(actions, "Loop", () -> promptRequest(project, service, output, "Loop file.py:line", "loops/explain", "location"));
        button(actions, "Uses", () -> promptRequest(project, service, output, "Qualified symbol", "uses/query", "symbol"));
        button(actions, "Callers", () -> promptRequest(project, service, output, "Qualified symbol", "callers/query", "symbol"));
        button(actions, "Move", () -> move(project, service, output));
        button(actions, "Rename", () -> rename(project, service, output));

        service.addListener(() -> status.setText(service.getStatus()));
        panel.add(status, BorderLayout.NORTH);
        panel.add(new JBScrollPane(output), BorderLayout.CENTER);
        panel.add(actions, BorderLayout.SOUTH);
        Content content = ContentFactory.getInstance().createContent(panel, "Workspace", false);
        toolWindow.getContentManager().addContent(content);
    }

    private static void dependency(Project project, PymgrProjectService service, String operation) {
        String value = Messages.showInputDialog(project, "Package requirement", "pymgr " + operation, null);
        if (value != null && !value.isBlank()) {
            service.mutateDependency(operation, value);
        }
    }

    private static void promptRequest(
            Project project,
            PymgrProjectService service,
            JBTextArea output,
            String prompt,
            String method,
            String key) {
        String value = Messages.showInputDialog(project, prompt, "pymgr", null);
        if (value == null || value.isBlank()) {
            return;
        }
        JsonObject params = new JsonObject();
        params.addProperty(key, value.trim());
        service.requestView(method, params, output::setText);
    }

    private static void move(Project project, PymgrProjectService service, JBTextArea output) {
        String oldModule = Messages.showInputDialog(project, "Current module", "pymgr Move", null);
        if (oldModule == null || oldModule.isBlank()) {
            return;
        }
        String newModule = Messages.showInputDialog(project, "New module", "pymgr Move", null);
        if (newModule == null || newModule.isBlank()) {
            return;
        }
        JsonObject params = new JsonObject();
        params.addProperty("oldModule", oldModule.trim());
        params.addProperty("newModule", newModule.trim());
        service.requestRefactor("refactors/move", params, output::setText);
    }

    private static void rename(Project project, PymgrProjectService service, JBTextArea output) {
        String symbol = Messages.showInputDialog(project, "Qualified symbol", "pymgr Rename", null);
        if (symbol == null || symbol.isBlank()) {
            return;
        }
        String newName = Messages.showInputDialog(project, "New symbol name", "pymgr Rename", null);
        if (newName == null || newName.isBlank()) {
            return;
        }
        JsonObject params = new JsonObject();
        params.addProperty("qualifiedSymbol", symbol.trim());
        params.addProperty("newName", newName.trim());
        service.requestRefactor("refactors/rename", params, output::setText);
    }

    private static JsonObject object() {
        return new JsonObject();
    }

    private static void button(JPanel panel, String label, Runnable action) {
        JButton button = new JButton(label);
        button.addActionListener(event -> action.run());
        panel.add(button);
    }
}
