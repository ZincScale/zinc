import java.util.*;
import java.util.stream.*;

public class Hello {
    public static String greet(String name) {
        return "Hello, " + name + "!";
    }
    
    public static void main(String[] args) throws Exception {
        System.out.println(greet("World"));
        System.out.println(greet("Zinc"));
        int x = 42;
        System.out.println("The answer is " + x);
    }
    
}
