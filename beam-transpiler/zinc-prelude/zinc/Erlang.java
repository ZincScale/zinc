package zinc;

/** FFI helpers. Erlang.ok(x) unwraps {ok, V} | matches the ok atom. Prelude stub. */
public final class Erlang {
  private Erlang() {}
  public static <T> T ok(Object wrapped) { throw Tag.stub(); }
}
