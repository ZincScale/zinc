package zinc;

/** A scoped file writer over an open file (raw + delayed_write), held in the current
 *  process. Use in try-with-resources so the fd is flushed and closed at block exit:
 *  `try (Writer w = Files.openWriter(p)) { w.writeLine(...); }`. Writes happen in-process
 *  (synchronous), so a read->write loop is backpressured and bounded. Prelude stub. */
public final class Writer implements AutoCloseable {
  private Writer() {}
  public void write(String s) { throw Tag.stub(); }
  public void writeLine(String s) { throw Tag.stub(); }
  @Override public void close() { throw Tag.stub(); }
}
