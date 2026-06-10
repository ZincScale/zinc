package zinc;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;

public class Main {
  public static void main(String[] args) throws IOException {
    if (args.length != 2) {
      System.err.println("usage: Main <file.src> <outdir>");
      System.exit(2);
    }
    try {
      String src = Files.readString(Path.of(args[0]));
      var toks = Lexer.lex(src);
      var fns = new Parser(toks).parseProgram();
      var modules = new CodeGen(fns).generateModules();
      Path outDir = Files.createDirectories(Path.of(args[1]));
      for (var e : modules.entrySet()) {
        Path path = outDir.resolve(e.getKey() + ".erl");
        Files.writeString(path, e.getValue());
        System.out.println(path);
      }
    } catch (CompileError e) {
      System.err.println(e.getMessage());
      System.exit(1);
    }
  }
}
