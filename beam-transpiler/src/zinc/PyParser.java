package zinc;

import java.util.ArrayList;
import java.util.HashSet;
import java.util.List;
import java.util.Set;
import zinc.Ast.*;

/**
 * Parser for the .zn surface. It accepts the original braces-Python shape plus the
 * canonical Zinc aliases being promoted for BEAM. Both emit the SAME {@link Ast} the
 * legal-Java {@link Parser} produces, so Resolve + CodeGen run unchanged. The expression
 * grammar is identical (C-style operators); only declarations/statements differ:
 *   - top-level `def`s (incl. main + helpers) -> a synthetic `class Main { static ... }`
 *     (mirrors the Java surface's `public class Main { public static void main(...) }`)
 *   - `if`/`while`/`for` take no parens; `for x in EXPR` is Python-style
 *   - `x = e` is a declaration (inferred `var`) the first time a name is seen, else assign
 *   - `print(...)` desugars to the System.out.println node CodeGen already lowers
 */
final class PyParser {
  private final List<Token> toks;
  private final String fileClass;                 // eponymous class for top-level defs
  private int pos = 0;
  private Set<String> declared = new HashSet<>(); // locals seen in the current method
  private ApplicationDecl application;            // the one `class Main(Application)`, if any
  private final List<InterfaceDecl> interfaces = new ArrayList<>();
  private final List<InstanceClassDecl> instanceClasses = new ArrayList<>();
  private final List<ExceptionDecl> exceptions = new ArrayList<>();
  private final List<RecordDecl> records = new ArrayList<>();
  private final List<SealedDecl> sealeds = new ArrayList<>();
  private final List<EnumDecl> enums = new ArrayList<>();

  PyParser(List<Token> toks) {
    this(toks, "Main");
  }

  PyParser(List<Token> toks, String fileClass) {
    this.toks = toks;
    this.fileClass = fileClass;
  }

  private Token cur() {
    return toks.get(pos);
  }

  private Token advance() {
    return toks.get(pos++);
  }

  private boolean check(TokKind k) {
    return cur().kind() == k;
  }

  private boolean checkIdent(String text) {
    return check(TokKind.IDENT) && cur().text().equals(text);
  }

  private boolean matchIdent(String text) {
    if (checkIdent(text)) {
      pos++;
      return true;
    }
    return false;
  }

  private boolean match(TokKind k) {
    if (check(k)) {
      pos++;
      return true;
    }
    return false;
  }

  private Token expect(TokKind k, String what) {
    if (!check(k)) {
      throw new CompileError("Parse error: expected " + what + " but got " + cur().kind()
          + " \"" + cur().text() + "\" at line " + cur().line());
    }
    return advance();
  }

  private void skipSemis() {
    while (check(TokKind.SEMI)) {
      advance();
    }
  }

  private void skipPub() {
    matchIdent("pub");
  }

  // ---- declarations ----

  Program parseProgram() {
    skipSemis();
    var imports = new ArrayList<Import>();
    var topDefs = new ArrayList<MethodDecl>(); // become static methods of class Main
    var topStmts = new ArrayList<Stmt>();       // become Main.main for script-style files
    var actors = new ArrayList<ActorDecl>();
    var topDeclared = new HashSet<String>();
    declared = topDeclared;
    while (!check(TokKind.EOF)) {
      skipPub();
      if (checkIdent("def") || looksLikeZincDef() || looksLikeTupleZincDef()) {
        topDefs.add(parseDef(true));
        declared = topDeclared;
      } else if (checkIdent("class")) {
        parseClass(actors);
        declared = topDeclared;
      } else if (check(TokKind.KW_IMPORT) || checkIdent("from")) {
        parseImports(imports);
      } else if (checkIdent("interface")) {
        parseInterface();
      } else if (checkIdent("record")) {
        parseRecord();
      } else if (checkIdent("enum")) {
        parseEnum();
      } else if (checkIdent("sealed")) {
        parseSealed();
      } else {
        topStmts.add(parseStatement());
        topDeclared = new HashSet<>(declared);
      }
      skipSemis();
    }
    var classes = new ArrayList<ClassDecl>();
    if (application != null) {
      if (!topDefs.isEmpty() || !topStmts.isEmpty()) {
        throw new CompileError("an Application program has no top-level code — helpers "
            + "live on Actors (the Application is the boundary)");
      }
    } else if (!topDefs.isEmpty() || !topStmts.isEmpty()) {
      if (!topStmts.isEmpty() && topDefs.stream().anyMatch(m -> m.name().equals("main"))) {
        throw new CompileError("a file cannot mix top-level statements with main()");
      }
      if (!topStmts.isEmpty()) {
        topDefs.add(new MethodDecl("void", "main", List.of(new Param("String[]", "args")),
            new Block(topStmts), Set.of("public", "static")));
      }
      // a file with main or top-level statements is the entry; otherwise top-level defs
      // live on a class named after the file (Fmt.banner(...)).
      boolean hasMain = topDefs.stream().anyMatch(m -> m.name().equals("main"));
      classes.add(new ClassDecl(hasMain ? "Main" : fileClass, topDefs));
    }
    var program = new Program(imports, classes, records, actors, enums, application,
        exceptions, interfaces, instanceClasses, List.of(), sealeds);
    return PyInfer.infer(program); // resolve `infer` return types from method bodies
  }

  private boolean looksLikeZincDef() {
    if (!check(TokKind.IDENT)) return false;
    String head = cur().text();
    if (head.equals("class") || head.equals("interface") || head.equals("record")
        || head.equals("enum") || head.equals("sealed") || head.equals("from")
        || head.equals("import") || head.equals("def")) {
      return false;
    }
    int i = skipTypeAhead(pos);
    if (toks.get(i).kind() == TokKind.QUESTION) i++;
    if (toks.get(i).kind() != TokKind.IDENT) return false;
    i++;
    if (toks.get(i).kind() == TokKind.LT) {
      i = genericHeadEnd(i - 1);
    }
    return toks.get(i).kind() == TokKind.LPAREN;
  }

  private boolean looksLikeTupleZincDef() {
    if (!check(TokKind.LPAREN)) return false;
    int i = pos + 1;
    int depth = 1;
    boolean comma = false;
    while (depth > 0 && toks.get(i).kind() != TokKind.EOF) {
      if (toks.get(i).kind() == TokKind.LPAREN) depth++;
      else if (toks.get(i).kind() == TokKind.RPAREN) depth--;
      else if (depth == 1 && toks.get(i).kind() == TokKind.COMMA) comma = true;
      i++;
    }
    return comma && toks.get(i).kind() == TokKind.IDENT
        && toks.get(i + 1).kind() == TokKind.LPAREN;
  }

  private Param parseNamedType(String what) {
    if (toks.get(pos + 1).kind() == TokKind.COLON) {
      String name = expect(TokKind.IDENT, what + " name").text();
      advance(); // ':'
      return new Param(parseType(), name);
    }
    String type = parseType();
    String name = expect(TokKind.IDENT, what + " name").text();
    return new Param(type, name);
  }

  /** `record Point(x: int, y: int)` or `record Pair<A, B>(A a, B b)` -> immutable map. */
  private void parseRecord() {
    expect(TokKind.IDENT, "'record'"); // 'record'
    String name = expect(TokKind.IDENT, "record name").text();
    List<String> typeParams = parseTypeParamNames();
    var comps = new ArrayList<Param>();
    expect(TokKind.LPAREN, "'('");
    if (!check(TokKind.RPAREN)) {
      do {
        comps.add(parseNamedType("component"));
      } while (match(TokKind.COMMA));
    }
    expect(TokKind.RPAREN, "')'");
    records.add(new RecordDecl(name, typeParams, comps));
  }

  private List<String> parseTypeParamNames() {
    if (!match(TokKind.LT)) return List.of();
    var names = new ArrayList<String>();
    do {
      names.add(expect(TokKind.IDENT, "type parameter").text());
    } while (match(TokKind.COMMA));
    expect(TokKind.GT, "'>'");
    return names;
  }

  /** `sealed T { V1(f: A)  V2(String g, C h) }` -> algebraic union; each variant is a
   *  record-shaped constructor that lowers to a tagged tuple; matched by variant pattern. */
  private void parseSealed() {
    expect(TokKind.IDENT, "'sealed'");
    String name = expect(TokKind.IDENT, "sealed type name").text();
    expect(TokKind.LBRACE, "'{'");
    skipSemis();
    var variants = new ArrayList<RecordDecl>();
    while (!check(TokKind.RBRACE) && !check(TokKind.EOF)) {
      String vname = expect(TokKind.IDENT, "variant name").text();
      var comps = new ArrayList<Param>();
      expect(TokKind.LPAREN, "'('");
      if (!check(TokKind.RPAREN)) {
        do {
          comps.add(parseNamedType("variant field"));
        } while (match(TokKind.COMMA));
      }
      expect(TokKind.RPAREN, "')'");
      variants.add(new RecordDecl(vname, comps));
      skipSemis();
    }
    expect(TokKind.RBRACE, "'}'");
    sealeds.add(new SealedDecl(name, variants));
  }

  /** `enum Color { RED, GREEN }` -> values lower to atoms; `Color.RED` reads one. */
  private void parseEnum() {
    expect(TokKind.IDENT, "'enum'"); // 'enum'
    String name = expect(TokKind.IDENT, "enum name").text();
    expect(TokKind.LBRACE, "'{'");
    skipSemis();
    var values = new ArrayList<String>();
    values.add(expect(TokKind.IDENT, "enum value").text());
    while (match(TokKind.COMMA)) {
      skipSemis();
      values.add(expect(TokKind.IDENT, "enum value").text());
    }
    skipSemis();
    expect(TokKind.RBRACE, "'}'");
    enums.add(new EnumDecl(name, values));
  }

  /** `interface Name { def m(self, ...) -> T ... }` or `Ret m(...)` — signatures only. */
  private void parseInterface() {
    expect(TokKind.IDENT, "'interface'"); // 'interface'
    String name = expect(TokKind.IDENT, "interface name").text();
    expect(TokKind.LBRACE, "'{'");
    skipSemis();
    var sigs = new ArrayList<MethodDecl>();
    while (!check(TokKind.RBRACE) && !check(TokKind.EOF)) {
      String ret = "void";
      String mName;
      if (matchIdent("def")) {
        mName = expect(TokKind.IDENT, "method name").text();
      } else {
        ret = parseType();
        if (ret.equals("None")) ret = "void";
        mName = expect(TokKind.IDENT, "method name").text();
      }
      List<Param> ps = stripSelf(parseParams());
      if (ret.equals("void") && match(TokKind.ARROW)) {
        ret = parseType();
        if (ret.equals("None")) ret = "void";
      }
      sigs.add(new MethodDecl(ret, mName, ps, null, Set.of("public")));
      skipSemis();
    }
    expect(TokKind.RBRACE, "'}'");
    interfaces.add(new InterfaceDecl(name, sigs));
  }

  /** `import util.mathutil`  or  `from util import mathutil` -> Import([util, mathutil]):
   *  a sibling module is referenced by its lowercase file name (Pythonic), same as the
   *  Erlang module it lowers to. FFI imports (`from erlang import gen_tcp`) are the same shape. */
  private void parseImports(List<Import> imports) {
    if (match(TokKind.KW_IMPORT)) {
      var path = new ArrayList<String>();
      path.add(expect(TokKind.IDENT, "module name").text());
      while (match(TokKind.DOT)) {
        path.add(expect(TokKind.IDENT, "module name").text());
      }
      imports.add(new Import(classify(path)));
      return;
    }
    expect(TokKind.IDENT, "'from'"); // 'from'
    var base = new ArrayList<String>();
    base.add(expect(TokKind.IDENT, "module name").text());
    while (match(TokKind.DOT)) {
      base.add(expect(TokKind.IDENT, "module name").text());
    }
    expect(TokKind.KW_IMPORT, "'import'");
    do {
      var path = new ArrayList<>(base);
      path.add(expect(TokKind.IDENT, "imported name").text());
      imports.add(new Import(classify(path)));
    } while (match(TokKind.COMMA));
  }

  /** Module names stay as written (lowercase file name) — both for sibling modules and
   *  erlang.* FFI imports; the leaf is the Erlang module it lowers to. */
  private List<String> classify(List<String> path) {
    return path;
  }

  /** `class NAME (BASE) { ... }` or `class NAME : BASE { ... }`. */
  private void parseClass(List<ActorDecl> actors) {
    expect(TokKind.IDENT, "'class'"); // 'class'
    String name = expect(TokKind.IDENT, "class name").text();
    String base = null;
    if (match(TokKind.COLON)) {
      base = expect(TokKind.IDENT, "base class").text();
    } else if (match(TokKind.LPAREN)) {
      if (!check(TokKind.RPAREN)) {
        base = expect(TokKind.IDENT, "base class").text();
        while (match(TokKind.COMMA)) {
          expect(TokKind.IDENT, "base class");
        }
      }
      expect(TokKind.RPAREN, "')'");
    }
    if ("Exception".equals(base) || "RuntimeException".equals(base)) {
      parseExceptionClass(name);
    } else if ("Actor".equals(base)) {
      Members m = parseMembers(name);
      actors.add(new ActorDecl(name, m.fields(), m.ctor(), m.methods()));
    } else if ("Application".equals(base)) {
      if (application != null) {
        throw new CompileError("more than one Application");
      }
      application = parseApplicationBody(name);
    } else {
      // no base, or an interface name -> an instance class (module + map value).
      Members m = parseMembers(name);
      instanceClasses.add(new InstanceClassDecl(name, base, m.fields(), m.ctor(), m.methods()));
    }
  }

  /** `class NotFound(Exception) {}` — a message-carrying exception. v1: empty body; the
   *  `NotFound(String message) { super(message); }` ctor is synthesized. */
  private void parseExceptionClass(String name) {
    expect(TokKind.LBRACE, "'{'");
    skipSemis();
    if (!check(TokKind.RBRACE)) {
      throw new CompileError("user exceptions are message-only (v1): write `class " + name
          + "(Exception) {}` and raise it with a message");
    }
    expect(TokKind.RBRACE, "'}'");
    var ctor = new MethodDecl("", name, List.of(new Param("String", "message")),
        new Block(List.of(new ExprStmt(new Call("super", List.of(new VarRef("message")))))),
        Set.of("public"));
    exceptions.add(new ExceptionDecl(name, List.of(), ctor));
  }

  /** `class Main(Application) { child = Actor()...  def main(self) {..} }` — fields are the
   *  root supervisor's permanent children; main is the entry. */
  private ApplicationDecl parseApplicationBody(String name) {
    expect(TokKind.LBRACE, "'{'");
    skipSemis();
    var fields = new ArrayList<FieldDecl>();
    MethodDecl main = null;
    while (!check(TokKind.RBRACE) && !check(TokKind.EOF)) {
      skipPub();
      if (checkIdent("def") || looksLikeZincDef() || looksLikeTupleZincDef()) {
        RawDef d = parseDefRaw();
        if (!d.name().equals("main")) {
          throw new CompileError("Application " + name + " can only declare main(): it is the "
              + "boundary, not a unit — methods live on Actors");
        }
        main = new MethodDecl("void", "main", stripSelf(d.params()), d.body(), Set.of("public"));
      } else if (checkIdent("var") && toks.get(pos + 1).kind() == TokKind.IDENT) {
        advance();
        String fname = expect(TokKind.IDENT, "child field name").text();
        Expr init = match(TokKind.ASSIGN) ? parseExpr() : null;
        fields.add(new FieldDecl(init == null ? "var" : literalType(init), fname, init));
      } else if (looksLikeTypeFirstField()) {
        String ftype = parseType();
        String fname = expect(TokKind.IDENT, "child field name").text();
        Expr init = match(TokKind.ASSIGN) ? parseExpr() : null;
        fields.add(new FieldDecl(ftype, fname, init));
      } else {
        String fname = expect(TokKind.IDENT, "child field name").text();
        String ftype = "var";
        if (match(TokKind.COLON)) {
          ftype = parseType();
        }
        Expr init = match(TokKind.ASSIGN) ? parseExpr() : null;
        if (ftype.equals("var") && init != null) {
          ftype = literalType(init);
        }
        fields.add(new FieldDecl(ftype, fname, init));
      }
      skipSemis();
    }
    expect(TokKind.RBRACE, "'}'");
    return new ApplicationDecl(name, fields, main);
  }

  private record Members(List<FieldDecl> fields, MethodDecl ctor, List<MethodDecl> methods) {}

  /** Class body: fields (`name [: T] [= init]`) and methods (`def m(self, ...) [-> T]`).
   *  `def init` becomes the constructor; `self.field` reads/writes are bare field refs.
   *  Shared by Actor and instance-class declarations. */
  private Members parseMembers(String name) {
    expect(TokKind.LBRACE, "'{'");
    skipSemis();
    var fields = new ArrayList<FieldDecl>();
    var methods = new ArrayList<MethodDecl>();
    MethodDecl ctor = null;
    while (!check(TokKind.RBRACE) && !check(TokKind.EOF)) {
      skipPub();
      if (checkIdent("def") || looksLikeZincDef() || looksLikeTupleZincDef()) {
        RawDef d = parseDefRaw();
        List<Param> ps = stripSelf(d.params());
        if (d.name().equals("init")) {
          ctor = new MethodDecl("", name, ps, d.body(), Set.of("public"));
        } else {
          methods.add(new MethodDecl(d.ret(), d.name(), ps, d.body(), Set.of("public")));
        }
      } else if (checkIdent("var") && toks.get(pos + 1).kind() == TokKind.IDENT) {
        advance();
        String fname = expect(TokKind.IDENT, "field name").text();
        Expr init = match(TokKind.ASSIGN) ? parseExpr() : null;
        fields.add(new FieldDecl(init == null ? "var" : literalType(init), fname, init));
      } else if (looksLikeTypeFirstField()) {
        String ftype = parseType();
        String fname = expect(TokKind.IDENT, "field name").text();
        Expr init = match(TokKind.ASSIGN) ? parseExpr() : null;
        fields.add(new FieldDecl(ftype, fname, init));
      } else {
        String fname = expect(TokKind.IDENT, "field name").text();
        String ftype = "var";
        if (match(TokKind.COLON)) {
          ftype = parseType();
        }
        Expr init = match(TokKind.ASSIGN) ? parseExpr() : null;
        if (ftype.equals("var") && init != null) {
          ftype = literalType(init);
        }
        fields.add(new FieldDecl(ftype, fname, init));
      }
      skipSemis();
    }
    expect(TokKind.RBRACE, "'}'");
    return new Members(fields, ctor, methods);
  }

  private boolean looksLikeTypeFirstField() {
    return check(TokKind.IDENT)
        && (toks.get(pos + 1).kind() == TokKind.IDENT
            || toks.get(pos + 1).kind() == TokKind.QUESTION
            || toks.get(pos + 1).kind() == TokKind.LT
            || toks.get(pos + 1).kind() == TokKind.LBRACKET);
  }

  private List<Param> stripSelf(List<Param> params) {
    if (!params.isEmpty() && params.get(0).name().equals("self")) {
      return params.subList(1, params.size());
    }
    return params;
  }

  /** Concrete type of a literal initializer; `var` if it can't be read off directly. */
  private String literalType(Expr e) {
    return switch (e) {
      case IntLit x -> "int";
      case FloatLit x -> "double";
      case BoolLit x -> "boolean";
      case NullLit x -> "var";
      case StrLit x -> "String";
      case NewExpr x -> x.typeName();
      case MapLit x -> mapLitType(x);
      case ListLit x -> listLitType(x);
      case TupleLit x -> tupleLitType(x);
      default -> "var";
    };
  }

  private String tupleLitType(TupleLit x) {
    var types = new ArrayList<String>();
    for (Expr e : x.elems()) {
      String t = literalType(e);
      if (t.equals("var")) return "var";
      types.add(t);
    }
    return "(" + String.join(",", types) + ")";
  }

  /** List literal type: List&lt;T&gt; when elements are homogeneous literals; bare List otherwise. */
  private String listLitType(ListLit x) {
    if (x.explicitType() != null) return x.explicitType();
    if (x.elems().isEmpty()) return "List";
    String e = literalType(x.elems().get(0));
    if (e.equals("var")) return "List";
    for (int i = 1; i < x.elems().size(); i++) {
      if (!e.equals(literalType(x.elems().get(i)))) return "List";
    }
    return "List<" + e + ">";
  }

  /** Dict literal type: HashMap&lt;K,V&gt; when keys and values are each homogeneous literals;
   *  bare HashMap otherwise (heterogeneous -> dynamic values). Mirrors CodeGen.mapLitType. */
  private String mapLitType(MapLit x) {
    if (x.explicitType() != null) return x.explicitType();
    if (x.keys().isEmpty()) return "HashMap";
    String k = literalType(x.keys().get(0)), v = literalType(x.values().get(0));
    if (k.equals("var") || v.equals("var")) return "HashMap";
    for (int i = 1; i < x.keys().size(); i++) {
      if (!k.equals(literalType(x.keys().get(i)))
          || !v.equals(literalType(x.values().get(i)))) {
        return "HashMap";
      }
    }
    return "HashMap<" + k + "," + v + ">";
  }

  private record RawDef(String name, List<String> typeParams, List<Param> params, String ret,
      Block body) {}

  /** `def NAME(params) [-> TYPE] { block }` or `TYPE NAME(params) { block }`. */
  private RawDef parseDefRaw() {
    String ret = "infer"; // sentinel: PyInfer resolves from the body (void if no value-return)
    String name;
    if (matchIdent("def")) {
      name = expect(TokKind.IDENT, "function name").text();
    } else {
      ret = parseType();
      if (ret.equals("None")) ret = "void";
      name = expect(TokKind.IDENT, "function name").text();
    }
    List<String> typeParams = parseTypeParamNames();
    List<Param> params = parseParams();
    if (ret.equals("infer") && match(TokKind.ARROW)) {
      ret = parseType();
      if (ret.equals("None")) ret = "void"; // `-> None` is an explicit cast/void
    }
    declared = new HashSet<>();
    for (Param p : params) {
      declared.add(p.name());
    }
    declared.add("self");
    Block body;
    if (match(TokKind.ASSIGN)) {
      body = new Block(List.of(new ReturnStmt(parseExpr())));
    } else {
      body = parseBlock();
    }
    return new RawDef(name, typeParams, params, ret, body);
  }

  /** Top-level def -> public static method of class Main; `main` gets synthetic args. */
  private MethodDecl parseDef(boolean topLevel) {
    RawDef d = parseDefRaw();
    List<Param> params = d.params();
    if (topLevel && d.name().equals("main") && params.isEmpty()) {
      params = List.of(new Param("String[]", "args")); // -> main/1, the hardwired entry
    }
    var mods = topLevel ? Set.of("public", "static") : Set.of("public");
    return new MethodDecl(d.ret(), d.name(), d.typeParams(), params, d.body(), mods);
  }

  /** `( [NAME : TYPE {, NAME : TYPE}] )`. Params must be typed (the language is statically
   *  typed: a value is typed by annotation or by first assignment, and a param has no first
   *  assignment to infer from) — except the receiver `self`, which is stripped. */
  private List<Param> parseParams() {
    expect(TokKind.LPAREN, "'('");
    var params = new ArrayList<Param>();
    if (!check(TokKind.RPAREN)) {
      do {
        String pname;
        String ptype;
        if (toks.get(pos + 1).kind() == TokKind.COLON) {
          pname = expect(TokKind.IDENT, "parameter name").text();
          advance(); // ':'
          ptype = parseType();
        } else if (checkIdent("self")) {
          pname = advance().text();
          ptype = "var"; // the receiver; removed by stripSelf
        } else if (toks.get(pos + 1).kind() != TokKind.COMMA
            && toks.get(pos + 1).kind() != TokKind.RPAREN) {
          ptype = parseType();
          pname = expect(TokKind.IDENT, "parameter name").text();
        } else {
          pname = expect(TokKind.IDENT, "parameter name").text();
          throw new CompileError("parameter '" + pname + "' needs a type — write '" + pname
              + ": T' or 'T " + pname
              + "' (params are typed; only locals infer from assignment) at line "
              + cur().line());
        }
        params.add(new Param(ptype, pname));
      } while (match(TokKind.COMMA));
    }
    expect(TokKind.RPAREN, "')'");
    return params;
  }

  /** A type name with its generic args and trailing []. Generics are KEPT (e.g.
   *  Channel<String>, List<int>, Map<String,int>) -- CodeGen reads them via typeArgs to
   *  type take()/get()/[] and to drive guarded crossings, matching the literal inference
   *  that already produces HashMap<K,V>/List<T>. */
  private String parseType() {
    if (match(TokKind.LPAREN)) {
      var elems = new ArrayList<String>();
      elems.add(parseType());
      expect(TokKind.COMMA, "',' (tuple types need at least two elements)");
      elems.add(parseType());
      while (match(TokKind.COMMA)) elems.add(parseType());
      expect(TokKind.RPAREN, "')'");
      return "(" + String.join(",", elems) + ")";
    }
    StringBuilder t = new StringBuilder(normalizeTypeName(expect(TokKind.IDENT, "type name").text()));
    if (check(TokKind.LT)) {
      t.append('<');
      advance();
      int depth = 1;
      while (depth > 0) {
        switch (cur().kind()) {
          case LT -> { depth++; t.append('<'); }
          case GT -> { depth--; t.append('>'); }
          case COMMA -> t.append(',');
          case IDENT -> t.append(normalizeTypeName(cur().text()));
          default -> t.append(cur().text());
        }
        advance();
      }
    }
    while (check(TokKind.LBRACKET) && toks.get(pos + 1).kind() == TokKind.RBRACKET) {
      advance();
      advance();
      t.append("[]");
    }
    if (match(TokKind.QUESTION)) {
      // Nullable is accepted as metadata-only syntax for now; strict analysis is deferred.
    }
    return t.toString();
  }

  private String parseGenericHeadType() {
    StringBuilder t = new StringBuilder(normalizeTypeName(expect(TokKind.IDENT, "type name").text()));
    expect(TokKind.LT, "'<'");
    t.append('<');
    int depth = 1;
    while (depth > 0) {
      switch (cur().kind()) {
        case LT -> { depth++; t.append('<'); }
        case GT -> { depth--; t.append('>'); }
        case COMMA -> t.append(',');
        case IDENT -> t.append(normalizeTypeName(cur().text()));
        default -> t.append(cur().text());
      }
      advance();
    }
    return t.toString();
  }

  private String normalizeTypeName(String name) {
    return switch (name) {
      case "bool" -> "boolean";
      case "long", "Int", "Long" -> "int";
      case "float", "Float", "Double" -> "double";
      default -> name;
    };
  }

  // ---- statements ----

  private Block parseBlock() {
    expect(TokKind.LBRACE, "'{'");
    skipSemis();
    var stmts = new ArrayList<Stmt>();
    while (!check(TokKind.RBRACE) && !check(TokKind.EOF)) {
      stmts.add(parseStatement());
      skipSemis();
    }
    expect(TokKind.RBRACE, "'}'");
    return new Block(stmts);
  }

  /** Stamps each statement with its source line so type/runtime errors cite file:line. */
  private Stmt parseStatement() {
    int ln = cur().line();
    return withLine(parseStatement0(), ln);
  }

  private static Stmt withLine(Stmt s, int ln) {
    return switch (s) {
      case VarStmt x -> new VarStmt(x.type(), x.name(), x.init(), x.isFinal(), ln);
      case DestructureStmt x -> new DestructureStmt(x.types(), x.names(), x.init(), ln);
      case AssignStmt x -> new AssignStmt(x.name(), x.op(), x.value(), ln);
      case FieldAssignStmt x -> new FieldAssignStmt(x.objVar(), x.field(), x.op(), x.value(), ln);
      case IndexAssignStmt x -> new IndexAssignStmt(x.arrVar(), x.index(), x.op(), x.value(), ln);
      case SwitchStmt x -> new SwitchStmt(x.subject(), x.cases(), x.defaultBlock(), ln);
      case IfStmt x -> new IfStmt(x.cond(), x.thenBlock(), x.elseBlock(), ln);
      case ForEachStmt x -> new ForEachStmt(x.varType(), x.varName(), x.iterable(), x.body(), ln);
      case WhileStmt x -> new WhileStmt(x.cond(), x.body(), ln);
      case ReturnStmt x -> new ReturnStmt(x.value(), ln);
      case ExprStmt x -> new ExprStmt(x.expr(), ln);
      case Ast.ThrowStmt x -> new Ast.ThrowStmt(x.exType(), x.args(), ln);
      case TryStmt x -> new TryStmt(x.resources(), x.tryBlock(), x.clauses(), ln);
      case SeqStmt x -> new SeqStmt(x.stmts().stream()
          .map(st -> st.line() == 0 ? withLine(st, ln) : st).toList());
      case BreakStmt x -> x;
      case ContinueStmt x -> x;
    };
  }

  private Stmt parseStatement0() {
    if (check(TokKind.KW_RETURN)) return parseReturn();
    if (check(TokKind.KW_IF)) return parseIf();
    if (check(TokKind.KW_WHILE)) return parseWhile();
    if (check(TokKind.KW_FOR)) return parseFor();
    if (checkIdent("try")) return parseTry();
    if (checkIdent("with")) return parseWith();
    if (checkIdent("throw") || checkIdent("raise")) return parseThrow();
    if (checkIdent("assert")) return parseAssert();
    if (checkIdent("match") && matchAhead()) return parseMatch();
    if (checkIdent("var") && toks.get(pos + 1).kind() == TokKind.LPAREN) {
      advance();
      var names = parseDestructureNames();
      expect(TokKind.ASSIGN, "'=' (destructuring needs an initializer)");
      Expr init = parseExpr();
      declared.addAll(names);
      return new DestructureStmt(names, init);
    }
    if (looksLikeTypedDestructure()) {
      var binding = parseTypedDestructure();
      expect(TokKind.ASSIGN, "'=' (destructuring needs an initializer)");
      Expr init = parseExpr();
      declared.addAll(binding.names());
      return new DestructureStmt(binding.types(), binding.names(), init);
    }
    if (checkIdent("var") && toks.get(pos + 1).kind() == TokKind.IDENT) {
      advance();
      String name = expect(TokKind.IDENT, "local name").text();
      expect(TokKind.ASSIGN, "'=' (var locals need an initializer)");
      Expr init = parseExpr();
      declared.add(name);
      return new VarStmt("var", name, init);
    }
    // Zinc-style typed local declaration: `Type name = expr`
    if (looksLikeTypeFirstLocal()) {
      String type = parseType();
      String name = expect(TokKind.IDENT, "local name").text();
      expect(TokKind.ASSIGN, "'=' (a typed local needs an initializer)");
      Expr init = parseExpr();
      declared.add(name);
      return new VarStmt(type, name, init);
    }
    // typed local declaration: `name: Type = expr`
    if (check(TokKind.IDENT) && toks.get(pos + 1).kind() == TokKind.COLON) {
      String name = advance().text();
      advance(); // ':'
      String type = parseType();
      expect(TokKind.ASSIGN, "'=' (an annotated local needs an initializer)");
      Expr init = parseExpr();
      declared.add(name);
      return new VarStmt(type, name, init);
    }
    if (check(TokKind.KW_BREAK)) {
      advance();
      return new BreakStmt();
    }
    if (check(TokKind.KW_CONTINUE)) {
      advance();
      return new ContinueStmt();
    }
    return parseSimpleStmt();
  }

  private List<String> parseDestructureNames() {
    expect(TokKind.LPAREN, "'('");
    var names = new ArrayList<String>();
    names.add(expect(TokKind.IDENT, "destructure variable").text());
    expect(TokKind.COMMA, "',' (destructuring needs at least two variables)");
    names.add(expect(TokKind.IDENT, "destructure variable").text());
    while (match(TokKind.COMMA)) {
      names.add(expect(TokKind.IDENT, "destructure variable").text());
    }
    expect(TokKind.RPAREN, "')'");
    return names;
  }

  private record DestructureBinding(List<String> types, List<String> names) {}

  private DestructureBinding parseTypedDestructure() {
    expect(TokKind.LPAREN, "'('");
    var types = new ArrayList<String>();
    var names = new ArrayList<String>();
    do {
      types.add(parseType());
      names.add(expect(TokKind.IDENT, "destructure variable").text());
    } while (match(TokKind.COMMA));
    expect(TokKind.RPAREN, "')'");
    if (names.size() < 2) {
      throw new CompileError("typed destructuring needs at least two variables");
    }
    return new DestructureBinding(types, names);
  }

  private boolean looksLikeTypedDestructure() {
    if (!check(TokKind.LPAREN)) return false;
    int i = pos + 1;
    if (toks.get(i).kind() != TokKind.IDENT) return false;
    i = skipTypeAhead(i);
    if (toks.get(i).kind() != TokKind.IDENT) return false;
    i++;
    if (toks.get(i).kind() != TokKind.COMMA) return false;
    while (toks.get(i).kind() == TokKind.COMMA) {
      i++;
      if (toks.get(i).kind() != TokKind.IDENT) return false;
      i = skipTypeAhead(i);
      if (toks.get(i).kind() != TokKind.IDENT) return false;
      i++;
    }
    return toks.get(i).kind() == TokKind.RPAREN
        && toks.get(i + 1).kind() == TokKind.ASSIGN;
  }

  private int skipTypeAhead(int i) {
    i++;
    if (toks.get(i).kind() == TokKind.LT) {
      int depth = 1;
      i++;
      while (depth > 0 && toks.get(i).kind() != TokKind.EOF) {
        if (toks.get(i).kind() == TokKind.LT) depth++;
        else if (toks.get(i).kind() == TokKind.GT) depth--;
        i++;
      }
    }
    while (toks.get(i).kind() == TokKind.LBRACKET
        && toks.get(i + 1).kind() == TokKind.RBRACKET) {
      i += 2;
    }
    if (toks.get(i).kind() == TokKind.QUESTION) i++;
    return i;
  }

  private boolean looksLikeTypeFirstLocal() {
    if (!check(TokKind.IDENT)) return false;
    int i = pos + 1;
    if (toks.get(i).kind() == TokKind.LBRACKET && toks.get(i + 1).kind() == TokKind.RBRACKET) {
      i += 2;
    } else if (toks.get(i).kind() == TokKind.LT) {
      int depth = 1;
      i++;
      while (depth > 0 && toks.get(i).kind() != TokKind.EOF) {
        if (toks.get(i).kind() == TokKind.LT) depth++;
        else if (toks.get(i).kind() == TokKind.GT) depth--;
        i++;
      }
    }
    if (toks.get(i).kind() == TokKind.QUESTION) i++;
    return toks.get(i).kind() == TokKind.IDENT
        && toks.get(i + 1).kind() == TokKind.ASSIGN;
  }

  private Stmt parseReturn() {
    expect(TokKind.KW_RETURN, "'return'");
    if (check(TokKind.SEMI) || check(TokKind.RBRACE)) return new ReturnStmt(null);
    return new ReturnStmt(parseExpr());
  }

  private Stmt parseAssert() {
    expect(TokKind.IDENT, "'assert'");
    Expr cond = parseExpr();
    if (match(TokKind.COMMA)) {
      parseExpr(); // message accepted for Zinc syntax; backend reports expression source.
    }
    return new ExprStmt(new MethodCall(new VarRef("Assert"), "isTrue", List.of(cond)));
  }

  private Stmt parseIf() {
    expect(TokKind.KW_IF, "'if'");
    Expr cond = parseExpr();
    Block thenBlock = parseBlock();
    Block elseBlock = null;
    if (match(TokKind.KW_ELSE)) {
      elseBlock = check(TokKind.KW_IF) ? new Block(List.of(parseIf())) : parseBlock();
    }
    return new IfStmt(cond, thenBlock, elseBlock);
  }

  private Stmt parseWhile() {
    expect(TokKind.KW_WHILE, "'while'");
    Expr cond = parseExpr();
    return new WhileStmt(cond, parseBlock());
  }

  /** `for NAME in EXPR { body }`. range(a,b)/range(b) desugar to a counting while. */
  private Stmt parseFor() {
    expect(TokKind.KW_FOR, "'for'");
    boolean parened = match(TokKind.LPAREN);
    String name = expect(TokKind.IDENT, "loop variable").text();
    if (!checkIdent("in")) {
      throw new CompileError("Parse error: expected 'in' in for-loop at line " + cur().line());
    }
    advance(); // 'in'
    declared.add(name);
    Expr iter = parseExpr();
    boolean rangeSyntax = false;
    boolean inclusive = false;
    Expr lo = null;
    Expr hi = null;
    if (check(TokKind.DOTDOT) || check(TokKind.DOTDOTEQ)) {
      rangeSyntax = true;
      inclusive = match(TokKind.DOTDOTEQ);
      if (!inclusive) expect(TokKind.DOTDOT, "'..'");
      lo = iter;
      hi = parseExpr();
    }
    if (parened) expect(TokKind.RPAREN, "')'");
    Block body = parseBlock();
    boolean rangeCall = iter instanceof Call rc && rc.callee().equals("range");
    if (rangeSyntax || rangeCall) {
      if (!rangeSyntax) {
        var c = (Call) iter;
        lo = c.args().size() == 1 ? new IntLit(0) : c.args().get(0);
        hi = c.args().size() == 1 ? c.args().get(0) : c.args().get(1);
      } else if (inclusive) {
        hi = new Binary("+", hi, new IntLit(1));
      }
      Stmt init = new VarStmt("var", name, lo);
      Stmt update = new AssignStmt(name, "+=", new IntLit(1));
      var stmts = new ArrayList<>(rewriteContinue(body, update).stmts());
      stmts.add(update);
      return new SeqStmt(List.of(init,
          new WhileStmt(new Binary("<", new VarRef(name), hi), new Block(stmts))));
    }
    return new ForEachStmt("var", name, iter, body);
  }

  /** `try { } catch TYPE [VAR] { } ...` — clauses match in order; transactional.
   *  Legacy `except TYPE [as VAR]` remains accepted for compatibility. */
  private Stmt parseTry() {
    expect(TokKind.IDENT, "'try'"); // 'try'
    Block tryBlock = parseBlock();
    var clauses = new ArrayList<Ast.CatchClause>();
    while (checkIdent("catch") || checkIdent("except")) {
      boolean legacyExcept = checkIdent("except");
      advance(); // 'catch' / 'except'
      String exType = expect(TokKind.IDENT, "exception type").text();
      String var = "_e";
      if (legacyExcept && checkIdent("as")) {
        advance();
        var = expect(TokKind.IDENT, "exception variable").text();
      } else if (check(TokKind.IDENT) && !checkIdent("catch") && !checkIdent("except")) {
        var = advance().text();
      }
      clauses.add(new Ast.CatchClause(exType, var, parseBlock()));
    }
    if (clauses.isEmpty()) {
      throw new CompileError("Parse error: try needs at least one `catch` at line "
          + cur().line());
    }
    return new TryStmt(List.of(), tryBlock, clauses);
  }

  /** `match` is a soft keyword: only a statement head when not used as a variable/call. */
  private boolean matchAhead() {
    return switch (toks.get(pos + 1).kind()) {
      case ASSIGN, COLON, PLUS_EQ, MINUS_EQ, STAR_EQ, DOT, SEMI -> false;
      default -> true;
    };
  }

  /** `match SUBJECT { case L[, L] { } ... case _ { } }` -> arrow-switch; `case _` = default. */
  private Stmt parseMatch() {
    expect(TokKind.IDENT, "'match'"); // 'match'
    Expr subject = parseExpr();
    expect(TokKind.LBRACE, "'{'");
    skipSemis();
    var cases = new ArrayList<SwitchCase>();
    Block defaultBlock = null;
    while (!check(TokKind.RBRACE) && !check(TokKind.EOF)) {
      if (!checkIdent("case")) {
        throw new CompileError("Parse error: expected `case` at line " + cur().line());
      }
      advance(); // 'case'
      if (check(TokKind.IDENT) && cur().text().equals("_")) {
        advance();
        if (defaultBlock != null) throw new CompileError("duplicate `case _`");
        defaultBlock = parseBlock();
      } else {
        var labels = new ArrayList<Expr>();
        labels.add(parseOr()); // not parseExpr: labels are constants, not lambdas
        while (match(TokKind.COMMA)) {
          labels.add(parseOr());
        }
        cases.add(new SwitchCase(labels, parseBlock()));
      }
      skipSemis();
    }
    expect(TokKind.RBRACE, "'}'");
    return new SwitchStmt(subject, cases, defaultBlock);
  }

  /** `with Files.openReader(p) as r { body }` -> scoped resource (auto-closed at block
   *  exit), i.e. a try-with-resources. The handle type is read off the opener method. */
  private Stmt parseWith() {
    expect(TokKind.IDENT, "'with'"); // 'with'
    Expr init = parseExpr();
    if (!checkIdent("as")) {
      throw new CompileError("Parse error: `with` needs `as <name>` at line " + cur().line());
    }
    advance(); // 'as'
    String var = expect(TokKind.IDENT, "resource variable").text();
    declared.add(var);
    String type = resourceType(init);
    Block body = parseBlock();
    return new TryStmt(List.of(new Ast.Resource(type, var, init)), body, List.of());
  }

  private String resourceType(Expr init) {
    if (init instanceof MethodCall mc) {
      switch (mc.method()) {
        case "openReader": return "Reader";
        case "openWriter":
        case "openAppender": return "Writer";
        default: break;
      }
    }
    throw new CompileError("`with` resource must be Files.openReader / openWriter / "
        + "openAppender (v1) at line " + cur().line());
  }

  /** `throw SomeError("msg")` -> throw. Legacy `raise SomeError("msg")` is accepted. */
  private Stmt parseThrow() {
    String kw = expect(TokKind.IDENT, "'throw'").text(); // 'throw' / 'raise'
    Expr e = parseExpr();
    if (e instanceof NewExpr nx) {
      return new Ast.ThrowStmt(nx.typeName(), nx.args());
    }
    throw new CompileError("Parse error: `" + kw + "` expects `" + kw
        + " SomeError(...)` at line "
        + cur().line());
  }

  /** Replace `continue` belonging to THIS loop with `update; continue` (descends ifs). */
  private Block rewriteContinue(Block b, Stmt update) {
    var out = new ArrayList<Stmt>();
    for (Stmt s : b.stmts()) {
      if (s instanceof ContinueStmt) {
        out.add(update);
        out.add(s);
      } else if (s instanceof IfStmt it) {
        out.add(new IfStmt(it.cond(), rewriteContinue(it.thenBlock(), update),
            it.elseBlock() == null ? null : rewriteContinue(it.elseBlock(), update)));
      } else {
        out.add(s);
      }
    }
    return new Block(out);
  }

  /** Assignment / declaration / bare expression. */
  private Stmt parseSimpleStmt() {
    Expr e = parseExpr();
    if (isAssignOp(cur().kind())) {
      String op = advance().text();
      Expr rhs = parseExpr();
      // first plain `name = e` in scope is a declaration; later ones are reassignment
      if (op.equals("=") && e instanceof VarRef v && !declared.contains(v.name())) {
        declared.add(v.name());
        return new VarStmt("var", v.name(), rhs);
      }
      return makeAssign(e, op, rhs);
    }
    return new ExprStmt(e);
  }

  private boolean isAssignOp(TokKind k) {
    return k == TokKind.ASSIGN || k == TokKind.PLUS_EQ || k == TokKind.MINUS_EQ
        || k == TokKind.STAR_EQ;
  }

  private Stmt makeAssign(Expr lvalue, String op, Expr rhs) {
    if (lvalue instanceof VarRef v) return new AssignStmt(v.name(), op, rhs);
    if (lvalue instanceof FieldAccess fa && fa.obj() instanceof VarRef v) {
      return new FieldAssignStmt(v.name(), fa.field(), op, rhs);
    }
    if (lvalue instanceof Index ix && ix.obj() instanceof VarRef v) {
      return new IndexAssignStmt(v.name(), ix.index(), op, rhs);
    }
    throw new CompileError("Parse error: invalid assignment target at line " + cur().line());
  }

  // ---- expressions (identical grammar to the legal-Java surface) ----

  Expr parseExpr() {
    if (checkIdent("lambda")) return parsePyLambda();
    if (lambdaAhead()) return parseLambda();
    return parseTernary();
  }

  /** Python lambda: `lambda a, b: expr` (also `lambda: expr`). */
  private Expr parsePyLambda() {
    expect(TokKind.IDENT, "'lambda'"); // 'lambda'
    var params = new ArrayList<String>();
    if (!check(TokKind.COLON)) {
      do {
        params.add(expect(TokKind.IDENT, "lambda parameter").text());
      } while (match(TokKind.COMMA));
    }
    expect(TokKind.COLON, "':' (lambda body)");
    Expr body = parseExpr();
    return new LambdaExpr(params, new Block(List.of(new ReturnStmt(body))));
  }

  private Expr parseTernary() {
    Expr cond = parseOr();
    if (match(TokKind.QUESTION)) {
      Expr then = parseTernary();
      expect(TokKind.COLON, "':' (ternary)");
      Expr els = parseTernary();
      return new Ternary(cond, then, els);
    }
    return cond;
  }

  private boolean lambdaAhead() {
    if (check(TokKind.IDENT) && toks.get(pos + 1).kind() == TokKind.ARROW) return true;
    if (!check(TokKind.LPAREN)) return false;
    int i = pos + 1;
    if (toks.get(i).kind() == TokKind.RPAREN) return toks.get(i + 1).kind() == TokKind.ARROW;
    while (toks.get(i).kind() == TokKind.IDENT) {
      i++;
      if (toks.get(i).kind() == TokKind.COMMA) {
        i++;
        continue;
      }
      break;
    }
    return toks.get(i).kind() == TokKind.RPAREN && toks.get(i + 1).kind() == TokKind.ARROW;
  }

  private Expr parseLambda() {
    var params = new ArrayList<String>();
    if (check(TokKind.IDENT)) {
      params.add(advance().text());
    } else {
      expect(TokKind.LPAREN, "'('");
      if (!check(TokKind.RPAREN)) {
        do {
          params.add(expect(TokKind.IDENT, "lambda parameter").text());
        } while (match(TokKind.COMMA));
      }
      expect(TokKind.RPAREN, "')'");
    }
    expect(TokKind.ARROW, "'->'");
    Block body = check(TokKind.LBRACE) ? parseBlock()
        : new Block(List.of(new ExprStmt(parseExpr())));
    return new LambdaExpr(params, body);
  }

  private Expr parseOr() {
    Expr left = parseAnd();
    while (check(TokKind.PIPE_PIPE)) {
      advance();
      left = new Binary("||", left, parseAnd());
    }
    return left;
  }

  private Expr parseAnd() {
    Expr left = parseBitOr();
    while (check(TokKind.AMP_AMP)) {
      advance();
      left = new Binary("&&", left, parseBitOr());
    }
    return left;
  }

  private Expr parseBitOr() {
    Expr left = parseBitXor();
    while (check(TokKind.PIPE)) {
      advance();
      left = new Binary("|", left, parseBitXor());
    }
    return left;
  }

  private Expr parseBitXor() {
    Expr left = parseBitAnd();
    while (check(TokKind.CARET)) {
      advance();
      left = new Binary("^", left, parseBitAnd());
    }
    return left;
  }

  private Expr parseBitAnd() {
    Expr left = parseEquality();
    while (check(TokKind.AMP)) {
      advance();
      left = new Binary("&", left, parseEquality());
    }
    return left;
  }

  private Expr parseEquality() {
    Expr left = parseRelational();
    while (check(TokKind.EQ) || check(TokKind.NE)) {
      String op = advance().text();
      left = new Binary(op, left, parseRelational());
    }
    return left;
  }

  private Expr parseRelational() {
    Expr left = parseShift();
    while (check(TokKind.LT) || check(TokKind.GT) || check(TokKind.LE) || check(TokKind.GE)) {
      String op = advance().text();
      left = new Binary(op, left, parseShift());
    }
    return left;
  }

  private Expr parseShift() {
    Expr left = parseAdditive();
    while (isShiftAhead()) {
      String op = shiftOp();
      left = new Binary(op, left, parseAdditive());
    }
    return left;
  }

  private boolean isShiftAhead() {
    return (check(TokKind.LT) && toks.get(pos + 1).kind() == TokKind.LT)
        || (check(TokKind.GT) && toks.get(pos + 1).kind() == TokKind.GT);
  }

  private String shiftOp() {
    String op = check(TokKind.LT) ? "<<" : ">>";
    advance();
    advance();
    return op;
  }

  private Expr parseAdditive() {
    Expr left = parseMultiplicative();
    while (check(TokKind.PLUS) || check(TokKind.MINUS)) {
      String op = advance().text();
      left = new Binary(op, left, parseMultiplicative());
    }
    return left;
  }

  private Expr parseMultiplicative() {
    Expr left = parseUnary();
    while (check(TokKind.STAR) || check(TokKind.SLASH) || check(TokKind.PERCENT)) {
      String op = advance().text();
      left = new Binary(op, left, parseUnary());
    }
    return left;
  }

  private Expr parseUnary() {
    if (check(TokKind.MINUS)) {
      advance();
      return new Unary("-", parseUnary());
    }
    if (check(TokKind.BANG)) {
      advance();
      return new Unary("!", parseUnary());
    }
    return parsePostfix();
  }

  private Expr parsePostfix() {
    Expr e = parsePrimary();
    // `self.x` collapses to the actor's bare member: self.field -> field (VarRef),
    // self.method(..) -> this.method(..) -- matching the legal-Java actor surface.
    boolean self = e instanceof VarRef v && v.name().equals("self");
    while (true) {
      if (match(TokKind.DOT)) {
        String name = expect(TokKind.IDENT, "member name").text();
        if (match(TokKind.LPAREN)) {
          var args = parseArgs();
          e = new MethodCall(self ? new VarRef("this") : e, name, args);
        } else {
          e = self ? new VarRef(name) : new FieldAccess(e, name);
        }
        self = false;
      } else if (check(TokKind.QUESTION) && toks.get(pos + 1).kind() == TokKind.DOT) {
        advance(); // '?'
        advance(); // '.'
        String name = expect(TokKind.IDENT, "member name").text();
        if (match(TokKind.LPAREN)) {
          e = new SafeMethodCall(self ? new VarRef("this") : e, name, parseArgs());
        } else {
          e = self ? new VarRef(name) : new SafeFieldAccess(e, name);
        }
        self = false;
      } else if (match(TokKind.LBRACKET)) {
        Expr idx = parseExpr();
        expect(TokKind.RBRACKET, "']'");
        e = new Index(e, idx);
      } else {
        break;
      }
    }
    if (e instanceof VarRef v && v.name().equals("self")) {
      return new VarRef("this"); // bare `self`
    }
    return e;
  }

  /** Implicit f-string: `"a {expr} b"` -> "a " + (expr) + " b". `{{`/`}}` are literal
   *  braces. Each hole is sub-parsed as a braces-Python expression. A string with no
   *  `{expr}` hole is returned verbatim (no brace unescaping). */
  private Expr fstring(String text) {
    int n = text.length();
    var parts = new ArrayList<Expr>();
    var lit = new StringBuilder();
    boolean any = false;
    int i = 0;
    while (i < n) {
      char c = text.charAt(i);
      if (c == '{') {
        if (i + 1 < n && text.charAt(i + 1) == '{') {
          lit.append('{');
          i += 2;
          continue;
        }
        int depth = 1;
        int j = i + 1;
        while (j < n && depth > 0) {
          char d = text.charAt(j);
          if (d == '{') depth++;
          else if (d == '}' && --depth == 0) break;
          j++;
        }
        if (depth != 0) throw new CompileError("unterminated `{` in string: " + text);
        parts.add(new StrLit(lit.toString()));
        lit.setLength(0);
        parts.add(new PyParser(PyLexer.lex(text.substring(i + 1, j))).parseExpr());
        any = true;
        i = j + 1;
      } else if (c == '}' && i + 1 < n && text.charAt(i + 1) == '}') {
        lit.append('}');
        i += 2;
      } else {
        lit.append(c);
        i++;
      }
    }
    if (!any) return new StrLit(text);
    parts.add(new StrLit(lit.toString()));
    Expr acc = parts.get(0); // a StrLit -> the whole fold has String-concat semantics
    for (int k = 1; k < parts.size(); k++) {
      acc = new Binary("+", acc, parts.get(k));
    }
    return acc;
  }

  /** Canonical Zinc interpolation: `"a ${expr} b"`. Plain strings without `${...}` stay
   *  literal, so legacy examples with ordinary braces are unaffected. */
  private Expr zincString(String text) {
    int n = text.length();
    var parts = new ArrayList<Expr>();
    var lit = new StringBuilder();
    boolean any = false;
    int i = 0;
    while (i < n) {
      char c = text.charAt(i);
      if (c == '$' && i + 1 < n && text.charAt(i + 1) == '{') {
        int depth = 1;
        int j = i + 2;
        while (j < n && depth > 0) {
          char d = text.charAt(j);
          if (d == '{') depth++;
          else if (d == '}' && --depth == 0) break;
          j++;
        }
        if (depth != 0) throw new CompileError("unterminated `${` in string: " + text);
        parts.add(new StrLit(lit.toString()));
        lit.setLength(0);
        parts.add(new PyParser(PyLexer.lex(text.substring(i + 2, j))).parseExpr());
        any = true;
        i = j + 1;
      } else {
        lit.append(c);
        i++;
      }
    }
    if (!any) return new StrLit(text);
    parts.add(new StrLit(lit.toString()));
    Expr acc = parts.get(0);
    for (int k = 1; k < parts.size(); k++) {
      acc = new Binary("+", acc, parts.get(k));
    }
    return acc;
  }

  private List<Expr> parseArgs() {
    var args = new ArrayList<Expr>();
    if (!check(TokKind.RPAREN)) {
      do {
        args.add(parseExpr());
      } while (match(TokKind.COMMA));
    }
    expect(TokKind.RPAREN, "')'");
    return args;
  }

  private Expr parsePrimary() {
    if (check(TokKind.INT_LIT)) return new IntLit(parseIntLit(advance().text()));
    if (check(TokKind.FLOAT_LIT)) return new FloatLit(Double.parseDouble(advance().text()));
    if (check(TokKind.KW_TRUE)) {
      advance();
      return new BoolLit(true);
    }
    if (check(TokKind.KW_FALSE)) {
      advance();
      return new BoolLit(false);
    }
    if (checkIdent("null")) {
      advance();
      return new NullLit();
    }
    if (check(TokKind.STR_LIT)) return zincString(advance().text());
    if (check(TokKind.FSTR_LIT)) return fstring(advance().text());    // f"..." interpolates
    if (match(TokKind.LPAREN)) {
      Expr e = parseExpr();
      if (match(TokKind.COMMA)) {
        var elems = new ArrayList<Expr>();
        elems.add(e);
        elems.add(parseExpr());
        while (match(TokKind.COMMA)) elems.add(parseExpr());
        expect(TokKind.RPAREN, "')'");
        return new TupleLit(elems);
      }
      expect(TokKind.RPAREN, "')'");
      return e;
    }
    if (match(TokKind.LBRACKET)) { // Python list literal: [a, b, c]
      var elems = new ArrayList<Expr>();
      if (!check(TokKind.RBRACKET)) {
        do {
          elems.add(parseExpr());
        } while (match(TokKind.COMMA));
      }
      expect(TokKind.RBRACKET, "']'");
      return new ListLit(elems);
    }
    if (match(TokKind.LBRACE)) { // dict literal: {k: v, ...} (only ever in expr position)
      var keys = new ArrayList<Expr>();
      var values = new ArrayList<Expr>();
      skipSemis();
      while (!check(TokKind.RBRACE)) {
        keys.add(parseExpr());
        expect(TokKind.COLON, "':' (dict entries are key: value)");
        values.add(parseExpr());
        skipSemis();
        if (!match(TokKind.COMMA)) break;
        skipSemis();
      }
      expect(TokKind.RBRACE, "'}'");
      return new MapLit(keys, values);
    }
    if (check(TokKind.IDENT)) {
      if (genericExprAhead()) {
        String collectionType = parseGenericHeadType();
        if (baseTypeName(collectionType).equals("List") && match(TokKind.LBRACKET)) {
          var elems = new ArrayList<Expr>();
          if (!check(TokKind.RBRACKET)) {
            do {
              elems.add(parseExpr());
            } while (match(TokKind.COMMA));
          }
          expect(TokKind.RBRACKET, "']'");
          return new ListLit(elems, collectionType);
        }
        if (baseTypeName(collectionType).equals("Map") && match(TokKind.LBRACE)) {
          return parseMapLiteral(collectionType);
        }
        if (match(TokKind.LPAREN)) {
          if (Character.isUpperCase(baseTypeName(collectionType).charAt(0))) {
            return new NewExpr(collectionType, parseArgs());
          }
          return new Call(collectionType, parseArgs());
        }
        throw new CompileError("Parse error: expected typed literal or constructor after "
            + collectionType + " at line " + cur().line());
      }
      String name = advance().text();
      String typeName = normalizeTypeName(name);
      if (isSizedArrayElementType(typeName) && match(TokKind.LBRACKET)) {
        Expr size = parseExpr();
        expect(TokKind.RBRACKET, "']'");
        return new ArrayNewExpr(typeName, size);
      }
      if (match(TokKind.LPAREN)) {
        var args = parseArgs();
        if (name.equals("print")) { // desugar to the node CodeGen already lowers
          return new MethodCall(new FieldAccess(new VarRef("System"), "out"), "println", args);
        }
        // CapWords callee is construction (Python convention); Resolve turns an Actor
        // construction into a spawn. lowercase is a plain function call.
        if (Character.isUpperCase(name.charAt(0))) {
          return new NewExpr(name, args);
        }
        return new Call(name, args);
      }
      return new VarRef(name);
    }
    throw new CompileError("Parse error: unexpected " + cur().kind() + " \"" + cur().text()
        + "\" at line " + cur().line());
  }

  private MapLit parseMapLiteral(String explicitType) {
    var keys = new ArrayList<Expr>();
    var values = new ArrayList<Expr>();
    skipSemis();
    while (!check(TokKind.RBRACE)) {
      keys.add(parseExpr());
      expect(TokKind.COLON, "':' (dict entries are key: value)");
      values.add(parseExpr());
      skipSemis();
      if (!match(TokKind.COMMA)) break;
      skipSemis();
    }
    expect(TokKind.RBRACE, "'}'");
    return new MapLit(keys, values, explicitType);
  }

  private String baseTypeName(String t) {
    int i = t.indexOf('<');
    return i < 0 ? t : t.substring(0, i);
  }

  private boolean genericExprAhead() {
    if (!check(TokKind.IDENT) || toks.get(pos + 1).kind() != TokKind.LT) return false;
    int i = genericHeadEnd(pos);
    return toks.get(i).kind() == TokKind.LPAREN
        || toks.get(i).kind() == TokKind.LBRACKET
        || toks.get(i).kind() == TokKind.LBRACE;
  }

  private int genericHeadEnd(int i) {
    i += 2; // ident '<'
    int depth = 1;
    while (depth > 0 && toks.get(i).kind() != TokKind.EOF) {
      if (toks.get(i).kind() == TokKind.LT) depth++;
      else if (toks.get(i).kind() == TokKind.GT) depth--;
      i++;
    }
    return i;
  }

  private boolean isSizedArrayElementType(String name) {
    return name.equals("int") || name.equals("double") || name.equals("boolean")
        || name.equals("String");
  }

  private long parseIntLit(String text) {
    if (text.startsWith("0x") || text.startsWith("0X")) {
      return Long.parseLong(text.substring(2), 16);
    }
    if (text.startsWith("0b") || text.startsWith("0B")) {
      return Long.parseLong(text.substring(2), 2);
    }
    if (text.startsWith("0o") || text.startsWith("0O")) {
      return Long.parseLong(text.substring(2), 8);
    }
    return Long.parseLong(text);
  }
}
