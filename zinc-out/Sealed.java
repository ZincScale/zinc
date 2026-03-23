import java.util.*;
import java.util.stream.*;

public class Sealed {
    public static void main(String[] args) throws Exception {
        var c = new Circle(5.0);
        var r = new Rect(3.0, 4.0);
        System.out.println(c);
        System.out.println(r);
    }
}
