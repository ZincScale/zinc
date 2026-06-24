package zinc;

import java.util.List;

/** BEAM List helper facade. Prelude stub; calls lower to lists:* primitives. */
public final class Lists {
  private Lists() {}

  public static <T> List<T> slice(List<T> xs, int start, int length) { throw Tag.stub(); }
  public static <T> int size(List<T> xs) { throw Tag.stub(); }
  public static <T> boolean isEmpty(List<T> xs) { throw Tag.stub(); }
  public static <T> T get(List<T> xs, int index) { throw Tag.stub(); }
  public static <T> T first(List<T> xs) { throw Tag.stub(); }
  public static <T> T last(List<T> xs) { throw Tag.stub(); }
  public static <T> List<T> prepend(T value, List<T> xs) { throw Tag.stub(); }
  public static <T> List<T> reverse(List<T> xs) { throw Tag.stub(); }
  public static <T> List<T> concat(List<T> left, List<T> right) { throw Tag.stub(); }
}
