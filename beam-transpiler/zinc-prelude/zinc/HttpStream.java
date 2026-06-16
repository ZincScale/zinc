package zinc;

/** A streaming HTTP response body, held in the current process. Demand-driven
 *  (backpressured) so a slow consumer can't flood memory. Scanner-style (no null):
 *  `while (s.hasNextChunk()) s.nextChunk()`. Use in try-with-resources so the request is
 *  cancelled/closed at block exit. Pairs with Files.openWriter for bounded-memory
 *  HTTP->file streaming of arbitrarily large bodies. Prelude stub. */
public final class HttpStream implements AutoCloseable {
  private HttpStream() {}
  public boolean hasNextChunk() { throw Tag.stub(); }
  public byte[] nextChunk() { throw Tag.stub(); }
  public String header(String name) { throw Tag.stub(); }
  @Override public void close() { throw Tag.stub(); }
}
