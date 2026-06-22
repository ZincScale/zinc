package zinc;

/** Small JSON config facade. Config.decode(T, path) reads the file and uses the same
 * derived record codec as Json.decode(T, ...). */
public final class Config {
  private Config() {}
  public static <T> T decode(Class<T> type, String path) { throw Tag.stub(); }
}
