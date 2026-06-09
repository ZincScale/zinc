import 'token.dart';
import 'ast.dart';
import 'lexer.dart';

class Parser {
  final List<Token> toks;
  int pos = 0;
  Parser(this.toks);

  Token get cur => toks[pos];
  Token advance() => toks[pos++];
  bool check(TokKind k) => cur.kind == k;
  bool match(TokKind k) {
    if (check(k)) {
      pos++;
      return true;
    }
    return false;
  }

  Token expect(TokKind k, String what) {
    if (!check(k)) {
      throw FormatException(
          'Parse error: expected $what but got ${cur.kind} "${cur.text}" at line ${cur.line}');
    }
    return advance();
  }

  List<FnDecl> parseProgram() {
    final fns = <FnDecl>[];
    while (!check(TokKind.eof)) {
      if (check(TokKind.kwStruct)) {
        _skipStruct(); // struct fields aren't needed for map-based codegen
      } else {
        fns.add(parseFn());
      }
    }
    return fns;
  }

  void _skipStruct() {
    expect(TokKind.kwStruct, "'struct'");
    expect(TokKind.ident, 'struct name');
    expect(TokKind.lbrace, "'{'");
    if (!check(TokKind.rbrace)) {
      expect(TokKind.ident, 'field name');
      while (match(TokKind.comma)) {
        expect(TokKind.ident, 'field name');
      }
    }
    expect(TokKind.rbrace, "'}'");
  }

  FnDecl parseFn() {
    expect(TokKind.kwFn, "'fn'");
    final name = expect(TokKind.ident, 'function name').text;
    expect(TokKind.lparen, "'('");
    final params = <String>[];
    if (!check(TokKind.rparen)) {
      params.add(expect(TokKind.ident, 'parameter').text);
      while (match(TokKind.comma)) {
        params.add(expect(TokKind.ident, 'parameter').text);
      }
    }
    expect(TokKind.rparen, "')'");
    return FnDecl(name, params, parseBlock());
  }

  Block parseBlock() {
    expect(TokKind.lbrace, "'{'");
    final stmts = <Stmt>[];
    while (!check(TokKind.rbrace) && !check(TokKind.eof)) {
      stmts.add(parseStmt());
    }
    expect(TokKind.rbrace, "'}'");
    return Block(stmts);
  }

  bool _isAssignOp(TokKind k) =>
      k == TokKind.assign ||
      k == TokKind.plusEq ||
      k == TokKind.minusEq ||
      k == TokKind.starEq;

  Stmt parseStmt() {
    switch (cur.kind) {
      case TokKind.kwVar:
        return parseVar();
      case TokKind.kwIf:
        return parseIf();
      case TokKind.kwFor:
        return parseFor();
      case TokKind.kwWhile:
        return parseWhile();
      case TokKind.kwReturn:
        return parseReturn();
      case TokKind.kwBreak:
        advance();
        return BreakStmt();
      case TokKind.kwContinue:
        advance();
        return ContinueStmt();
      default:
        final e = parseExpr();
        if (_isAssignOp(cur.kind)) {
          final op = advance().text;
          return _makeAssign(e, op, parseExpr());
        }
        return ExprStmt(e);
    }
  }

  Stmt _makeAssign(Expr lvalue, String op, Expr rhs) {
    if (lvalue is VarRef) return AssignStmt(lvalue.name, op, rhs);
    if (lvalue is FieldAccess && lvalue.obj is VarRef) {
      return FieldAssignStmt((lvalue.obj as VarRef).name, lvalue.field, op, rhs);
    }
    throw FormatException(
        'Parse error: invalid assignment target at line ${cur.line}');
  }

  Stmt parseVar() {
    expect(TokKind.kwVar, "'var'");
    final name = expect(TokKind.ident, 'variable name').text;
    expect(TokKind.assign, "'='");
    return VarStmt(name, parseExpr());
  }

  Stmt parseIf() {
    expect(TokKind.kwIf, "'if'");
    expect(TokKind.lparen, "'('");
    final cond = parseExpr();
    expect(TokKind.rparen, "')'");
    final then_ = parseBlock();
    Block? else_;
    if (match(TokKind.kwElse)) {
      // `else if` -> wrap the nested if as the else block
      else_ = check(TokKind.kwIf) ? Block([parseIf()]) : parseBlock();
    }
    return IfStmt(cond, then_, else_);
  }

  Stmt parseFor() {
    expect(TokKind.kwFor, "'for'");
    expect(TokKind.lparen, "'('");
    final varName = expect(TokKind.ident, 'loop variable').text;
    expect(TokKind.kwIn, "'in'");
    final first = parseExpr();
    if (match(TokKind.dotDot)) {
      final end = parseExpr();
      expect(TokKind.rparen, "')'");
      return ForRangeStmt(varName, first, end, parseBlock());
    }
    expect(TokKind.rparen, "')'");
    return ForEachStmt(varName, first, parseBlock());
  }

  Stmt parseWhile() {
    expect(TokKind.kwWhile, "'while'");
    expect(TokKind.lparen, "'('");
    final cond = parseExpr();
    expect(TokKind.rparen, "')'");
    return WhileStmt(cond, parseBlock());
  }

  Stmt parseReturn() {
    expect(TokKind.kwReturn, "'return'");
    if (check(TokKind.rbrace)) return ReturnStmt(null);
    return ReturnStmt(parseExpr());
  }

  Expr parseExpr() => parseOr();

  Expr parseOr() {
    var left = parseAnd();
    while (check(TokKind.pipePipe)) {
      advance();
      left = Binary('||', left, parseAnd());
    }
    return left;
  }

  Expr parseAnd() {
    var left = parseEquality();
    while (check(TokKind.ampAmp)) {
      advance();
      left = Binary('&&', left, parseEquality());
    }
    return left;
  }

  Expr parseEquality() {
    var left = parseRelational();
    while (check(TokKind.eq) || check(TokKind.ne)) {
      final op = advance().text;
      left = Binary(op, left, parseRelational());
    }
    return left;
  }

  Expr parseRelational() {
    var left = parseAdditive();
    while (check(TokKind.lt) ||
        check(TokKind.gt) ||
        check(TokKind.le) ||
        check(TokKind.ge)) {
      final op = advance().text;
      left = Binary(op, left, parseAdditive());
    }
    return left;
  }

  Expr parseAdditive() {
    var left = parseMultiplicative();
    while (check(TokKind.plus) || check(TokKind.minus)) {
      final op = advance().text;
      left = Binary(op, left, parseMultiplicative());
    }
    return left;
  }

  Expr parseMultiplicative() {
    var left = parseUnary();
    while (check(TokKind.star) ||
        check(TokKind.slash) ||
        check(TokKind.percent)) {
      final op = advance().text;
      left = Binary(op, left, parseUnary());
    }
    return left;
  }

  Expr parseUnary() {
    if (check(TokKind.minus)) {
      advance();
      return Unary('-', parseUnary());
    }
    if (check(TokKind.bang)) {
      advance();
      return Unary('!', parseUnary());
    }
    return parsePostfix();
  }

  Expr parsePostfix() {
    var e = parsePrimary();
    while (true) {
      if (match(TokKind.dot)) {
        e = FieldAccess(e, expect(TokKind.ident, 'field name').text);
      } else if (match(TokKind.lbracket)) {
        final idx = parseExpr();
        expect(TokKind.rbracket, "']'");
        e = Index(e, idx);
      } else {
        break;
      }
    }
    return e;
  }

  Expr parsePrimary() {
    if (check(TokKind.intLit)) return IntLit(int.parse(advance().text));
    if (check(TokKind.floatLit)) return FloatLit(double.parse(advance().text));
    if (check(TokKind.kwTrue)) {
      advance();
      return BoolLit(true);
    }
    if (check(TokKind.kwFalse)) {
      advance();
      return BoolLit(false);
    }
    if (check(TokKind.strLit)) return _parseStr(advance().text);
    if (match(TokKind.lparen)) {
      final e = parseExpr();
      expect(TokKind.rparen, "')'");
      return e;
    }
    if (match(TokKind.lbracket)) {
      final elems = <Expr>[];
      if (!check(TokKind.rbracket)) {
        elems.add(parseExpr());
        while (match(TokKind.comma)) {
          elems.add(parseExpr());
        }
      }
      expect(TokKind.rbracket, "']'");
      return ListLit(elems);
    }
    if (check(TokKind.ident)) {
      final name = advance().text;
      if (match(TokKind.lparen)) {
        final args = <Expr>[];
        if (!check(TokKind.rparen)) {
          args.add(parseExpr());
          while (match(TokKind.comma)) {
            args.add(parseExpr());
          }
        }
        expect(TokKind.rparen, "')'");
        return Call(name, args);
      }
      if (check(TokKind.lbrace)) return _parseStructLit(name);
      return VarRef(name);
    }
    throw FormatException(
        'Parse error: unexpected ${cur.kind} "${cur.text}" at line ${cur.line}');
  }

  Expr _parseStructLit(String name) {
    expect(TokKind.lbrace, "'{'");
    final fields = <(String, Expr)>[];
    if (!check(TokKind.rbrace)) {
      do {
        final f = expect(TokKind.ident, 'field name').text;
        expect(TokKind.colon, "':'");
        fields.add((f, parseExpr()));
      } while (match(TokKind.comma));
    }
    expect(TokKind.rbrace, "'}'");
    return StructLit(name, fields);
  }

  Expr _parseStr(String raw) {
    final parts = <StrPart>[];
    final buf = StringBuffer();
    var i = 0;
    while (i < raw.length) {
      if (i + 1 < raw.length && raw[i] == r'$' && raw[i + 1] == '{') {
        if (buf.isNotEmpty) {
          parts.add(StrText(buf.toString()));
          buf.clear();
        }
        final end = raw.indexOf('}', i + 2);
        if (end < 0) throw FormatException('unterminated \${...} in string');
        final sub = Parser(lex(raw.substring(i + 2, end))).parseExpr();
        parts.add(StrExpr(sub));
        i = end + 1;
      } else {
        buf.write(raw[i]);
        i++;
      }
    }
    if (buf.isNotEmpty) parts.add(StrText(buf.toString()));
    return StrLit(parts);
  }
}
