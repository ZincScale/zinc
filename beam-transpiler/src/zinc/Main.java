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
      System.err.println("usage: Main <File.zinc | project-dir> <outdir>");
      System.exit(2);
    }
    try {
      Path in = Path.of(args[0]);
      List<Program> files = parseAll(in);

      // project-wide registries; type names and module names must be unique
      var classes = new LinkedHashMap<String, ClassInfo>();
      var records = new LinkedHashMap<String, Ast.RecordDecl>();
      var enums = new LinkedHashMap<String, Ast.EnumDecl>();
      var actors = new LinkedHashMap<String, Ast.ActorDecl>();
      var modules = new java.util.HashSet<String>(List.of("actor_sup"));
      var typeNames = new java.util.HashSet<String>();
      var reserved = java.util.Set.of("System", "Thread", "Atom", "Tuple", "Erlang",
          "HashMap", "Map", "ArrayList", "List", "Math", "Integer", "Arrays", "Object",
          "String", "Exception", "Actor", "Application");
      for (Program p : files) {
        var names = new ArrayList<String>();
        p.classes().forEach(c -> names.add(c.name()));
        p.records().forEach(r -> names.add(r.name()));
        p.actors().forEach(a -> names.add(a.name()));
        p.enums().forEach(e2 -> names.add(e2.name()));
        for (String n : names) {
          if (reserved.contains(n)) throw new CompileError("'" + n + "' is a reserved name");
          if (!typeNames.add(n)) throw new CompileError("duplicate type name: " + n);
        }
        for (var c : p.classes()) {
          var methods = new LinkedHashMap<String, String>();
          for (var m : c.methods()) methods.put(m.name() + "/" + m.params().size(), m.retType());
          classes.put(c.name(), new ClassInfo(c.erlMod(), methods));
          if (!modules.add(c.erlMod())) {
            throw new CompileError("module name collision: " + c.name());
          }
        }
        p.records().forEach(r -> records.put(r.name(), r));
        p.enums().forEach(e2 -> enums.put(e2.name(), e2));
        for (var a : p.actors()) {
          actors.put(a.name(), a);
          if (!modules.add(a.erlMod())) {
            throw new CompileError("module name collision: actor " + a.name());
          }
        }
      }
      ClassInfo entry = classes.get("Main");
      if (entry == null || !entry.methods().containsKey("main/1")) {
        throw new CompileError("project needs a class Main with main(String[] args)");
      }

      // generate everything before writing anything: no partial output on a compile error
      boolean hasActors = !actors.isEmpty();
      var generated = new LinkedHashMap<String, String>();
      for (Program p : files) {
        Program resolved = Resolve.spawns(p, actors.keySet());
        generated.putAll(
            new CodeGen(resolved, classes, records, enums, actors, hasActors).generateAll());
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
      walk.filter(p -> p.toString().endsWith(".zinc")).sorted().forEach(srcFiles::add);
    }
    if (srcFiles.isEmpty()) throw new CompileError("no .zinc files under " + in);
    for (Path p : srcFiles) {
      Program prog = parse(p);
      checkFileName(in.relativize(p), prog);
      programs.add(prog);
    }
    return programs;
  }

  /** Java convention: File.zinc declares its eponymous public type. */
  private static void checkFileName(Path rel, Program prog) {
    String stem = rel.getFileName().toString();
    stem = stem.substring(0, stem.length() - ".zinc".length());
    var names = new ArrayList<String>();
    prog.classes().forEach(c -> names.add(c.name()));
    prog.records().forEach(r -> names.add(r.name()));
    prog.actors().forEach(a -> names.add(a.name()));
    prog.enums().forEach(e -> names.add(e.name()));
    if (!names.contains(stem)) {
      throw new CompileError(rel + " must declare a type named '" + stem + "'");
    }
  }

  private static Program parse(Path file) throws IOException {
    return new Parser(Lexer.lex(Files.readString(file))).parseProgram();
  }
}
