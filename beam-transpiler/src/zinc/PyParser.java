package zinc;

import java.util.ArrayList;
import java.util.HashSet;
import java.util.List;
import java.util.Set;
import zinc.Ast.*;

/**
 * Parser for the braces-Python surface (.zn). Emits the SAME {@link Ast} the legal-Java
 * {@link Parser} produces, so Resolve + CodeGen run unchanged. The expression grammar is
 * identical (C-style operators); only declarations/statements differ:
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

  // ---- declarations ----

  Program parseProgram() {
    skipSemis();
    var imports = new ArrayList<Import>();
    var topDefs = new ArrayList<MethodDecl>(); // become static methods of class Main
    var actors = new ArrayList<ActorDecl>();
    while (!check(TokKind.EOF)) {
      if (checkIdent("def")) {
        topDefs.add(parseDef(true));
      } else if (checkIdent("class")) {
        parseClass(actors);
      } else if (check(TokKind.KW_IMPORT) || checkIdent("from")) {
        parseImports(imports);
      } else if (checkIdent("interface")) {
        parseInterface();
      } else if (checkIdent("record")) {
        parseRecord();
      } else if (checkIdent("enum")) {
        parseEnum();
      } else {
        throw new CompileError("Parse error: expected `def`, `class`, `interface`, `record`,"
            + " `enum` or `import` at line " + cur().line());
      }
      skipSemis();
    }
    var classes = new ArrayList<ClassDecl>();
    if (application != null) {
      if (!topDefs.isEmpty()) {
        throw new CompileError("an Application program has no top-level `def`s — helpers "
            + "live on Actors (the Application is the boundary)");
      }
    } else if (!topDefs.isEmpty()) {
      // a file with `def main` is the entry (class Main -> module main); otherwise the
      // file's top-level defs live on a class named after the file (Fmt.banner(...)).
      boolean hasMain = topDefs.stream().anyMatch(m -> m.name().equals("main"));
      classes.add(new ClassDecl(hasMain ? "Main" : fileClass, topDefs));
    }
    var program = new Program(imports, classes, records, actors, enums, application,
        exceptions, interfaces, instanceClasses, List.of());
    return PyInfer.infer(program); // resolve `infer` return types from method bodies
  }

  /** `record Point(x: int, y: int)` -> immutable map value; `p.x` reads a component. */
  private void parseRecord() {
    expect(TokKind.IDENT, "'record'"); // 'record'
    String name = expect(TokKind.IDENT, "record name").text();
    var comps = new ArrayList<Param>();
    expect(TokKind.LPAREN, "'('");
    if (!check(TokKind.RPAREN)) {
      do {
        String cname = expect(TokKind.IDENT, "component name").text();
        expect(TokKind.COLON, "':' (record components are typed: name: T)");
        comps.add(new Param(parseType(), cname));
      } while (match(TokKind.COMMA));
    }
    expect(TokKind.RPAREN, "')'");
    records.add(new RecordDecl(name, comps));
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

  /** `interface Name { def m(self, ...) -> T  ... }` — signatures only, no bodies. */
  private void parseInterface() {
    expect(TokKind.IDENT, "'interface'"); // 'interface'
    String name = expect(TokKind.IDENT, "interface name").text();
    expect(TokKind.LBRACE, "'{'");
    skipSemis();
    var sigs = new ArrayList<MethodDecl>();
    while (!check(TokKind.RBRACE) && !check(TokKind.EOF)) {
      expect(TokKind.IDENT, "'def'"); // 'def'
      String mName = expect(TokKind.IDENT, "method name").text();
      List<Param> ps = stripSelf(parseParams());
      String ret = match(TokKind.ARROW) ? parseType() : "void";
      if (ret.equals("None")) ret = "void";
      sigs.add(new MethodDecl(ret, mName, ps, null, Set.of("public")));
      skipSemis();
    }
    expect(TokKind.RBRACE, "'}'");
    interfaces.add(new InterfaceDecl(name, sigs));
  }

  /** `import util.mathutil`  or  `from util import mathutil` -> Import([util, Mathutil]):
   *  a sibling module is a class, so the leaf is capitalized to the class name. FFI imports
   *  (`from erlang import gen_tcp`) keep the lowercase OTP module name. */
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

  /** Capitalize the leaf (= class name) unless this is an erlang.* FFI import. */
  private List<String> classify(List<String> path) {
    if (path.isEmpty() || path.get(0).equals("erlang")) return path;
    int last = path.size() - 1;
    String leaf = path.get(last);
    if (!leaf.isEmpty() && Character.isLowerCase(leaf.charAt(0))) {
      path.set(last, Character.toUpperCase(leaf.charAt(0)) + leaf.substring(1));
    }
    return path;
  }

  /** `class NAME ( BASE ) { ... }`. v1: `(Actor)` or `(Application)`. */
  private void parseClass(List<ActorDecl> actors) {
    expect(TokKind.IDENT, "'class'"); // 'class'
    String name = expect(TokKind.IDENT, "class name").text();
    String base = null;
    if (match(TokKind.LPAREN)) {
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
      if (checkIdent("def")) {
        RawDef d = parseDefRaw();
        if (!d.name().equals("main")) {
          throw new CompileError("Application " + name + " can only declare main(): it is the "
              + "boundary, not a unit — methods live on Actors");
        }
        main = new MethodDecl("void", "main", stripSelf(d.params()), d.body(), Set.of("public"));
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
      if (checkIdent("def")) {
        RawDef d = parseDefRaw();
        List<Param> ps = stripSelf(d.params());
        if (d.name().equals("init")) {
          ctor = new MethodDecl("", name, ps, d.body(), Set.of("public"));
        } else {
          methods.add(new MethodDecl(d.ret(), d.name(), ps, d.body(), Set.of("public")));
        }
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
      case StrLit x -> "String";
      case NewExpr x -> x.typeName();
      default -> "var";
    };
  }

  private record RawDef(String name, List<Param> params, String ret, Block body) {}

  /** `def NAME ( params ) [-> TYPE] { block }`. */
  private RawDef parseDefRaw() {
    expect(TokKind.IDENT, "'def'"); // 'def'
    String name = expect(TokKind.IDENT, "function name").text();
    List<Param> params = parseParams();
    String ret = "infer"; // sentinel: PyInfer resolves from the body (void if no value-return)
    if (match(TokKind.ARROW)) {
      ret = parseType();
      if (ret.equals("None")) ret = "void"; // `-> None` is an explicit cast/void
    }
    declared = new HashSet<>();
    for (Param p : params) {
      declared.add(p.name());
    }
    declared.add("self");
    Block body = parseBlock();
    return new RawDef(name, params, ret, body);
  }

  /** Top-level def -> public static method of class Main; `main` gets synthetic args. */
  private MethodDecl parseDef(boolean topLevel) {
    RawDef d = parseDefRaw();
    List<Param> params = d.params();
    if (topLevel && d.name().equals("main") && params.isEmpty()) {
      params = List.of(new Param("String[]", "args")); // -> main/1, the hardwired entry
    }
    var mods = topLevel ? Set.of("public", "static") : Set.of("public");
    return new MethodDecl(d.ret(), d.name(), params, d.body(), mods);
  }

  /** `( [NAME [: TYPE] {, NAME [: TYPE]}] )`. Untyped params infer as `var`. */
  private List<Param> parseParams() {
    expect(TokKind.LPAREN, "'('");
    var params = new ArrayList<Param>();
    if (!check(TokKind.RPAREN)) {
      do {
        String pname = expect(TokKind.IDENT, "parameter name").text();
        String ptype = "var";
        if (match(TokKind.COLON)) {
          ptype = parseType();
        }
        params.add(new Param(ptype, pname));
      } while (match(TokKind.COMMA));
    }
    expect(TokKind.RPAREN, "')'");
    return params;
  }

  /** A type name, with erased generics and trailing []. */
  private String parseType() {
    StringBuilder t = new StringBuilder(expect(TokKind.IDENT, "type name").text());
    if (check(TokKind.LT)) { // erased generics
      advance();
      int depth = 1;
      while (depth > 0) {
        if (check(TokKind.LT)) depth++;
        else if (check(TokKind.GT)) depth--;
        if (depth > 0) advance();
        else advance();
      }
    }
    while (check(TokKind.LBRACKET) && toks.get(pos + 1).kind() == TokKind.RBRACKET) {
      advance();
      advance();
      t.append("[]");
    }
    return t.toString();
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
    if (checkIdent("raise")) return parseRaise();
    if (checkIdent("match") && matchAhead()) return parseMatch();
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

  private Stmt parseReturn() {
    expect(TokKind.KW_RETURN, "'return'");
    if (check(TokKind.SEMI) || check(TokKind.RBRACE)) return new ReturnStmt(null);
    return new ReturnStmt(parseExpr());
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
    String name = expect(TokKind.IDENT, "loop variable").text();
    if (!checkIdent("in")) {
      throw new CompileError("Parse error: expected 'in' in for-loop at line " + cur().line());
    }
    advance(); // 'in'
    declared.add(name);
    Expr iter = parseExpr();
    Block body = parseBlock();
    if (iter instanceof Call c && c.callee().equals("range")) {
      Expr lo = c.args().size() == 1 ? new IntLit(0) : c.args().get(0);
      Expr hi = c.args().size() == 1 ? c.args().get(0) : c.args().get(1);
      Stmt init = new VarStmt("var", name, lo);
      Stmt update = new AssignStmt(name, "+=", new IntLit(1));
      var stmts = new ArrayList<>(rewriteContinue(body, update).stmts());
      stmts.add(update);
      return new SeqStmt(List.of(init,
          new WhileStmt(new Binary("<", new VarRef(name), hi), new Block(stmts))));
    }
    return new ForEachStmt("var", name, iter, body);
  }

  /** `try { } except TYPE [as VAR] { } ...` — clauses match in order; transactional. */
  private Stmt parseTry() {
    expect(TokKind.IDENT, "'try'"); // 'try'
    Block tryBlock = parseBlock();
    var clauses = new ArrayList<Ast.CatchClause>();
    while (checkIdent("except")) {
      advance(); // 'except'
      String exType = expect(TokKind.IDENT, "exception type").text();
      String var = "_e";
      if (checkIdent("as")) {
        advance();
        var = expect(TokKind.IDENT, "exception variable").text();
      }
      clauses.add(new Ast.CatchClause(exType, var, parseBlock()));
    }
    if (clauses.isEmpty()) {
      throw new CompileError("Parse error: try needs at least one `except` at line "
          + cur().line());
    }
    return new TryStmt(List.of(), tryBlock, clauses);
  }

  /** `match` is a soft keyword: only a statement head when not used as a variable/call. */
  private boolean matchAhead() {
    return switch (toks.get(pos + 1).kind()) {
      case ASSIGN, COLON, PLUS_EQ, MINUS_EQ, STAR_EQ, DOT, LPAREN, SEMI -> false;
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

  /** `raise SomeError("msg")` -> throw. */
  private Stmt parseRaise() {
    expect(TokKind.IDENT, "'raise'"); // 'raise'
    Expr e = parseExpr();
    if (e instanceof NewExpr nx) {
      return new Ast.ThrowStmt(nx.typeName(), nx.args());
    }
    throw new CompileError("Parse error: `raise` expects `raise SomeError(...)` at line "
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
        : new Block(List.of(new ReturnStmt(parseExpr())));
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
    Expr left = parseEquality();
    while (check(TokKind.AMP_AMP)) {
      advance();
      left = new Binary("&&", left, parseEquality());
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
    Expr left = parseAdditive();
    while (check(TokKind.LT) || check(TokKind.GT) || check(TokKind.LE) || check(TokKind.GE)) {
      String op = advance().text();
      left = new Binary(op, left, parseAdditive());
    }
    return left;
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
    if (check(TokKind.INT_LIT)) return new IntLit(Long.parseLong(advance().text()));
    if (check(TokKind.FLOAT_LIT)) return new FloatLit(Double.parseDouble(advance().text()));
    if (check(TokKind.KW_TRUE)) {
      advance();
      return new BoolLit(true);
    }
    if (check(TokKind.KW_FALSE)) {
      advance();
      return new BoolLit(false);
    }
    if (check(TokKind.STR_LIT)) return fstring(advance().text());
    if (match(TokKind.LPAREN)) {
      Expr e = parseExpr();
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
    if (check(TokKind.IDENT)) {
      String name = advance().text();
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
}
