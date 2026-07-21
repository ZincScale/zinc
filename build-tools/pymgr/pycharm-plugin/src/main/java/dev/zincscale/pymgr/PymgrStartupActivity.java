package dev.zincscale.pymgr;

import com.intellij.openapi.project.Project;
import com.intellij.openapi.startup.StartupActivity;
import org.jetbrains.annotations.NotNull;

@SuppressWarnings("deprecation")
public final class PymgrStartupActivity implements StartupActivity.DumbAware {
    @Override
    public void runActivity(@NotNull Project project) {
        project.getService(PymgrProjectService.class).start();
    }
}
