package zinc;

/** An Erlang atom. Built only from a compile-time literal: Tag.of("ok"). Prelude stub. */
public final class Tag {
  private Tag() {}
  public static Tag of(String literal) { throw stub(); }
  static RuntimeException stub() { return new UnsupportedOperationException("zinc prelude stub"); }
}
