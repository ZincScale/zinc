package zinc;

import java.util.*;
import java.util.stream.*;
import supervisors.Team;
import actors.Counter;
import actors.Greeter;

abstract public class Supervisor {
    private int reaperTimeoutMs = 10000;
    private Runnable reaperHandler = null;
    
    
    public int getReaperTimeoutMs() { return this.reaperTimeoutMs; }
    public void setReaperTimeoutMs(int reaperTimeoutMs) { this.reaperTimeoutMs = reaperTimeoutMs; }
    public Runnable getReaperHandler() { return this.reaperHandler; }
    public void setReaperHandler(Runnable reaperHandler) { this.reaperHandler = reaperHandler; }
    public abstract void start() throws Exception;
    
    public abstract void shutdown() throws Exception;
    
    public abstract void shutdown(long timeoutMs) throws Exception;
    
    public abstract void kill() throws Exception;
    
}
