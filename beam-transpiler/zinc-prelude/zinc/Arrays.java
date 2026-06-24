package zinc;

import java.util.List;

/** BEAM array helper facade. Prelude stub; calls lower to array:* primitives. */
public final class Arrays {
  private Arrays() {}

  public static <T> List<T> asList(T[] array) { throw Tag.stub(); }
  public static <T> List<T> toList(T[] array) { throw Tag.stub(); }
  public static <T> T[] fromList(List<T> values) { throw Tag.stub(); }
  public static <T> int size(T[] array) { throw Tag.stub(); }
  public static <T> T get(T[] array, int index) { throw Tag.stub(); }
  public static <T> T[] set(T[] array, int index, T value) { throw Tag.stub(); }
}
