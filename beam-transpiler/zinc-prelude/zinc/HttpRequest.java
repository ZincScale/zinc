package zinc;

/** HTTP request builder. Prelude stub. */
public final class HttpRequest {
  private HttpRequest() {}
  public static Builder newBuilder(String url) { throw Tag.stub(); }

  public static final class Builder {
    private Builder() {}
    public Builder GET()              { throw Tag.stub(); }
    public Builder POST(String body)  { throw Tag.stub(); }
    public Builder header(String k, String v) { throw Tag.stub(); }
    public Builder timeout(int millis) { throw Tag.stub(); }
    public HttpRequest build()        { throw Tag.stub(); }
  }
}
