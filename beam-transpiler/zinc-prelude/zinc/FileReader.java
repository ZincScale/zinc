package zinc;

/** A pump Actor: reads a file line-by-line into a Channel (closing it at EOF), in its own
 *  process. Channel backpressure paces it, so it streams an arbitrarily large file in
 *  bounded memory. The source end of a multi-process pipeline (NiFi GetFile). Prelude stub. */
public final class FileReader {
  public FileReader(String path, Channel<String> out) { throw Tag.stub(); }
}
