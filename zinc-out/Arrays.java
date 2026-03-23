import java.util.*;
import java.util.stream.*;

public class Arrays {
    public static int sum(int[] numbers) {
        int total = 0;
        for (var n : numbers) {
            total = total + n;
        }
        return total;
    }
    
    public static void main(String[] args) throws Exception {
        int[] nums = new int[] {10, 20, 30, 40, 50};
        System.out.println("first: " + nums[0]);
        System.out.println("length: " + nums.length);
        System.out.println("sum: " + sum(nums));
        String[] names = new String[] {"Alice", "Bob", "Charlie"};
        for (var name : names) {
            System.out.println("Hello, " + name + "!");
        }
        int[] empty = new int[0];
        System.out.println("empty length: " + empty.length);
    }
    
}
