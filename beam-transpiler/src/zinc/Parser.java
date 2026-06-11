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
    while (!check(TokKind.EOF)) {
      skipModifiers();
      if (checkIdent("class")) {
        parseClassLike(classes, actors, application, exceptions);
      } else if (checkIdent("record")) {
        records.add(parseRecord());
      } else if (checkIdent("enum")) {
        enums.add(parseEnum());
      } else {
        throw new CompileError("Parse error: expected class, record or enum at line "
            + cur().line());
      }
    }
    return new Program(imports, classes, records, actors, enums, application[0], exceptions);
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

  private void skipModifiers() {
    while (check(TokKind.IDENT) && MODIFIERS.contains(cur().text())) {
      advance();
    }
  }

  /** Base type or array type: int, double, String, Point, int[], void, var. */
  private String parseType() {
    if (match(TokKind.KW_VAR)) return "var";
    var t = new StringBuilder(expect(TokKind.IDENT, "type").text());
    while (check(TokKind.LBRACKET) && toks.get(pos + 1).kind() == TokKind.RBRACKET) {
      pos += 2;
      t.append("[]");
    }
    return t.toString();
  }

  /** class Name [implements Application|Actor | extends Exception] { ... } */
  private void parseClassLike(List<ClassDecl> classes, List<ActorDecl> actors,
      ApplicationDecl[] application, List<ExceptionDecl> exceptions) {
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
      actors.add(parseActorBody(name));
    } else if (marker.equals("Application")) {
      if (application[0] != null) {
        throw new CompileError("more than one Application in this file");
      }
      ActorDecl body = parseActorBody(name); // same body shape: fields + methods
      if (body.ctor() != null) {
        throw new CompileError("Application " + name + " cannot have a constructor");
      }
      MethodDecl main = null;
      for (MethodDecl m : body.methods()) {
        if (!m.name().equals("main")) {
          throw new CompileError("Application " + name + " can only declare main(String[]):"
              + " it is the boundary, not a unit — methods live on Actors");
        }
        main = m;
      }
      application[0] = new ApplicationDecl(name, body.fields(), main);
    } else {
      throw new CompileError("unknown interface '" + marker
          + "': v1 markers are Application and Actor, at line " + cur().line());
    }
  }

  private ClassDecl parseClassBody(String name) {
    expect(TokKind.LBRACE, "'{'");
    var methods = new ArrayList<MethodDecl>();
    while (!check(TokKind.RBRACE)) {
      skipModifiers();
      methods.add(parseMethod());
    }
    expect(TokKind.RBRACE, "'}'");
    return new ClassDecl(name, methods);
  }

  private MethodDecl parseMethod() {
    String ret = parseType();
    String name = expect(TokKind.IDENT, "method name").text();
    if (!check(TokKind.LPAREN)) {
      throw new CompileError("static fields are not supported (v1), at line " + cur().line());
    }
    return new MethodDecl(ret, name, parseParams(), parseBlock());
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
      skipModifiers();
      String type = parseType();
      String fieldName = expect(TokKind.IDENT, "field name").text();
      expect(TokKind.SEMI, "';' (exception classes hold fields only, v1)");
      fields.add(new FieldDecl(type, fieldName, null));
    }
    expect(TokKind.RBRACE, "'}'");
    return new ExceptionDecl(name, fields);
  }

  private ActorDecl parseActorBody(String name) {
    expect(TokKind.LBRACE, "'{'");
    var fields = new ArrayList<FieldDecl>();
    var methods = new ArrayList<MethodDecl>();
    MethodDecl ctor = null;
    while (!check(TokKind.RBRACE)) {
      skipModifiers();
      String type = parseType();
      if (type.equals(name) && check(TokKind.LPAREN)) { // constructor
        if (ctor != null) throw new CompileError("Actor " + name + ": duplicate constructor");
        ctor = new MethodDecl("", name, parseParams(), parseBlock());
        continue;
      }
      String memberName = expect(TokKind.IDENT, "member name").text();
      if (check(TokKind.LPAREN)) {
        methods.add(new MethodDecl(type, memberName, parseParams(), parseBlock()));
      } else if (match(TokKind.ASSIGN)) {
        Expr init = parseExpr();
        expect(TokKind.SEMI, "';'");
        fields.add(new FieldDecl(type, memberName, init));
      } else {
        expect(TokKind.SEMI, "';'");
        fields.add(new FieldDecl(type, memberName, null)); // defaulted by type
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

  private Stmt parseStmt() {
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
    while (check(TokKind.LBRACKET) && toks.get(pos + 1).kind() == TokKind.RBRACKET) {
      pos += 2;
    }
    boolean decl = check(TokKind.IDENT);
    pos = save;
    return decl;
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
