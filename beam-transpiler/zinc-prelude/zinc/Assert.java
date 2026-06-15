package zinc;

/** Test assertions. Assert.fails takes a zero-arg block expected to throw. Prelude stub. */
public final class Assert {
  private Assert() {}
  public static void equals(Object expected, Object actual) { throw Tag.stub(); }
  public static void isTrue(boolean cond) { throw Tag.stub(); }
  public static void fails(Block body) { throw Tag.stub(); }

  @FunctionalInterface
  public interface Block { void run() throws Exception; }
}
