package zinc;

import java.util.List;
import java.util.function.Function;

/** SQL pool as a supervision subtree. Rows come back as dynamic Json values;
 *  map them to records with Json.decodeAll(T.class, rows). Prelude stub. */
public final class Db {
  public Db(String url, int connections) { throw Tag.stub(); }
  public int exec(String sql, Object... params)         { throw Tag.stub(); }
  public List<Json> query(String sql, Object... params) { throw Tag.stub(); }
  public <T> T transaction(Function<Tx, T> body)        { throw Tag.stub(); }
}
