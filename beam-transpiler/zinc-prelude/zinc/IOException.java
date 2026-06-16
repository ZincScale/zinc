package zinc;

/** File/IO error (no such file, permission denied, ...). Unchecked (the zinc failure
 *  ladder, rung 1): catch it where you have a plan, else it crashes the process. The
 *  one-level parent for zinc.io failures. Prelude stub. */
public class IOException extends RuntimeException {
  public IOException(String message) { super(message); }
}
