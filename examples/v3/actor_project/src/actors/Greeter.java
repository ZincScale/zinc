package actors;

import java.util.*;
import java.util.stream.*;
import zinc.Actor;
import zinc.Supervisor;
import supervisors.Team;

public class Greeter extends Actor {
    private String prefix = "hello";
    
    public Greeter(String prefix) throws Exception {
        this.prefix = prefix;
    }
    
    public String greet(String name) throws Exception {
        if (getMailbox() != null) {
            var _future = new java.util.concurrent.CompletableFuture<String>();
            getMailbox().add(() -> {
                try {
                    _future.complete(prefix + " " + name);
                } catch (Exception e) { _future.completeExceptionally(e); }
            });
            return _future.get();
        } else {
            return prefix + " " + name;
        }
    }
    
}
