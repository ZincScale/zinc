import java.util.*;
import java.util.stream.*;

public class Collections {
    public static void main(String[] args) throws Exception {
        List<String> names = new java.util.ArrayList<>(java.util.List.of("Alice", "Bob", "Charlie", "Dave"));
        names.add("Eve");
        System.out.println("Count: " + names.size());
        for (var name : names) {
            System.out.println("Hello, " + name);
        }
        Map<String, Integer> ages = new java.util.HashMap<>(java.util.Map.of("Alice", 30, "Bob", 25, "Charlie", 35));
        ages.put("Dave", 28);
        for (var entry : ages.entrySet()) {
            System.out.println(entry.getKey() + " is " + entry.getValue());
        }
        List<Integer> numbers = new java.util.ArrayList<>(java.util.List.of(1, 2, 3, 4, 5, 6, 7, 8, 9, 10));
        var evens = numbers.stream().filter(x -> java.util.Objects.equals(x % 2, 0)).toList();
        System.out.println("Evens: " + evens);
    }
}
