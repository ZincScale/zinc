package actors;

import java.util.*;
import java.util.stream.*;
import zinc.Actor;
import supervisors.Team;
import zinc.Supervisor;

public class Counter extends Actor {
    private int count = 0;
    
    public Counter(int start) throws Exception {
        count = start;
    }
    
    public void increment() throws Exception {
        if (getMailbox() != null) {
            getMailbox().add(() -> {
                try {
                    count += 1;
                } catch (Exception e) { throw (e instanceof RuntimeException re) ? re : new RuntimeException(e); }
            });
        } else {
            count += 1;
        }
    }
    
    public void add(int n) throws Exception {
        if (getMailbox() != null) {
            getMailbox().add(() -> {
                try {
                    count += n;
                } catch (Exception e) { throw (e instanceof RuntimeException re) ? re : new RuntimeException(e); }
            });
        } else {
            count += n;
        }
    }
    
    public int getCount() throws Exception {
        if (getMailbox() != null) {
            var _future = new java.util.concurrent.CompletableFuture<Integer>();
            getMailbox().add(() -> {
                try {
                    _future.complete(count);
                } catch (Exception e) { _future.completeExceptionally(e); }
            });
            return _future.get();
        } else {
            return count;
        }
    }
    
    public void reset() throws Exception {
        if (getMailbox() != null) {
            getMailbox().add(() -> {
                try {
                    count = 0;
                } catch (Exception e) { throw (e instanceof RuntimeException re) ? re : new RuntimeException(e); }
            });
        } else {
            count = 0;
        }
    }
    
}
