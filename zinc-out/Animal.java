import java.util.*;
import java.util.stream.*;

public class Animal {
    private String name = "";
    private String sound = "";
    
    
    public String getName() { return this.name; }
    public void setName(String name) { this.name = name; }
    public String getSound() { return this.sound; }
    public void setSound(String sound) { this.sound = sound; }
    public String speak() {
        return name + " says " + sound;
    }
    
}
