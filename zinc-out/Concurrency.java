import java.util.*;
import java.util.stream.*;

public class Concurrency {
    public static void main(String[] args) throws Exception {
        Thread.startVirtualThread(() -> { System.out.println("background task started"); });
        Thread.sleep(50);
        var numbers = new java.util.ArrayList<>(java.util.List.of(1, 2, 3, 4, 5));
        try (var _scope = java.util.concurrent.StructuredTaskScope.open()) {
            for (var i : numbers) {
                _scope.fork(() -> {
                    System.out.println("parallel: " + i);
                    return null;
                });
            }
            _scope.join();
        }
    }
}
