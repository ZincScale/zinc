import java.util.*;
import java.util.stream.*;

public class Streams {
    public static void main(String[] args) throws Exception {
        var numbers = new java.util.ArrayList<>(java.util.List.of(1, 2, 3, 4, 5, 6, 7, 8, 9, 10));
        var evens = numbers.stream().filter(x -> java.util.Objects.equals(x % 2, 0)).toList();
        System.out.println("Evens: " + evens);
        var big = numbers.stream().filter(_it -> _it > 5).toList();
        System.out.println("Big: " + big);
        var doubled = numbers.stream().map(_it -> _it * 2).toList();
        System.out.println("Doubled: " + doubled);
        var total = numbers.stream().filter(_it -> _it > 5).map(_it -> _it * 10).mapToInt(Integer::intValue).sum();
        System.out.println("Sum of >5 * 10: " + total);
        var hasNine = numbers.stream().anyMatch(_it -> _it > 8);
        System.out.println("Has >8: " + hasNine);
        var first = numbers.stream().filter(_it -> _it > 7).findFirst().orElse(null);
        System.out.println("First >7: " + first);
    }
}
