package zinc;

import java.util.ArrayList;
import java.util.List;
import zinc.Ast.*;

class Parser {
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

  Program parseProgram() {
    var imports = new ArrayList<Import>();
    while (check(TokKind.KW_IMPORT)) {
      imports.add(parseImport());
    }
    var fns = new ArrayList<FnDecl>();
    while (!check(TokKind.EOF)) {
      if (check(TokKind.KW_STRUCT)) {
        skipStruct(); // struct fields aren't needed for map-based codegen
      } else {
        fns.add(parseFn());
      }
    }
    return new Program(imports, fns);
  }

  private Import parseImport() {
    expect(TokKind.KW_IMPORT, "'import'");
    var path = new ArrayList<String>();
    path.add(expect(TokKind.IDENT, "module path").text());
    while (match(TokKind.SLASH)) {
      path.add(expect(TokKind.IDENT, "module path segment").text());
    }
    return new Import(path);
  }

  private void skipStruct() {
    expect(TokKind.KW_STRUCT, "'struct'");
    expect(TokKind.IDENT, "struct name");
    expect(TokKind.LBRACE, "'{'");
    if (!check(TokKind.RBRACE)) {
      expect(TokKind.IDENT, "field name");
      while (match(TokKind.COMMA)) {
        expect(TokKind.IDENT, "field name");
      }
    }
    expect(TokKind.RBRACE, "'}'");
  }

  private FnDecl parseFn() {
    expect(TokKind.KW_FN, "'fn'");
    String name = expect(TokKind.IDENT, "function name").text();
    expect(TokKind.LPAREN, "'('");
    var params = new ArrayList<String>();
    if (!check(TokKind.RPAREN)) {
      params.add(expect(TokKind.IDENT, "parameter").text());
      while (match(TokKind.COMMA)) {
        params.add(expect(TokKind.IDENT, "parameter").text());
      }
    }
    expect(TokKind.RPAREN, "')'");
    return new FnDecl(name, params, parseBlock());
  }

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
      case KW_VAR:
        return parseVar();
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
        return new BreakStmt();
      case KW_CONTINUE:
        advance();
        return new ContinueStmt();
      default:
        Expr e = parseExpr();
        if (isAssignOp(cur().kind())) {
          String op = advance().text();
          return makeAssign(e, op, parseExpr());
        }
        return new ExprStmt(e);
    }
  }

  private Stmt makeAssign(Expr lvalue, String op, Expr rhs) {
    if (lvalue instanceof VarRef v) return new AssignStmt(v.name(), op, rhs);
    if (lvalue instanceof FieldAccess fa && fa.obj() instanceof VarRef v) {
      return new FieldAssignStmt(v.name(), fa.field(), op, rhs);
    }
    throw new CompileError("Parse error: invalid assignment target at line " + cur().line());
  }

  private Stmt parseVar() {
    expect(TokKind.KW_VAR, "'var'");
    String name = expect(TokKind.IDENT, "variable name").text();
    expect(TokKind.ASSIGN, "'='");
    return new VarStmt(name, parseExpr());
  }

  private Stmt parseIf() {
    expect(TokKind.KW_IF, "'if'");
    expect(TokKind.LPAREN, "'('");
    Expr cond = parseExpr();
    expect(TokKind.RPAREN, "')'");
    Block thenBlock = parseBlock();
    Block elseBlock = null;
    if (match(TokKind.KW_ELSE)) {
      // `else if` -> wrap the nested if as the else block
      elseBlock = check(TokKind.KW_IF) ? new Block(List.of(parseIf())) : parseBlock();
    }
    return new IfStmt(cond, thenBlock, elseBlock);
  }

  private Stmt parseFor() {
    expect(TokKind.KW_FOR, "'for'");
    expect(TokKind.LPAREN, "'('");
    String varName = expect(TokKind.IDENT, "loop variable").text();
    expect(TokKind.KW_IN, "'in'");
    Expr first = parseExpr();
    if (match(TokKind.DOT_DOT)) {
      Expr end = parseExpr();
      expect(TokKind.RPAREN, "')'");
      return new ForRangeStmt(varName, first, end, parseBlock());
    }
    expect(TokKind.RPAREN, "')'");
    return new ForEachStmt(varName, first, parseBlock());
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
    if (check(TokKind.RBRACE)) return new ReturnStmt(null);
    return new ReturnStmt(parseExpr());
  }

  Expr parseExpr() {
    return parseOr();
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
        String name = expect(TokKind.IDENT, "field name").text();
        if (match(TokKind.LPAREN)) {
          var args = new ArrayList<Expr>();
          if (!check(TokKind.RPAREN)) {
            args.add(parseExpr());
            while (match(TokKind.COMMA)) {
              args.add(parseExpr());
            }
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
    if (check(TokKind.STR_LIT)) return parseStr(advance().text());
    if (match(TokKind.LPAREN)) {
      Expr e = parseExpr();
      expect(TokKind.RPAREN, "')'");
      return e;
    }
    if (match(TokKind.LBRACKET)) {
      var elems = new ArrayList<Expr>();
      if (!check(TokKind.RBRACKET)) {
        elems.add(parseExpr());
        while (match(TokKind.COMMA)) {
          elems.add(parseExpr());
        }
      }
      expect(TokKind.RBRACKET, "']'");
      return new ListLit(elems);
    }
    if (check(TokKind.IDENT)) {
      String name = advance().text();
      if (match(TokKind.LPAREN)) {
        var args = new ArrayList<Expr>();
        if (!check(TokKind.RPAREN)) {
          args.add(parseExpr());
          while (match(TokKind.COMMA)) {
            args.add(parseExpr());
          }
        }
        expect(TokKind.RPAREN, "')'");
        return new Call(name, args);
      }
      if (check(TokKind.LBRACE)) return parseStructLit(name);
      return new VarRef(name);
    }
    throw new CompileError("Parse error: unexpected " + cur().kind() + " \"" + cur().text()
        + "\" at line " + cur().line());
  }

  private Expr parseStructLit(String name) {
    expect(TokKind.LBRACE, "'{'");
    var fields = new ArrayList<FieldInit>();
    if (!check(TokKind.RBRACE)) {
      do {
        String f = expect(TokKind.IDENT, "field name").text();
        expect(TokKind.COLON, "':'");
        fields.add(new FieldInit(f, parseExpr()));
      } while (match(TokKind.COMMA));
    }
    expect(TokKind.RBRACE, "'}'");
    return new StructLit(name, fields);
  }

  private Expr parseStr(String raw) {
    var parts = new ArrayList<StrPart>();
    var buf = new StringBuilder();
    int i = 0;
    while (i < raw.length()) {
      if (i + 1 < raw.length() && raw.charAt(i) == '$' && raw.charAt(i + 1) == '{') {
        if (buf.length() > 0) {
          parts.add(new StrText(buf.toString()));
          buf.setLength(0);
        }
        int end = raw.indexOf('}', i + 2);
        if (end < 0) throw new CompileError("unterminated ${...} in string");
        Expr sub = new Parser(Lexer.lex(raw.substring(i + 2, end))).parseExpr();
        parts.add(new StrExpr(sub));
        i = end + 1;
      } else {
        buf.append(raw.charAt(i));
        i++;
      }
    }
    if (buf.length() > 0) parts.add(new StrText(buf.toString()));
    return new StrLit(parts);
  }
}
