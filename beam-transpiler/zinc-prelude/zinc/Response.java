package zinc;

/** HTTP response builder. Prelude stub. */
public final class Response {
  private Response() {}
  public static Response ok(String body)  { throw Tag.stub(); }
  public static Response status(int code) { throw Tag.stub(); }
  public Response header(String k, String v) { throw Tag.stub(); }
}
