package zinc;

/** Base for HTTP client failures (ConnectException, TimeoutException). Prelude stub. */
public class HttpException extends RuntimeException {
  public HttpException(String message) { super(message); }
}
