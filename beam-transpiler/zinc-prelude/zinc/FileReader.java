package zinc;

/** A source pump: spawns a background process that reads a file line-by-line into a Channel
 *  and closes it at EOF (NiFi GetFile). Static spawn-and-go -- like Thread.startVirtualThread,
 *  no `new` to discard. Channel backpressure paces it, so it streams an arbitrarily large
 *  file in bounded memory. Prelude stub. */
public final class FileReader {
  private FileReader() {}
  public static void pump(String path, Channel<String> ch) { throw Tag.stub(); }
}
