package zinc;

import java.util.List;

/** Basic file I/O over Erlang file/filelib. Three lifetimes:
 *   - whole-file (small files): readString/writeString/... slurp the entire file.
 *   - scoped streaming (large files): openReader/openWriter return AutoCloseable handles
 *     for try-with-resources -- in-process, backpressured, constant memory.
 *   - long-lived/shared: a Writer/Reader Actor (deferred).
 *  Failure is a zinc.io.IOException (value-or-throw over Erlang's {ok,_}/{error,_}).
 *  Prelude stub: bodies throw; the transpiler lowers calls to the 'zinc.io' module. */
public final class Files {
  private Files() {}

  // whole-file -- SMALL files only (slurps the whole thing into memory)
  public static String readString(String path) { throw Tag.stub(); }
  public static byte[] readBytes(String path) { throw Tag.stub(); }
  public static List<String> readLines(String path) { throw Tag.stub(); }
  public static void writeString(String path, String content) { throw Tag.stub(); }
  public static void appendString(String path, String content) { throw Tag.stub(); }
  public static void writeBytes(String path, byte[] content) { throw Tag.stub(); }

  // paths / directories
  public static boolean exists(String path) { throw Tag.stub(); }
  public static boolean isDirectory(String path) { throw Tag.stub(); }
  public static List<String> list(String dir) { throw Tag.stub(); }
  public static String join(String a, String b) { throw Tag.stub(); }
  public static String baseName(String path) { throw Tag.stub(); }
  public static String extension(String path) { throw Tag.stub(); }
  public static void createDirectories(String path) { throw Tag.stub(); }
  public static void delete(String path) { throw Tag.stub(); }
  // zinc int is an arbitrary-precision Erlang integer, so it holds any file size
  public static int size(String path) { throw Tag.stub(); }

  // scoped streaming handles -- the LARGE-file path. Use in try-with-resources so the
  // fd is closed at block exit: try (Reader r = Files.openReader(p)) { while ... }
  public static Reader openReader(String path) { throw Tag.stub(); }
  public static Writer openWriter(String path) { throw Tag.stub(); }
  public static Writer openAppender(String path) { throw Tag.stub(); }
}
