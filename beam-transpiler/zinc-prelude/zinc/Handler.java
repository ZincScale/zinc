package zinc;

/** HTTP route handler — a SAM so both classes and lambdas serve routes. Prelude stub. */
@FunctionalInterface
public interface Handler {
  Response handle(Request req);
}
