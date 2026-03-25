package zinc;

import java.util.*;
import java.util.stream.*;
import java.util.concurrent.ArrayBlockingQueue;
import actors.Greeter;
import supervisors.Team;
import actors.Counter;

abstract public class Actor {
    private int mailboxCapacity = 1000;
    private ArrayBlockingQueue<Runnable> mailbox = null;
    private Thread actorThread = null;
    private boolean running = false;
    
    
    public int getMailboxCapacity() { return this.mailboxCapacity; }
    public void setMailboxCapacity(int mailboxCapacity) { this.mailboxCapacity = mailboxCapacity; }
    public ArrayBlockingQueue<Runnable> getMailbox() { return this.mailbox; }
    public void setMailbox(ArrayBlockingQueue<Runnable> mailbox) { this.mailbox = mailbox; }
    public Thread getActorThread() { return this.actorThread; }
    public void setActorThread(Thread actorThread) { this.actorThread = actorThread; }
    public boolean getRunning() { return this.running; }
    public void setRunning(boolean running) { this.running = running; }
}
