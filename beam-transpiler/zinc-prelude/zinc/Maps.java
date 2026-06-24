package zinc;

import java.util.List;
import java.util.Map;

/** BEAM Map helper facade. Prelude stub; calls lower to maps:* primitives. */
public final class Maps {
  private Maps() {}

  public static <K, V> V get(Map<K, V> map, K key) { throw Tag.stub(); }
  public static <K, V> V getOrDefault(Map<K, V> map, K key, V fallback) { throw Tag.stub(); }
  public static <K, V> Map<K, V> put(Map<K, V> map, K key, V value) { throw Tag.stub(); }
  public static <K, V> Map<K, V> remove(Map<K, V> map, K key) { throw Tag.stub(); }
  public static <K, V> boolean containsKey(Map<K, V> map, K key) { throw Tag.stub(); }
  public static <K, V> int size(Map<K, V> map) { throw Tag.stub(); }
  public static <K, V> boolean isEmpty(Map<K, V> map) { throw Tag.stub(); }
  public static <K, V> List<K> keys(Map<K, V> map) { throw Tag.stub(); }
  public static <K, V> List<V> values(Map<K, V> map) { throw Tag.stub(); }
  public static <K, V> List<Object> entries(Map<K, V> map) { throw Tag.stub(); }
  public static <K, V> Map<K, V> merge(Map<K, V> left, Map<K, V> right) { throw Tag.stub(); }
}
