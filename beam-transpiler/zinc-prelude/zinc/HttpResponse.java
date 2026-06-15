package zinc;

/** HTTP response. Prelude stub. */
public final class HttpResponse {
  private HttpResponse() {}
  public int statusCode()        { throw Tag.stub(); }
  public String body()           { throw Tag.stub(); }
  public String header(String k) { throw Tag.stub(); }
}
