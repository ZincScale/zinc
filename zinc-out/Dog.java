import java.util.*;
import java.util.stream.*;

public class Dog extends Animal {
    private String breed = "";
    
    
    public String getBreed() { return this.breed; }
    public void setBreed(String breed) { this.breed = breed; }
    @Override
    public String toString() {
        return "Dog(" + getName() + ", " + getBreed() + ")";
    }
    
}
