package zinc;

/** Incoming HTTP request. Prelude stub. */
public final class Request {
  private Request() {}
  public String method()              { throw Tag.stub(); }
  public String path()                { throw Tag.stub(); }
  public String pathParam(String k)   { throw Tag.stub(); }
  public String queryParam(String k)  { throw Tag.stub(); }
  public String header(String k)      { throw Tag.stub(); }
  public String body()                { throw Tag.stub(); }
}
