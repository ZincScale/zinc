package zinc;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.Map;
import java.util.Set;
import zinc.Ast.Program;

public class Main {
  public static void main(String[] args) throws IOException {
    if (args.length != 2) {
      System.err.println("usage: Main <file.src | project-dir> <outdir>");
      System.exit(2);
    }
    try {
      Path in = Path.of(args[0]);
      Map<String, Program> modules = Files.isDirectory(in) ? loadProject(in) : loadSingle(in);
      if (!modules.containsKey("main")) {
        throw new CompileError("project has no main.src at its root");
      }

      var moduleFns = new HashMap<String, Set<String>>();
      for (var e : modules.entrySet()) {
        var fns = new LinkedHashSet<String>();
        for (var fn : e.getValue().fns()) fns.add(fn.name() + "/" + fn.params().size());
        moduleFns.put(e.getKey(), fns);
      }
      if (!moduleFns.get("main").contains("main/0")) {
        throw new CompileError("entry module must define fn main()");
      }

      // generate everything before writing anything: no partial output on a compile error
      var generated = new LinkedHashMap<String, String>();
      for (var e : modules.entrySet()) {
        generated.put(e.getKey(), new CodeGen(e.getKey(), e.getValue(), moduleFns).generate());
      }
      Path outDir = Files.createDirectories(Path.of(args[1]));
      for (var e : generated.entrySet()) {
        Path path = outDir.resolve(e.getKey() + ".erl");
        Files.writeString(path, e.getValue());
        System.out.println(path);
      }
    } catch (CompileError e) {
      System.err.println(e.getMessage());
      System.exit(1);
    }
  }

  /** Single-file mode: the file is the entry module, whatever its name. */
  private static Map<String, Program> loadSingle(Path file) throws IOException {
    var modules = new LinkedHashMap<String, Program>();
    modules.put("main", parse(file));
    return modules;
  }

  /**
   * Project mode: every *.src under the root is one module; util/math.src -> util_math.
   * main.src at the root is the entry module.
   */
  private static Map<String, Program> loadProject(Path root) throws IOException {
    var files = new ArrayList<Path>();
    try (var walk = Files.walk(root)) {
      walk.filter(p -> p.toString().endsWith(".src")).sorted().forEach(files::add);
    }
    if (files.isEmpty()) throw new CompileError("no .src files under " + root);
    var modules = new LinkedHashMap<String, Program>();
    for (Path p : files) {
      String name = moduleName(root.relativize(p));
      if (modules.put(name, parse(p)) != null) {
        throw new CompileError("module name collision: two files map to '" + name + "'");
      }
    }
    return modules;
  }

  private static String moduleName(Path rel) {
    String s = rel.toString();
    s = s.substring(0, s.length() - ".src".length()).replace(java.io.File.separatorChar, '_');
    if (!s.matches("[a-z][a-z0-9_]*")) {
      throw new CompileError("bad module path '" + rel
          + "': each segment must match [a-z][a-z0-9_]*");
    }
    return s;
  }

  private static Program parse(Path file) throws IOException {
    return new Parser(Lexer.lex(Files.readString(file))).parseProgram();
  }
}
