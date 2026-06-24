package zinc;

/** Runtime facade. Sys.sleep throws no checked InterruptedException (unlike
 *  java.lang.Thread.sleep), so callers do not need a throws clause. Prelude stub. */
public final class Sys {
  private Sys() {}
  public static void sleep(long millis) { throw Tag.stub(); }
}
