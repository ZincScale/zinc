package zinc;

/** A scoped file writer handle, valid only inside a Files.withWriter / withAppender
 *  lambda. Writes go straight to the open fd in the calling process (synchronous =
 *  backpressured, bounded memory); the handle is opened and closed for you. Prelude stub. */
public final class Writer {
  private Writer() {}
  public void write(String s) { throw Tag.stub(); }
  public void writeLine(String s) { throw Tag.stub(); }
}
