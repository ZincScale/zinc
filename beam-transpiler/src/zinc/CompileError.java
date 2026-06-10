package zinc;

class CompileError extends RuntimeException {
  CompileError(String message) {
    super(message);
  }
}
