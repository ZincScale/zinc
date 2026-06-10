package zinc;

record Token(TokKind kind, String text, int line) {
  @Override
  public String toString() {
    return kind + "(\"" + text + "\")@" + line;
  }
}
