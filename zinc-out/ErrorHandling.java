import java.util.*;
import java.util.stream.*;

public class ErrorHandling {
    public static int divide(int a, int b) {
        if (java.util.Objects.equals(b, 0)) {
            throw new RuntimeException("division by zero");
        }
        return a / b;
    }
    
    public static void main(String[] args) throws Exception {
        Object result;
        try { result = divide(10, 0); } catch (Exception err) {
            result = -1;
        }
        System.out.println("divide(10, 0) or -1 = " + result);
        Object ok;
        try { ok = divide(10, 2); } catch (Exception err) {
            ok = 0;
        }
        System.out.println("divide(10, 2) = " + ok);
        int x = 42;
        if (x > 0) {
            System.out.println("positive");
        } else {
            System.out.println("non-positive");
        }
        switch (x) {
            case 0 -> {
                System.out.println("zero");
            }
            default -> {
                System.out.println("nonzero");
            }
        }
    }
}
