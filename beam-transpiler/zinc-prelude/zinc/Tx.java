package zinc;

import java.util.List;

/** A transaction handle: return commits, throw rolls back. Prelude stub. */
public final class Tx {
  private Tx() {}
  public int exec(String sql, Object... params)         { throw Tag.stub(); }
  public List<Json> query(String sql, Object... params) { throw Tag.stub(); }
}
