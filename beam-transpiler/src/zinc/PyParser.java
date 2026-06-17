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
  private int pos = 0;
  private Set<String> declared = new HashSet<>(); // locals seen in the current method

  PyParser(List<Token> toks) {
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

  private void skipSemis() {
    while (check(TokKind.SEMI)) {
      advance();
    }
  }

  // ---- declarations ----

  Program parseProgram() {
    skipSemis();
    var topDefs = new ArrayList<MethodDecl>(); // become static methods of class Main
    while (!check(TokKind.EOF)) {
      if (checkIdent("def")) {
        topDefs.add(parseDef(true));
      } else {
        throw new CompileError("Parse error: only top-level `def`s are supported (v1) at line "
            + cur().line());
      }
      skipSemis();
    }
    var classes = new ArrayList<ClassDecl>();
    if (!topDefs.isEmpty()) {
      classes.add(new ClassDecl("Main", topDefs));
    }
    var program = new Program(List.of(), classes, List.of(), List.of(), List.of(), null,
        List.of(), List.of(), List.of(), List.of());
    return PyInfer.infer(program); // resolve `infer` return types from method bodies
  }

  /** `def NAME ( params ) [-> TYPE] { block }`. Top-level defs are public static; `main`
   *  with no params gets the synthetic `String[] args` so it matches the Java entry path. */
  private MethodDecl parseDef(boolean topLevel) {
    expect(TokKind.IDENT, "'def'"); // 'def'
    String name = expect(TokKind.IDENT, "function name").text();
    List<Param> params = parseParams();
    String ret = "infer"; // sentinel: PyInfer resolves from the body (void if no value-return)
    if (match(TokKind.ARROW)) {
      ret = parseType();
    }
    declared = new HashSet<>();
    for (Param p : params) {
      declared.add(p.name());
    }
    Block body = parseBlock();
    if (topLevel && name.equals("main") && params.isEmpty()) {
      params = List.of(new Param("String[]", "args")); // -> main/1, the hardwired entry
    }
    var mods = topLevel ? Set.of("public", "static") : Set.of("public");
    return new MethodDecl(ret, name, params, body, mods);
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

  private Stmt parseStatement() {
    if (check(TokKind.KW_RETURN)) return parseReturn();
    if (check(TokKind.KW_IF)) return parseIf();
    if (check(TokKind.KW_WHILE)) return parseWhile();
    if (check(TokKind.KW_FOR)) return parseFor();
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
    if (lambdaAhead()) return parseLambda();
    return parseTernary();
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
    while (true) {
      if (match(TokKind.DOT)) {
        String name = expect(TokKind.IDENT, "member name").text();
        if (match(TokKind.LPAREN)) {
          var args = parseArgs();
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
    if (check(TokKind.STR_LIT)) return new StrLit(advance().text());
    if (match(TokKind.LPAREN)) {
      Expr e = parseExpr();
      expect(TokKind.RPAREN, "')'");
      return e;
    }
    if (check(TokKind.IDENT)) {
      String name = advance().text();
      if (match(TokKind.LPAREN)) {
        var args = parseArgs();
        if (name.equals("print")) { // desugar to the node CodeGen already lowers
          return new MethodCall(new FieldAccess(new VarRef("System"), "out"), "println", args);
        }
        return new Call(name, args);
      }
      return new VarRef(name);
    }
    throw new CompileError("Parse error: unexpected " + cur().kind() + " \"" + cur().text()
        + "\" at line " + cur().line());
  }
}
