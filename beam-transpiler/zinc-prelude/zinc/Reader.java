package zinc;

/** A scoped line reader over an open file (raw + read-ahead), held in the current
 *  process. Scanner-style (no null): `while (r.hasNextLine()) r.nextLine()`. Use in
 *  try-with-resources so the fd is closed at block exit. Reads happen in-process, so a
 *  read->write loop is backpressured and bounded -- 8KB or 5GB, same code. Prelude stub. */
public final class Reader implements AutoCloseable {
  private Reader() {}
  public boolean hasNextLine() { throw Tag.stub(); }
  public String nextLine() { throw Tag.stub(); }
  @Override public void close() { throw Tag.stub(); }
}
