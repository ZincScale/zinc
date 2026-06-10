package zinc;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import zinc.Ast.Program;
import zinc.Ast.RecordDecl;
import zinc.CodeGen.ClassInfo;

public class Main {
  public static void main(String[] args) throws IOException {
    if (args.length != 2) {
      System.err.println("usage: Main <File.src | project-dir> <outdir>");
      System.exit(2);
    }
    try {
      Path in = Path.of(args[0]);
      List<Program> files = parseAll(in);

      // project-wide registries; every module name must be unique
      var classes = new LinkedHashMap<String, ClassInfo>();
      var records = new LinkedHashMap<String, RecordDecl>();
      var modules = new java.util.HashSet<String>(List.of("actor_sup"));
      var reserved = java.util.Set.of("System", "Thread", "Atom", "Tuple", "Erlang",
          "HashMap", "Map", "ArrayList", "List", "Math", "Integer", "String", "Exception");
      boolean hasActors = false;
      for (Program p : files) {
        var names = new ArrayList<String>();
        p.classes().forEach(c -> names.add(c.name()));
        p.records().forEach(r -> names.add(r.name()));
        p.actors().forEach(a -> names.add(a.name()));
        for (String n : names) {
          if (reserved.contains(n)) throw new CompileError("'" + n + "' is a reserved name");
        }
        for (var c : p.classes()) {
          var methods = new LinkedHashMap<String, String>();
          for (var m : c.methods()) methods.put(m.name() + "/" + m.params().size(), m.retType());
          if (classes.put(c.name(), new ClassInfo(c.erlMod(), methods)) != null
              || !modules.add(c.erlMod())) {
            throw new CompileError("duplicate class/module name: " + c.name());
          }
        }
        for (var r : p.records()) {
          if (records.put(r.name(), r) != null) {
            throw new CompileError("duplicate record name: " + r.name());
          }
        }
        for (var a : p.actors()) {
          hasActors = true;
          if (!modules.add(a.erlMod())) {
            throw new CompileError("actor name '" + a.name()
                + "' collides with a class, another actor, or a reserved name");
          }
        }
      }
      ClassInfo entry = classes.get("Main");
      if (entry == null || !entry.methods().containsKey("main/1")) {
        throw new CompileError("project needs a class Main with main(String[] args)");
      }

      // generate everything before writing anything: no partial output on a compile error
      var generated = new LinkedHashMap<String, String>();
      for (Program p : files) {
        generated.putAll(new CodeGen(p, classes, records, hasActors).generateAll());
      }
      if (hasActors) generated.put("actor_sup", CodeGen.SUP_SOURCE);

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

  private static List<Program> parseAll(Path in) throws IOException {
    var programs = new ArrayList<Program>();
    if (!Files.isDirectory(in)) {
      programs.add(parse(in));
      return programs;
    }
    var srcFiles = new ArrayList<Path>();
    try (var walk = Files.walk(in)) {
      walk.filter(p -> p.toString().endsWith(".src")).sorted().forEach(srcFiles::add);
    }
    if (srcFiles.isEmpty()) throw new CompileError("no .src files under " + in);
    for (Path p : srcFiles) {
      Program prog = parse(p);
      checkFileName(in.relativize(p), prog);
      programs.add(prog);
    }
    return programs;
  }

  /** Java convention: File.src declares its eponymous public type. */
  private static void checkFileName(Path rel, Program prog) {
    String stem = rel.getFileName().toString();
    stem = stem.substring(0, stem.length() - ".src".length());
    var names = new ArrayList<String>();
    prog.classes().forEach(c -> names.add(c.name()));
    prog.records().forEach(r -> names.add(r.name()));
    prog.actors().forEach(a -> names.add(a.name()));
    if (!names.contains(stem)) {
      throw new CompileError(rel + " must declare a type named '" + stem + "'");
    }
  }

  private static Program parse(Path file) throws IOException {
    return new Parser(Lexer.lex(Files.readString(file))).parseProgram();
  }
}
