package zinc;

import java.util.ArrayList;
import java.util.List;
import java.util.Set;
import zinc.Ast.*;

class Parser {
  private static final Set<String> MODIFIERS =
      Set.of("public", "private", "protected", "static", "final");

  private final List<Token> toks;
  private int pos = 0;

  Parser(List<Token> toks) {
    this.toks = toks;
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

  // ---- declarations ----

  Program parseProgram() {
    var imports = new ArrayList<Import>();
    while (check(TokKind.KW_IMPORT)) {
      imports.add(parseImport());
    }
    var classes = new ArrayList<ClassDecl>();
    var records = new ArrayList<RecordDecl>();
    var actors = new ArrayList<ActorDecl>();
    var enums = new ArrayList<EnumDecl>();
    var application = new ApplicationDecl[1];
    var exceptions = new ArrayList<ExceptionDecl>();
    var interfaces = new ArrayList<InterfaceDecl>();
    var instanceClasses = new ArrayList<InstanceClassDecl>();
    var tests = new ArrayList<TestDecl>();
    while (!check(TokKind.EOF)) {
      skipModifiers();
      if (checkIdent("class")) {
        parseClassLike(classes, actors, application, exceptions, instanceClasses, tests);
      } else if (checkIdent("interface")) {
        interfaces.add(parseInterface());
      } else if (checkIdent("record")) {
        records.add(parseRecord());
      } else if (checkIdent("enum")) {
        enums.add(parseEnum());
      } else {
        throw new CompileError("Parse error: expected class, interface, record or enum at line "
            + cur().line());
      }
    }
    return new Program(imports, classes, records, actors, enums, application[0], exceptions,
        interfaces, instanceClasses, tests);
  }

  /** interface Name { Ret m(T a); ... } — signatures only (no default methods, v1). */
  private InterfaceDecl parseInterface() {
    advance(); // 'interface'
    String name = expect(TokKind.IDENT, "interface name").text();
    expect(TokKind.LBRACE, "'{'");
    var sigs = new ArrayList<MethodDecl>();
    while (!check(TokKind.RBRACE)) {
      Set<String> mods = skipModifiers();
      if (mods.contains("private") || mods.contains("static") || mods.contains("final")) {
        throw new CompileError("interface " + name
            + ": method signatures are implicitly public (no private/static/final)");
      }
      String ret = parseType();
      String mName = expect(TokKind.IDENT, "method name").text();
      List<Param> params = parseParams();
      expect(TokKind.SEMI, "';' (interface methods have no body — default methods: later)");
      sigs.add(new MethodDecl(ret, mName, params, null));
    }
    expect(TokKind.RBRACE, "'}'");
    return new InterfaceDecl(name, sigs);
  }

  private EnumDecl parseEnum() {
    advance(); // 'enum'
    String name = expect(TokKind.IDENT, "enum name").text();
    expect(TokKind.LBRACE, "'{'");
    var values = new ArrayList<String>();
    values.add(expect(TokKind.IDENT, "enum value").text());
    while (match(TokKind.COMMA)) {
      values.add(expect(TokKind.IDENT, "enum value").text());
    }
    match(TokKind.SEMI);
    expect(TokKind.RBRACE, "'}'");
    return new EnumDecl(name, values);
  }

  private Import parseImport() {
    expect(TokKind.KW_IMPORT, "'import'");
    var path = new ArrayList<String>();
    path.add(expect(TokKind.IDENT, "package or class name").text());
    while (match(TokKind.DOT)) {
      path.add(expect(TokKind.IDENT, "package or class name").text());
    }
    expect(TokKind.SEMI, "';'");
    return new Import(path);
  }

  /** Collects modifiers. Decision #10: accepted syntax is enforced or rejected,
   *  never silently decorative — every consumer validates what it allows. */
  private Set<String> skipModifiers() {
    var seen = new java.util.HashSet<String>();
    while (check(TokKind.IDENT) && MODIFIERS.contains(cur().text())) {
      seen.add(advance().text());
    }
    if (seen.contains("protected")) {
      throw new CompileError("'protected' has no meaning — zinc has no inheritance; "
          + "use private or public");
    }
    if (seen.contains("public") && seen.contains("private")) {
      throw new CompileError("a member cannot be both public and private");
    }
    return seen;
  }

  private void checkMethodMods(Set<String> mods, String where, boolean staticRequired,
      boolean privateOk) {
    if (mods.contains("final")) {
      throw new CompileError(where + ": 'final' on methods has no meaning — "
          + "zinc has no inheritance");
    }
    if (staticRequired && !mods.contains("static")) {
      throw new CompileError(where + ": utility-class methods are static — add 'static' "
          + "(instance state lives in Actors and instance classes)");
    }
    if (!staticRequired && mods.contains("static")) {
      throw new CompileError(where + ": 'static' does not belong here — "
          + "these methods run against an instance");
    }
    if (!privateOk && mods.contains("private")) {
      throw new CompileError(where + ": cannot be private — an Actor's methods are its "
          + "protocol; private helpers belong in a utility class");
    }
  }

  private void checkFieldMods(Set<String> mods, String where) {
    if (mods.contains("static")) {
      throw new CompileError(where + ": static fields are not supported (v1)");
    }
    if (mods.contains("public")) {
      throw new CompileError(where + ": fields are encapsulated — no public fields; "
          + "expose state through methods");
    }
  }

  /** Base, generic or array type: int, List<String>, Map<String, Integer>, int[], var.
   *  Generics are ERASED at lowering; type args feed static checks + boundary guards. */
  private String parseType() {
    if (match(TokKind.KW_VAR)) return "var";
    var t = new StringBuilder(expect(TokKind.IDENT, "type").text());
    if (check(TokKind.LT)) {
      int save = pos;
      advance();
      var args = new ArrayList<String>();
      args.add(parseType());
      while (match(TokKind.COMMA)) args.add(parseType());
      if (check(TokKind.GT)) {
        advance();
        t.append("<").append(String.join(", ", args)).append(">");
      } else {
        pos = save; // was a comparison, not type args
      }
    }
    while (check(TokKind.LBRACKET) && toks.get(pos + 1).kind() == TokKind.RBRACKET) {
      pos += 2;
      t.append("[]");
    }
    return t.toString();
  }

  /** class Name [implements Application|Actor|Test|<Interface> | extends Exception] { ... } */
  private void parseClassLike(List<ClassDecl> classes, List<ActorDecl> actors,
      ApplicationDecl[] application, List<ExceptionDecl> exceptions,
      List<InstanceClassDecl> instanceClasses, List<TestDecl> tests) {
    advance(); // 'class'
    String name = expect(TokKind.IDENT, "class name").text();
    if (checkIdent("extends")) {
      advance();
      String parent = expect(TokKind.IDENT, "parent type").text();
      if (!parent.equals("Exception")) {
        throw new CompileError("class " + name + " extends " + parent
            + ": 'extends Exception' is the one sanctioned extends (v1)");
      }
      exceptions.add(parseExceptionBody(name));
      return;
    }
    String marker = null;
    if (checkIdent("implements")) {
      advance();
      marker = expect(TokKind.IDENT, "interface name").text();
    }
    if (marker == null) {
      classes.add(parseClassBody(name));
    } else if (marker.equals("Actor")) {
      actors.add(parseActorBody(name, "Actor"));
    } else if (marker.equals("Test")) {
      tests.add(parseTestBody(name));
    } else if (marker.equals("Application")) {
      if (application[0] != null) {
        throw new CompileError("more than one Application in this file");
      }
      ActorDecl body = parseActorBody(name, "Application"); // same body shape
      if (body.ctor() != null) {
        throw new CompileError("Application " + name + " cannot have a constructor");
      }
      MethodDecl main = null;
      for (MethodDecl m : body.methods()) {
        if (!m.name().equals("main")) {
          throw new CompileError("Application " + name + " can only declare main(String[]):"
              + " it is the boundary, not a unit — methods live on Actors");
        }
        if (!m.mods().contains("public")) {
          throw new CompileError(name
              + ".main: the entrypoint is 'public static void main(String[] args)'");
        }
        main = m;
      }
      application[0] = new ApplicationDecl(name, body.fields(), main);
    } else {
      // user interface: an instance class — module + map term, ctor-set immutable fields
      ActorDecl body = parseActorBody(name, "instance class");
      instanceClasses.add(
          new InstanceClassDecl(name, marker, body.fields(), body.ctor(), body.methods()));
    }
  }

  private ClassDecl parseClassBody(String name) {
    expect(TokKind.LBRACE, "'{'");
    var methods = new ArrayList<MethodDecl>();
    while (!check(TokKind.RBRACE)) {
      Set<String> mods = skipModifiers();
      MethodDecl m = parseMethod(mods);
      checkMethodMods(mods, name + "." + m.name(), true, true);
      methods.add(m);
    }
    expect(TokKind.RBRACE, "'}'");
    return new ClassDecl(name, methods);
  }

  /** Test class: methods only (no fields/ctor — state lives in spawned Actors, reclaimed
   *  when the test's process dies); public void zero-arg = test case, rest helpers. */
  private TestDecl parseTestBody(String name) {
    expect(TokKind.LBRACE, "'{'");
    var methods = new ArrayList<MethodDecl>();
    var testNames = new ArrayList<String>();
    while (!check(TokKind.RBRACE)) {
      Set<String> mods = skipModifiers();
      MethodDecl m = parseMethod(mods);
      if (mods.contains("static") || mods.contains("final")) {
        throw new CompileError(name + "." + m.name()
            + ": test methods follow the JUnit-3 shape — public void name(), no static/final");
      }
      methods.add(m);
      if (mods.contains("public") && m.retType().equals("void") && m.params().isEmpty()) {
        testNames.add(m.name());
      }
    }
    expect(TokKind.RBRACE, "'}'");
    if (testNames.isEmpty()) {
      throw new CompileError("test class " + name
          + " has no test cases: a test is a public void method with no parameters");
    }
    return new TestDecl(name, methods, testNames);
  }

  private MethodDecl parseMethod(Set<String> mods) {
    String ret = parseType();
    String name = expect(TokKind.IDENT, "method name").text();
    if (!check(TokKind.LPAREN)) {
      throw new CompileError("static fields are not supported (v1), at line " + cur().line());
    }
    return new MethodDecl(ret, name, parseParams(), parseBlock(), mods);
  }

  private List<Param> parseParams() {
    expect(TokKind.LPAREN, "'('");
    var params = new ArrayList<Param>();
    if (!check(TokKind.RPAREN)) {
      do {
        String t = parseType();
        params.add(new Param(t, expect(TokKind.IDENT, "parameter name").text()));
      } while (match(TokKind.COMMA));
    }
    expect(TokKind.RPAREN, "')'");
    return params;
  }

  private RecordDecl parseRecord() {
    advance(); // 'record'
    String name = expect(TokKind.IDENT, "record name").text();
    List<Param> comps = parseParams();
    expect(TokKind.LBRACE, "'{'");
    expect(TokKind.RBRACE, "'}'  (record bodies not supported, v1)");
    return new RecordDecl(name, comps);
  }

  /** Exception body: fields only — final values, positional construction at throw. */
  private ExceptionDecl parseExceptionBody(String name) {
    expect(TokKind.LBRACE, "'{'");
    var fields = new ArrayList<FieldDecl>();
    while (!check(TokKind.RBRACE)) {
      Set<String> mods = skipModifiers();
      String type = parseType();
      String fieldName = expect(TokKind.IDENT, "field name").text();
      checkFieldMods(mods, name + "." + fieldName);
      expect(TokKind.SEMI, "';' (exception classes hold fields only, v1)");
      fields.add(new FieldDecl(type, fieldName, null, mods));
    }
    expect(TokKind.RBRACE, "'}'");
    return new ExceptionDecl(name, fields);
  }

  /** Shared body shape for Actor / Application / instance classes; `kind` picks the
   *  modifier rules: Application methods (main) are static, the others never are;
   *  private methods only make sense on instance classes (Actor methods = protocol). */
  private ActorDecl parseActorBody(String name, String kind) {
    boolean isApp = kind.equals("Application");
    boolean isInstance = kind.equals("instance class");
    expect(TokKind.LBRACE, "'{'");
    var fields = new ArrayList<FieldDecl>();
    var methods = new ArrayList<MethodDecl>();
    MethodDecl ctor = null;
    while (!check(TokKind.RBRACE)) {
      Set<String> mods = skipModifiers();
      String type = parseType();
      if (type.equals(name) && check(TokKind.LPAREN)) { // constructor
        if (ctor != null) throw new CompileError("Actor " + name + ": duplicate constructor");
        if (mods.contains("static") || mods.contains("final") || mods.contains("private")) {
          throw new CompileError(name + ": a constructor takes no modifiers (public is ok)");
        }
        ctor = new MethodDecl("", name, parseParams(), parseBlock(), mods);
        continue;
      }
      String memberName = expect(TokKind.IDENT, "member name").text();
      if (check(TokKind.LPAREN)) {
        checkMethodMods(mods, name + "." + memberName, isApp, isInstance);
        methods.add(new MethodDecl(type, memberName, parseParams(), parseBlock(), mods));
      } else if (match(TokKind.ASSIGN)) {
        checkFieldMods(mods, name + "." + memberName);
        Expr init = parseExpr();
        expect(TokKind.SEMI, "';'");
        fields.add(new FieldDecl(type, memberName, init, mods));
      } else {
        checkFieldMods(mods, name + "." + memberName);
        expect(TokKind.SEMI, "';'");
        fields.add(new FieldDecl(type, memberName, null, mods)); // defaulted by type
      }
    }
    expect(TokKind.RBRACE, "'}'");
    return new ActorDecl(name, fields, ctor, methods);
  }

  // ---- statements ----

  private Block parseBlock() {
    expect(TokKind.LBRACE, "'{'");
    var stmts = new ArrayList<Stmt>();
    while (!check(TokKind.RBRACE) && !check(TokKind.EOF)) {
      stmts.add(parseStmt());
    }
    expect(TokKind.RBRACE, "'}'");
    return new Block(stmts);
  }

  private boolean isAssignOp(TokKind k) {
    return k == TokKind.ASSIGN || k == TokKind.PLUS_EQ || k == TokKind.MINUS_EQ
        || k == TokKind.STAR_EQ;
  }

  /** Statements get their source line here — one site, every statement kind. */
  private Stmt parseStmt() {
    int ln = cur().line();
    return withLine(parseStmt0(), ln);
  }

  private static Stmt withLine(Stmt s, int ln) {
    return switch (s) {
      case VarStmt x -> new VarStmt(x.type(), x.name(), x.init(), x.isFinal(), ln);
      case AssignStmt x -> new AssignStmt(x.name(), x.op(), x.value(), ln);
      case FieldAssignStmt x -> new FieldAssignStmt(x.objVar(), x.field(), x.op(),
          x.value(), ln);
      case IndexAssignStmt x -> new IndexAssignStmt(x.arrVar(), x.index(), x.op(),
          x.value(), ln);
      case SwitchStmt x -> new SwitchStmt(x.subject(), x.cases(), x.defaultBlock(), ln);
      case IfStmt x -> new IfStmt(x.cond(), x.thenBlock(), x.elseBlock(), ln);
      case ForEachStmt x -> new ForEachStmt(x.varType(), x.varName(), x.iterable(),
          x.body(), ln);
      case WhileStmt x -> new WhileStmt(x.cond(), x.body(), ln);
      case ReturnStmt x -> new ReturnStmt(x.value(), ln);
      case ExprStmt x -> new ExprStmt(x.expr(), ln);
      case ThrowStmt x -> new ThrowStmt(x.exType(), x.args(), ln);
      case TryStmt x -> new TryStmt(x.tryBlock(), x.clauses(), ln);
      // desugared classic for: stamp the parts that came from this source line
      case SeqStmt x -> new SeqStmt(x.stmts().stream()
          .map(st -> st.line() == 0 ? withLine(st, ln) : st).toList());
      case BreakStmt x -> x;
      case ContinueStmt x -> x;
    };
  }

  private Stmt parseStmt0() {
    switch (cur().kind()) {
      case KW_IF:
        return parseIf();
      case KW_FOR:
        return parseFor();
      case KW_WHILE:
        return parseWhile();
      case KW_RETURN:
        return parseReturn();
      case KW_BREAK:
        advance();
        expect(TokKind.SEMI, "';'");
        return new BreakStmt();
      case KW_CONTINUE:
        advance();
        expect(TokKind.SEMI, "';'");
        return new ContinueStmt();
      default:
        if (checkIdent("try")) return parseTry();
        if (checkIdent("throw")) {
          advance();
          if (!checkIdent("new")) {
            throw new CompileError("v1: throw takes a new exception — throw new X(...), at line "
                + cur().line());
          }
          advance();
          String exType = expect(TokKind.IDENT, "exception type").text();
          expect(TokKind.LPAREN, "'('");
          var args = new ArrayList<Expr>();
          if (!check(TokKind.RPAREN)) {
            do {
              args.add(parseExpr());
            } while (match(TokKind.COMMA));
          }
          expect(TokKind.RPAREN, "')'");
          expect(TokKind.SEMI, "';'");
          return new ThrowStmt(exType, args);
        }
        if (checkIdent("switch") && toks.get(pos + 1).kind() == TokKind.LPAREN) {
          return parseSwitch();
        }
        if (checkIdent("final")) { // final local: declaration that can't be reassigned
          advance();
          if (!looksLikeDecl()) {
            throw new CompileError("'final' precedes a variable declaration, at line "
                + cur().line());
          }
          VarStmt v = (VarStmt) parseLocalDecl();
          return new VarStmt(v.type(), v.name(), v.init(), true);
        }
        if (looksLikeDecl()) return parseLocalDecl();
        Stmt s = parseSimpleStmt();
        expect(TokKind.SEMI, "';'");
        return s;
    }
  }

  private Stmt parseSwitch() {
    advance(); // 'switch'
    expect(TokKind.LPAREN, "'('");
    Expr subject = parseExpr();
    expect(TokKind.RPAREN, "')'");
    expect(TokKind.LBRACE, "'{'");
    var cases = new ArrayList<SwitchCase>();
    Block defaultBlock = null;
    while (!check(TokKind.RBRACE)) {
      if (checkIdent("default")) {
        advance();
        expect(TokKind.ARROW, "'->'");
        if (defaultBlock != null) throw new CompileError("duplicate default case");
        defaultBlock = parseArmBody();
      } else if (checkIdent("case")) {
        advance();
        var labels = new ArrayList<Expr>();
        labels.add(parseOr()); // parseOr, not parseExpr: labels can't be lambdas
        while (match(TokKind.COMMA)) {
          labels.add(parseOr());
        }
        expect(TokKind.ARROW, "'->'");
        cases.add(new SwitchCase(labels, parseArmBody()));
      } else {
        throw new CompileError("Parse error: expected case/default at line " + cur().line());
      }
    }
    expect(TokKind.RBRACE, "'}'");
    return new SwitchStmt(subject, cases, defaultBlock);
  }

  private Block parseArmBody() {
    return check(TokKind.LBRACE) ? parseBlock() : new Block(List.of(parseStmt()));
  }

  private Stmt parseTry() {
    advance(); // 'try'
    Block tryBlock = parseBlock();
    if (!checkIdent("catch")) {
      throw new CompileError("Parse error: expected 'catch' at line " + cur().line());
    }
    var clauses = new ArrayList<CatchClause>();
    while (checkIdent("catch")) {
      advance();
      expect(TokKind.LPAREN, "'('");
      String exType = expect(TokKind.IDENT, "exception type").text();
      String exVar = expect(TokKind.IDENT, "exception variable").text();
      expect(TokKind.RPAREN, "')'");
      clauses.add(new CatchClause(exType, exVar, parseBlock()));
    }
    return new TryStmt(tryBlock, clauses);
  }

  /** IDENT ('[' ']')* IDENT, or 'var' IDENT — a local declaration. */
  private boolean looksLikeDecl() {
    if (check(TokKind.KW_VAR)) return true;
    if (!check(TokKind.IDENT)) return false;
    int save = pos;
    advance();
    skipTypeArgs();
    while (check(TokKind.LBRACKET) && toks.get(pos + 1).kind() == TokKind.RBRACKET) {
      pos += 2;
    }
    boolean decl = check(TokKind.IDENT);
    pos = save;
    return decl;
  }

  /** Best-effort skip of a balanced <...> in a type position (lookahead only). */
  private void skipTypeArgs() {
    if (!check(TokKind.LT)) return;
    int depth = 0;
    int p = pos;
    while (p < toks.size()) {
      TokKind k = toks.get(p).kind();
      if (k == TokKind.LT) depth++;
      else if (k == TokKind.GT && --depth == 0) {
        pos = p + 1;
        return;
      } else if (k != TokKind.IDENT && k != TokKind.COMMA && k != TokKind.LBRACKET
          && k != TokKind.RBRACKET) {
        return; // not type args (e.g. a < b comparison)
      }
      p++;
    }
  }

  private Stmt parseLocalDecl() {
    String type = parseType();
    String name = expect(TokKind.IDENT, "variable name").text();
    expect(TokKind.ASSIGN, "'=' (declarations need an initializer, v1)");
    Expr init = check(TokKind.LBRACE) ? parseArrayLit() : parseExpr();
    expect(TokKind.SEMI, "';'");
    return new VarStmt(type, name, init);
  }

  private Expr parseArrayLit() {
    expect(TokKind.LBRACE, "'{'");
    var elems = new ArrayList<Expr>();
    if (!check(TokKind.RBRACE)) {
      do {
        elems.add(parseExpr());
      } while (match(TokKind.COMMA));
    }
    expect(TokKind.RBRACE, "'}'");
    return new ListLit(elems);
  }

  /** Assignment, ++/--, or expression — no trailing semicolon (also used as for-update). */
  private Stmt parseSimpleStmt() {
    Expr e = parseExpr();
    if (check(TokKind.PLUS_PLUS) || check(TokKind.MINUS_MINUS)) {
      String op = advance().kind() == TokKind.PLUS_PLUS ? "+=" : "-=";
      return makeAssign(e, op, new IntLit(1));
    }
    if (isAssignOp(cur().kind())) {
      String op = advance().text();
      return makeAssign(e, op, parseExpr());
    }
    return new ExprStmt(e);
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

  private Stmt parseIf() {
    expect(TokKind.KW_IF, "'if'");
    expect(TokKind.LPAREN, "'('");
    Expr cond = parseExpr();
    expect(TokKind.RPAREN, "')'");
    Block thenBlock = parseBlock();
    Block elseBlock = null;
    if (match(TokKind.KW_ELSE)) {
      elseBlock = check(TokKind.KW_IF) ? new Block(List.of(parseIf())) : parseBlock();
    }
    return new IfStmt(cond, thenBlock, elseBlock);
  }

  private Stmt parseWhile() {
    expect(TokKind.KW_WHILE, "'while'");
    expect(TokKind.LPAREN, "'('");
    Expr cond = parseExpr();
    expect(TokKind.RPAREN, "')'");
    return new WhileStmt(cond, parseBlock());
  }

  private Stmt parseReturn() {
    expect(TokKind.KW_RETURN, "'return'");
    if (match(TokKind.SEMI)) return new ReturnStmt(null);
    Expr v = parseExpr();
    expect(TokKind.SEMI, "';'");
    return new ReturnStmt(v);
  }

  /** Enhanced for -> ForEachStmt; classic for -> { init; while (cond) { body'; update } }. */
  private Stmt parseFor() {
    expect(TokKind.KW_FOR, "'for'");
    expect(TokKind.LPAREN, "'('");
    if (looksLikeDecl()) {
      int save = pos;
      String type = parseType();
      String name = expect(TokKind.IDENT, "variable name").text();
      if (match(TokKind.COLON)) {
        Expr iter = parseExpr();
        expect(TokKind.RPAREN, "')'");
        return new ForEachStmt(type, name, iter, parseBlock());
      }
      pos = save;
    }
    Stmt init = looksLikeDecl() ? parseLocalDecl() : seqInit();
    Expr cond = parseExpr();
    expect(TokKind.SEMI, "';'");
    Stmt update = parseSimpleStmt();
    expect(TokKind.RPAREN, "')'");
    Block body = parseBlock();
    // continue must still run the update; break must not
    var stmts = new ArrayList<>(rewriteContinue(body, update).stmts());
    stmts.add(update);
    return new SeqStmt(List.of(init, new WhileStmt(cond, new Block(stmts))));
  }

  private Stmt seqInit() {
    Stmt s = parseSimpleStmt();
    expect(TokKind.SEMI, "';'");
    return s;
  }

  /** Replace `continue` belonging to THIS loop with `update; continue` (descends ifs only). */
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

  // ---- expressions ----

  Expr parseExpr() {
    if (lambdaAhead()) return parseLambda();
    return parseOr();
  }

  /** IDENT ->  |  ( [IDENT {, IDENT}] ) ->  */
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
    while (true) {
      if (match(TokKind.DOT)) {
        String name = expect(TokKind.IDENT, "member name").text();
        if (match(TokKind.LPAREN)) {
          var args = new ArrayList<Expr>();
          if (!check(TokKind.RPAREN)) {
            do {
              args.add(parseExpr());
            } while (match(TokKind.COMMA));
          }
          expect(TokKind.RPAREN, "')'");
          e = new MethodCall(e, name, args);
        } else {
          e = new FieldAccess(e, name);
        }
      } else if (match(TokKind.LBRACKET)) {
        Expr idx = parseExpr();
        expect(TokKind.RBRACKET, "']'");
        e = new Index(e, idx);
      } else {
        break;
      }
    }
    return e;
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
    if (check(TokKind.STR_LIT)) return new StrLit(advance().text());
    if (checkIdent("new")) {
      advance();
      String type = expect(TokKind.IDENT, "type name").text();
      if (check(TokKind.LT)) { // type args, ERASED: new HashMap<String, User>() / diamond
        advance();
        if (!match(TokKind.GT)) {
          do {
            parseType();
          } while (match(TokKind.COMMA));
          expect(TokKind.GT, "'>'");
        }
      }
      if (match(TokKind.LBRACKET)) { // new int[n]
        Expr size = parseExpr();
        expect(TokKind.RBRACKET, "']'");
        return new ArrayNewExpr(type, size);
      }
      expect(TokKind.LPAREN, "'('");
      var args = new ArrayList<Expr>();
      if (!check(TokKind.RPAREN)) {
        do {
          args.add(parseExpr());
        } while (match(TokKind.COMMA));
      }
      expect(TokKind.RPAREN, "')'");
      return new NewExpr(type, args);
    }
    if (match(TokKind.LPAREN)) {
      Expr e = parseExpr();
      expect(TokKind.RPAREN, "')'");
      return e;
    }
    if (check(TokKind.IDENT)) {
      String name = advance().text();
      if (match(TokKind.LPAREN)) {
        var args = new ArrayList<Expr>();
        if (!check(TokKind.RPAREN)) {
          do {
            args.add(parseExpr());
          } while (match(TokKind.COMMA));
        }
        expect(TokKind.RPAREN, "')'");
        return new Call(name, args);
      }
      return new VarRef(name);
    }
    throw new CompileError("Parse error: unexpected " + cur().kind() + " \"" + cur().text()
        + "\" at line " + cur().line());
  }
}
