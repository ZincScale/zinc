package zinc;

/** SQL error (constraint violation, bad query). Prelude stub. */
public class SqlException extends RuntimeException {
  public SqlException(String message) { super(message); }
}
