enum TokKind {
  ident, intLit, floatLit, strLit,
  kwFn, kwVar, kwFor, kwIn, kwWhile, kwIf, kwElse, kwReturn, kwStruct,
  kwTrue, kwFalse, kwBreak, kwContinue,
  lparen, rparen, lbrace, rbrace, lbracket, rbracket, comma, dot, colon,
  assign, plusEq, minusEq, starEq,
  plus, minus, star, slash, percent, bang,
  ampAmp, pipePipe,
  eq, ne, lt, gt, le, ge,
  dotDot,
  eof,
}

class Token {
  final TokKind kind;
  final String text;
  final int line;
  Token(this.kind, this.text, this.line);
  @override
  String toString() => '$kind("$text")@$line';
}
