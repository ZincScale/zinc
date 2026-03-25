package supervisors;

import java.util.*;
import java.util.stream.*;
import zinc.Supervisor;
import actors.Counter;
import zinc.Actor;
import actors.Greeter;

public class Team extends Supervisor {
    private final Counter c1;
    private final Counter c2;
    
    public Team(Counter c1, Counter c2) throws Exception {
        this.c1 = c1;
        this.c2 = c2;
    }
    
    
    public Counter getC1() { return this.c1; }
    public Counter getC2() { return this.c2; }
    public void start() throws Exception {
        if (c1 != null) {
            c1.setRunning(true);
            c1.setMailbox(new java.util.concurrent.ArrayBlockingQueue<>(c1.getMailboxCapacity()));
            c1.setActorThread(Thread.startVirtualThread(() -> {
                while (c1.getRunning()) {
                    try {
                        Runnable msg = c1.getMailbox().take();
                        msg.run();
                    } catch (InterruptedException e) {
                        Thread.currentThread().interrupt();
                        break;
                    } catch (Exception e) {
                        System.err.println("Actor error in c1: " + e.getMessage());
                    }
                }
            }));
        }
        if (c2 != null) {
            c2.setRunning(true);
            c2.setMailbox(new java.util.concurrent.ArrayBlockingQueue<>(c2.getMailboxCapacity()));
            c2.setActorThread(Thread.startVirtualThread(() -> {
                while (c2.getRunning()) {
                    try {
                        Runnable msg = c2.getMailbox().take();
                        msg.run();
                    } catch (InterruptedException e) {
                        Thread.currentThread().interrupt();
                        break;
                    } catch (Exception e) {
                        System.err.println("Actor error in c2: " + e.getMessage());
                    }
                }
            }));
        }
    }
    
    public void shutdown() throws Exception {
        if (c1 != null && c1.getActorThread() != null) {
            c1.setRunning(false);
            c1.getMailbox().add(() -> {});
            c1.getActorThread().join();
        }
        if (c2 != null && c2.getActorThread() != null) {
            c2.setRunning(false);
            c2.getMailbox().add(() -> {});
            c2.getActorThread().join();
        }
    }
    
    public void shutdown(long timeoutMs) throws Exception {
        if (c1 != null && c1.getActorThread() != null) {
            c1.setRunning(false);
            c1.getMailbox().add(() -> {});
            c1.getActorThread().join(timeoutMs);
            if (c1.getActorThread().isAlive()) { c1.getActorThread().interrupt(); }
        }
        if (c2 != null && c2.getActorThread() != null) {
            c2.setRunning(false);
            c2.getMailbox().add(() -> {});
            c2.getActorThread().join(timeoutMs);
            if (c2.getActorThread().isAlive()) { c2.getActorThread().interrupt(); }
        }
    }
    
    public void kill() throws Exception {
        java.util.List<Thread> killed = new java.util.ArrayList<>();
        if (c1 != null && c1.getActorThread() != null) {
            c1.setRunning(false);
            c1.getActorThread().interrupt();
            if (c1.getMailbox() != null) { c1.getMailbox().clear(); }
            killed.add(c1.getActorThread());
        }
        if (c2 != null && c2.getActorThread() != null) {
            c2.setRunning(false);
            c2.getActorThread().interrupt();
            if (c2.getMailbox() != null) { c2.getMailbox().clear(); }
            killed.add(c2.getActorThread());
        }
        Thread.sleep(getReaperTimeoutMs());
        for (var t : killed) {
            if (t.isAlive()) {
                if (getReaperHandler() != null) {
                    getReaperHandler().run();
                } else {
                    System.err.println("FATAL: Actor thread refused to terminate after kill. Forcing exit.");
                    System.exit(1);
                }
            }
        }
    }
}
