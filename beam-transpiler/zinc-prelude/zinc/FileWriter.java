package zinc;

/** A sink pump: spawns a background process that drains a Channel into a file until the
 *  channel is closed (NiFi PutFile). `drain` is a static factory returning a handle so the
 *  caller can join() -- block until the pipeline has finished. No `new` to discard. Prelude
 *  stub. */
public final class FileWriter {
  private FileWriter() {}
  public static FileWriter drain(Channel<String> ch, String path) { throw Tag.stub(); }
  public void join() { throw Tag.stub(); }
}
