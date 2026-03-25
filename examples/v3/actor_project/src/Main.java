import java.util.*;
import java.util.stream.*;
import actors.Counter;
import actors.Greeter;
import supervisors.Team;
import zinc.Actor;
import zinc.Supervisor;

public class Main {
    public static void main(String[] args) throws Exception {
        var counter = new Counter(0);
        var greeter = new Greeter("hi");
        counter.increment();
        counter.increment();
        counter.add(8);
        var n = counter.getCount();
        System.out.println("counter: " + n);
        var msg = greeter.greet("world");
        System.out.println(msg);
        counter.reset();
        var after = counter.getCount();
        System.out.println("after reset: " + after);
        var s1 = new Counter(0);
        var s2 = new Counter(0);
        var team = new Team(s1, s2);
        team.start();
        s1.increment();
        s2.increment();
        s2.increment();
        Thread.sleep(100);
        var t1 = s1.getCount();
        var t2 = s2.getCount();
        System.out.println("supervised: " + t1 + ", " + t2);
        team.shutdown();
        System.out.println("team shutdown");
        System.out.println("Multi-file actors OK");
    }
    
}
