import 'token.dart';

const _keywords = {
  'fn': TokKind.kwFn,
  'var': TokKind.kwVar,
  'for': TokKind.kwFor,
  'in': TokKind.kwIn,
  'while': TokKind.kwWhile,
  'if': TokKind.kwIf,
  'else': TokKind.kwElse,
  'return': TokKind.kwReturn,
  'struct': TokKind.kwStruct,
  'true': TokKind.kwTrue,
  'false': TokKind.kwFalse,
  'break': TokKind.kwBreak,
  'continue': TokKind.kwContinue,
};

const _two = {
  '&&': TokKind.ampAmp,
  '||': TokKind.pipePipe,
  '==': TokKind.eq,
  '!=': TokKind.ne,
  '<=': TokKind.le,
  '>=': TokKind.ge,
  '+=': TokKind.plusEq,
  '-=': TokKind.minusEq,
  '*=': TokKind.starEq,
  '..': TokKind.dotDot,
};

const _one = {
  '(': TokKind.lparen,
  ')': TokKind.rparen,
  '{': TokKind.lbrace,
  '}': TokKind.rbrace,
  '[': TokKind.lbracket,
  ']': TokKind.rbracket,
  ',': TokKind.comma,
  '.': TokKind.dot,
  ':': TokKind.colon,
  '=': TokKind.assign,
  '+': TokKind.plus,
  '-': TokKind.minus,
  '*': TokKind.star,
  '/': TokKind.slash,
  '%': TokKind.percent,
  '!': TokKind.bang,
  '<': TokKind.lt,
  '>': TokKind.gt,
};

List<Token> lex(String src) {
  final toks = <Token>[];
  var i = 0;
  var line = 1;
  final n = src.length;

  bool isDigit(int c) => c >= 0x30 && c <= 0x39;
  bool isAlpha(int c) =>
      (c >= 0x41 && c <= 0x5A) || (c >= 0x61 && c <= 0x7A) || c == 0x5F;
  bool isAlnum(int c) => isAlpha(c) || isDigit(c);

  while (i < n) {
    final c = src.codeUnitAt(i);
    if (c == 0x0A) {
      line++;
      i++;
      continue;
    }
    if (c == 0x20 || c == 0x09 || c == 0x0D) {
      i++;
      continue;
    }
    if (c == 0x2F && i + 1 < n && src.codeUnitAt(i + 1) == 0x2F) {
      while (i < n && src.codeUnitAt(i) != 0x0A) {
        i++;
      }
      continue;
    }
    // string literal "..." (raw content kept; interpolation parsed later)
    if (c == 0x22) {
      final start = i + 1;
      i++;
      while (i < n && src.codeUnitAt(i) != 0x22) {
        if (src.codeUnitAt(i) == 0x0A) line++;
        i++;
      }
      if (i >= n) throw FormatException('unterminated string at line $line');
      toks.add(Token(TokKind.strLit, src.substring(start, i), line));
      i++; // closing quote
      continue;
    }
    if (isAlpha(c)) {
      final s = i;
      while (i < n && isAlnum(src.codeUnitAt(i))) {
        i++;
      }
      final t = src.substring(s, i);
      toks.add(Token(_keywords[t] ?? TokKind.ident, t, line));
      continue;
    }
    if (isDigit(c)) {
      final s = i;
      while (i < n && isDigit(src.codeUnitAt(i))) {
        i++;
      }
      // float: '.' followed by a digit (not '..' range, not '.field')
      if (i + 1 < n &&
          src.codeUnitAt(i) == 0x2E &&
          isDigit(src.codeUnitAt(i + 1))) {
        i++;
        while (i < n && isDigit(src.codeUnitAt(i))) {
          i++;
        }
        toks.add(Token(TokKind.floatLit, src.substring(s, i), line));
      } else {
        toks.add(Token(TokKind.intLit, src.substring(s, i), line));
      }
      continue;
    }
    if (i + 1 < n) {
      final two = src.substring(i, i + 2);
      final k2 = _two[two];
      if (k2 != null) {
        toks.add(Token(k2, two, line));
        i += 2;
        continue;
      }
    }
    final one = src[i];
    final k1 = _one[one];
    if (k1 != null) {
      toks.add(Token(k1, one, line));
      i++;
      continue;
    }
    throw FormatException('Lex error: unexpected "$one" at line $line');
  }
  toks.add(Token(TokKind.eof, '', line));
  return toks;
}
