package zinc;

/** Fluent route table; handlers are Handler (SAM) so lambdas and classes both work. Prelude stub. */
public final class Router {
  private Router() {}
  public static Router create() { throw Tag.stub(); }
  public Router get(String path, Handler h)    { throw Tag.stub(); }
  public Router post(String path, Handler h)   { throw Tag.stub(); }
  public Router put(String path, Handler h)    { throw Tag.stub(); }
  public Router delete(String path, Handler h) { throw Tag.stub(); }
}
