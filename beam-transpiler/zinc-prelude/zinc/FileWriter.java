package zinc;

/** A pump Actor: drains a Channel into a file until the channel is closed, in its own
 *  process. The sink end of a multi-process pipeline (NiFi PutFile). join() blocks until
 *  the drain has finished (the kick/join idiom) so a caller can wait for completion.
 *  Prelude stub. */
public final class FileWriter {
  public FileWriter(Channel<String> in, String path) { throw Tag.stub(); }
  public void join() { throw Tag.stub(); }
}
