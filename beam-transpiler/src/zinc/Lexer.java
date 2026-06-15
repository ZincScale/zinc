package zinc;

import java.util.ArrayList;
import java.util.List;
import java.util.Map;

final class Lexer {
  private Lexer() {}

  private static final Map<String, TokKind> KEYWORDS = Map.ofEntries(
      Map.entry("import", TokKind.KW_IMPORT),
      Map.entry("var", TokKind.KW_VAR),
      Map.entry("for", TokKind.KW_FOR),
      Map.entry("while", TokKind.KW_WHILE),
      Map.entry("if", TokKind.KW_IF),
      Map.entry("else", TokKind.KW_ELSE),
      Map.entry("return", TokKind.KW_RETURN),
      Map.entry("true", TokKind.KW_TRUE),
      Map.entry("false", TokKind.KW_FALSE),
      Map.entry("break", TokKind.KW_BREAK),
      Map.entry("continue", TokKind.KW_CONTINUE));

  private static final Map<String, TokKind> TWO = Map.ofEntries(
      Map.entry("&&", TokKind.AMP_AMP),
      Map.entry("||", TokKind.PIPE_PIPE),
      Map.entry("==", TokKind.EQ),
      Map.entry("!=", TokKind.NE),
      Map.entry("<=", TokKind.LE),
      Map.entry(">=", TokKind.GE),
      Map.entry("+=", TokKind.PLUS_EQ),
      Map.entry("-=", TokKind.MINUS_EQ),
      Map.entry("*=", TokKind.STAR_EQ),
      Map.entry("++", TokKind.PLUS_PLUS),
      Map.entry("--", TokKind.MINUS_MINUS),
      Map.entry("->", TokKind.ARROW));

  private static final Map<String, TokKind> ONE = Map.ofEntries(
      Map.entry("(", TokKind.LPAREN),
      Map.entry(")", TokKind.RPAREN),
      Map.entry("{", TokKind.LBRACE),
      Map.entry("}", TokKind.RBRACE),
      Map.entry("[", TokKind.LBRACKET),
      Map.entry("]", TokKind.RBRACKET),
      Map.entry(",", TokKind.COMMA),
      Map.entry(".", TokKind.DOT),
      Map.entry(":", TokKind.COLON),
      Map.entry(";", TokKind.SEMI),
      Map.entry("?", TokKind.QUESTION),
      Map.entry("=", TokKind.ASSIGN),
      Map.entry("+", TokKind.PLUS),
      Map.entry("-", TokKind.MINUS),
      Map.entry("*", TokKind.STAR),
      Map.entry("/", TokKind.SLASH),
      Map.entry("%", TokKind.PERCENT),
      Map.entry("!", TokKind.BANG),
      Map.entry("<", TokKind.LT),
      Map.entry(">", TokKind.GT));

  static List<Token> lex(String src) {
    var toks = new ArrayList<Token>();
    int i = 0;
    int line = 1;
    final int n = src.length();

    while (i < n) {
      char c = src.charAt(i);
      if (c == '\n') {
        line++;
        i++;
        continue;
      }
      if (c == ' ' || c == '\t' || c == '\r') {
        i++;
        continue;
      }
      if (c == '/' && i + 1 < n && src.charAt(i + 1) == '/') {
        while (i < n && src.charAt(i) != '\n') {
          i++;
        }
        continue;
      }
      // string literal with Java escape sequences decoded here; re-encoded at emit
      if (c == '"') {
        var buf = new StringBuilder();
        i++;
        while (i < n && src.charAt(i) != '"') {
          char ch = src.charAt(i);
          if (ch == '\n') line++;
          if (ch == '\\') {
            if (i + 1 >= n) throw new CompileError("unterminated string at line " + line);
            char e = src.charAt(i + 1);
            buf.append(switch (e) {
              case 'n' -> '\n';
              case 't' -> '\t';
              case 'r' -> '\r';
              case 'b' -> '\b';
              case 'f' -> '\f';
              case '0' -> '\0';
              case '"' -> '"';
              case '\'' -> '\'';
              case '\\' -> '\\';
              default -> throw new CompileError("unknown escape \\" + e + " at line " + line);
            });
            i += 2;
            continue;
          }
          buf.append(ch);
          i++;
        }
        if (i >= n) throw new CompileError("unterminated string at line " + line);
        toks.add(new Token(TokKind.STR_LIT, buf.toString(), line));
        i++; // closing quote
        continue;
      }
      if (isAlpha(c)) {
        int s = i;
        while (i < n && isAlnum(src.charAt(i))) {
          i++;
        }
        String t = src.substring(s, i);
        toks.add(new Token(KEYWORDS.getOrDefault(t, TokKind.IDENT), t, line));
        continue;
      }
      if (isDigit(c)) {
        int s = i;
        while (i < n && isDigit(src.charAt(i))) {
          i++;
        }
        // float: '.' followed by a digit (not '..' range, not '.field')
        if (i + 1 < n && src.charAt(i) == '.' && isDigit(src.charAt(i + 1))) {
          i++;
          while (i < n && isDigit(src.charAt(i))) {
            i++;
          }
          toks.add(new Token(TokKind.FLOAT_LIT, src.substring(s, i), line));
        } else {
          toks.add(new Token(TokKind.INT_LIT, src.substring(s, i), line));
        }
        continue;
      }
      if (i + 1 < n) {
        String two = src.substring(i, i + 2);
        TokKind k2 = TWO.get(two);
        if (k2 != null) {
          toks.add(new Token(k2, two, line));
          i += 2;
          continue;
        }
      }
      String one = String.valueOf(c);
      TokKind k1 = ONE.get(one);
      if (k1 != null) {
        toks.add(new Token(k1, one, line));
        i++;
        continue;
      }
      throw new CompileError("Lex error: unexpected \"" + one + "\" at line " + line);
    }
    toks.add(new Token(TokKind.EOF, "", line));
    return toks;
  }

  private static boolean isDigit(char c) {
    return c >= '0' && c <= '9';
  }

  private static boolean isAlpha(char c) {
    return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '_';
  }

  private static boolean isAlnum(char c) {
    return isAlpha(c) || isDigit(c);
  }
}
