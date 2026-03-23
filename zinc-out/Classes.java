import java.util.*;
import java.util.stream.*;

public class Classes {
    public static void main(String[] args) throws Exception {
        var u = new User("Alice", "alice@example.com", 30);
        System.out.println(u);
        var d = new Dog();
        d.setName("Rex");
        d.setSound("Woof");
        d.setBreed("Lab");
        System.out.println(d.speak());
        System.out.println(d);
    }
}
