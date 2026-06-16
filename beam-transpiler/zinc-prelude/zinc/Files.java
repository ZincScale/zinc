package zinc;

import java.util.List;
import java.util.function.BiFunction;
import java.util.function.Consumer;

/** Basic file I/O over Erlang file/filelib. Streaming is explicit and first-class
 *  (forEachLine/foldLines/forEachChunk — slice 2); these whole-file ops read/write the
 *  ENTIRE file and are for SMALL files (config-sized). Large data must stream. Failure
 *  is a zinc.io.IOException (the value-or-throw idiom over Erlang's {ok,_}/{error,_}).
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
  public static void createDirectories(String path) { throw Tag.stub(); }
  public static void delete(String path) { throw Tag.stub(); }
  // zinc int is an arbitrary-precision Erlang integer, so it holds any file size
  public static int size(String path) { throw Tag.stub(); }

  // streaming reads -- the LARGE-file path. Constant memory, in-process loop, handle
  // closed for you. Use these (not readString) for anything that can grow.
  public static void forEachLine(String path, Consumer<String> action) { throw Tag.stub(); }
  public static <T> T foldLines(String path, T acc, BiFunction<T, String, T> fn) { throw Tag.stub(); }
  public static void forEachChunk(String path, int size, Consumer<byte[]> action) { throw Tag.stub(); }

  // scoped streaming WRITE -- handle held open for the lambda, closed for you. Writes are
  // in-process (synchronous) so a read->write loop is backpressured and bounded. Use this,
  // not appendString-per-line, to stream large output without reopening the file each time.
  public static void withWriter(String path, Consumer<Writer> action) { throw Tag.stub(); }
  public static void withAppender(String path, Consumer<Writer> action) { throw Tag.stub(); }
}
