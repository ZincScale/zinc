package zinc;

class CompileError extends RuntimeException {
  private static final long serialVersionUID = 1L;

  CompileError(String message) {
    super(message);
  }
}
