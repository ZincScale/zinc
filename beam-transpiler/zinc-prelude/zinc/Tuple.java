package zinc;

/** An Erlang tuple. Tuple.of(a, b, ...) builds it; get(i) reads a slot. Prelude stub. */
public final class Tuple {
  private Tuple() {}
  public static Tuple of(Object... elems) { throw Tag.stub(); }
  public Object get(int i) { throw Tag.stub(); }
}
