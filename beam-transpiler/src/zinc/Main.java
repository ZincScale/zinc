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
      List<Src> files = parseAll(in);

      // project-wide registries; type names and module names must be unique
      var classes = new LinkedHashMap<String, ClassInfo>();
      var records = new LinkedHashMap<String, Ast.RecordDecl>();
      var enums = new LinkedHashMap<String, Ast.EnumDecl>();
      var actors = new LinkedHashMap<String, Ast.ActorDecl>();
      Ast.ApplicationDecl application = null;
      var modules = new java.util.HashSet<String>(List.of("zinc_root_sup", "zinc_dyn_sup"));
      var typeNames = new java.util.HashSet<String>();
      var reserved = java.util.Set.of("System", "Thread", "Atom", "Tag", "Tuple", "Erlang",
          "HashMap", "Map", "ArrayList", "List", "Math", "Integer", "Arrays", "Object",
          "String", "Exception", "Actor", "Application", "Log",
          "HttpClient", "HttpRequest", "HttpResponse",
          "HttpException", "ConnectException", "TimeoutException", "Json",
          "Router", "Response", "Request", "HttpServer", "Handler");
      var actorMods = new LinkedHashMap<String, String>(); // simple name -> FQ module
      var exceptions = new LinkedHashMap<String, Ast.ExceptionDecl>();
      var excTags = new LinkedHashMap<String, String>();   // simple name -> FQ tag atom
      for (String[] bx : CodeGen.BUILTIN_EXCEPTIONS) {
        exceptions.put(bx[0], new Ast.ExceptionDecl(bx[0],
            List.of(new Ast.FieldDecl("String", "message", null))));
        excTags.put(bx[0], bx[1]);
      }
      var interfaces = new LinkedHashMap<String, Ast.InterfaceDecl>();
      interfaces.put("Handler", new Ast.InterfaceDecl("Handler",
          List.of(new Ast.MethodDecl("Response", "handle",
              List.of(new Ast.Param("Request", "req")), null))));
      var instClasses = new LinkedHashMap<String, Ast.InstanceClassDecl>();
      var instMods = new LinkedHashMap<String, String>();  // simple name -> FQ module
      for (Src src : files) {
        Program p = src.prog();
        String pkg = src.pkg();
        var names = new ArrayList<String>();
        p.classes().forEach(c -> names.add(c.name()));
        p.records().forEach(r -> names.add(r.name()));
        p.actors().forEach(a -> names.add(a.name()));
        p.enums().forEach(e2 -> names.add(e2.name()));
        if (p.application() != null) names.add(p.application().name());
        p.exceptions().forEach(x -> names.add(x.name()));
        p.interfaces().forEach(i -> names.add(i.name()));
        p.instanceClasses().forEach(c -> names.add(c.name()));
        for (String n : names) {
          if (reserved.contains(n)) throw new CompileError("'" + n + "' is a reserved name");
          if (!typeNames.add(n)) throw new CompileError("duplicate type name: " + n);
        }
        for (var c : p.classes()) {
          var methods = new LinkedHashMap<String, String>();
          for (var m : c.methods()) methods.put(m.name() + "/" + m.params().size(), m.retType());
          String mod = fqMod(pkg, c.name());
          classes.put(c.name(), new ClassInfo(mod, methods));
          if (!modules.add(mod)) {
            throw new CompileError("module name collision: " + c.name());
          }
        }
        p.records().forEach(r -> records.put(r.name(), r));
        p.enums().forEach(e2 -> enums.put(e2.name(), e2));
        for (var x : p.exceptions()) {
          exceptions.put(x.name(), x);
          excTags.put(x.name(), fqMod(pkg, x.name())); // tag = FQ name, no module emitted
        }
        p.interfaces().forEach(i -> interfaces.put(i.name(), i));
        for (var c : p.instanceClasses()) {
          instClasses.put(c.name(), c);
          String mod = fqMod(pkg, c.name());
          instMods.put(c.name(), mod);
          if (!modules.add(mod)) {
            throw new CompileError("module name collision: " + c.name());
          }
        }
        for (var a : p.actors()) {
          actors.put(a.name(), a);
          String mod = fqMod(pkg, a.name());
          actorMods.put(a.name(), mod);
          if (!modules.add(mod)) {
            throw new CompileError("module name collision: actor " + a.name());
          }
          if (!a.fields().isEmpty()) modules.add(mod + "_sup"); // supervisor pair
        }
        if (p.application() != null) {
          if (application != null) {
            throw new CompileError("a project has at most one Application: "
                + application.name() + " and " + p.application().name());
          }
          if (!pkg.isEmpty()) {
            throw new CompileError("v1: the Application must live in the root package");
          }
          application = p.application();
          if (!modules.add(application.erlMod())) {
            throw new CompileError("module name collision: " + application.name());
          }
        }
      }
      // nominal conformance: implements = every interface method present, signature match
      for (var c : instClasses.values()) {
        Ast.InterfaceDecl iface = interfaces.get(c.iface());
        if (iface == null) {
          throw new CompileError("class " + c.name() + " implements " + c.iface()
              + ": unknown interface");
        }
        for (var sig : iface.sigs()) {
          var impl = c.methods().stream().filter(m -> m.name().equals(sig.name())
              && m.params().size() == sig.params().size()).findFirst();
          if (impl.isEmpty()) {
            throw new CompileError("class " + c.name() + " implements " + c.iface()
                + " but is missing " + sig.retType() + " " + sig.name() + "/"
                + sig.params().size());
          }
          if (!impl.get().retType().equals(sig.retType())) {
            throw new CompileError("class " + c.name() + "." + sig.name()
                + ": return type " + impl.get().retType() + " does not match "
                + c.iface() + "'s " + sig.retType());
          }
        }
      }
      if (application != null) {
        if (!application.name().equals("Main")) {
          // v1: the entry module is hardwired to main:main()/main:run()
          throw new CompileError("v1: the Application class must be named Main");
        }
      } else {
        ClassInfo entry = classes.get("Main");
        if (entry == null || !entry.methods().containsKey("main/1")) {
          throw new CompileError(
              "project needs a class Main with main(String[] args), or an Application");
        }
      }

      // generate everything before writing anything: no partial output on a compile error
      boolean supervised = !actors.isEmpty() || application != null;
      var generated = new LinkedHashMap<String, String>();
      Ast.ApplicationDecl resolvedApp = null;
      boolean anyHttp = false;
      boolean anyServer = false;
      for (Src src : files) {
        Program resolved = Resolve.spawns(src.prog(), actors.keySet());
        if (resolved.application() != null) resolvedApp = resolved.application();
        var cg = new CodeGen(resolved, classes, records, enums, actors, actorMods,
            exceptions, excTags, interfaces, instClasses, instMods, supervised);
        generated.putAll(cg.generateAll());
        anyHttp |= cg.usedHttp();
        anyServer |= cg.usedServer();
      }
      if (anyHttp) generated.put("zinc.http", CodeGen.HTTP_SOURCE);
      if (anyServer) generated.put("zinc.httpserver", CodeGen.SERVER_SOURCE);
      if (supervised) {
        generated.put("zinc_dyn_sup", CodeGen.DYN_SUP_SOURCE);
        generated.put("zinc_root_sup", CodeGen.rootSupSource(resolvedApp, actors, actorMods));
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

  /** A parsed file plus its package (= directory path, Java convention). */
  record Src(Program prog, String pkg) {}

  /** FQ class name = module, as a lowercase dotted atom: util.MathUtil -> 'util.mathutil'. */
  static String fqMod(String pkg, String name) {
    return pkg.isEmpty() ? name.toLowerCase() : pkg + "." + name.toLowerCase();
  }

  private static List<Src> parseAll(Path in) throws IOException {
    var programs = new ArrayList<Src>();
    if (!Files.isDirectory(in)) {
      programs.add(new Src(parse(in), ""));
      return programs;
    }
    var srcFiles = new ArrayList<Path>();
    try (var walk = Files.walk(in)) {
      walk.filter(p -> p.toString().endsWith(".zinc")).sorted().forEach(srcFiles::add);
    }
    if (srcFiles.isEmpty()) throw new CompileError("no .zinc files under " + in);
    for (Path p : srcFiles) {
      Program prog = parse(p);
      Path rel = in.relativize(p);
      checkFileName(rel, prog);
      String pkg = rel.getParent() == null ? ""
          : rel.getParent().toString().replace('/', '.').toLowerCase();
      programs.add(new Src(prog, pkg));
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
    if (prog.application() != null) names.add(prog.application().name());
    if (!names.contains(stem)) {
      throw new CompileError(rel + " must declare a type named '" + stem + "'");
    }
  }

  private static Program parse(Path file) throws IOException {
    return new Parser(Lexer.lex(Files.readString(file))).parseProgram();
  }
}
