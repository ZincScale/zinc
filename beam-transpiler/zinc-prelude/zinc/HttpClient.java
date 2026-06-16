package zinc;

/** HTTP client over httpc. Prelude stub. */
public final class HttpClient {
  private HttpClient() {}
  public static Builder newBuilder() { throw Tag.stub(); }
  public HttpResponse send(HttpRequest req) { throw Tag.stub(); }
  public HttpStream openStream(HttpRequest req) { throw Tag.stub(); }

  public static final class Builder {
    private Builder() {}
    public Builder connectTimeout(int millis) { throw Tag.stub(); }
    public HttpClient build() { throw Tag.stub(); }
  }
}
