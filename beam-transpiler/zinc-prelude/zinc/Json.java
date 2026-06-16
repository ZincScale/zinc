package zinc;

import java.util.List;

/** JSON: typed codecs for records (encode/decode/decodeAll) and a dynamic value
 *  (parse + get + asX terminals) for foreign JSON / SQL rows. Prelude stub. */
public final class Json {
  private Json() {}

  // typed codecs
  public static String encode(Object value) { throw Tag.stub(); }
  public static <T> T decode(Class<T> type, String json) { throw Tag.stub(); }
  public static <T> List<T> decodeAll(Class<T> type, List<Json> rows) { throw Tag.stub(); }
  public static <T> List<T> decodeList(Class<T> type, String json) { throw Tag.stub(); }

  // dynamic value
  public static Json parse(String json) { throw Tag.stub(); }
  public Json get(String key) { throw Tag.stub(); }
  public int asInt()       { throw Tag.stub(); }
  public String asText()   { throw Tag.stub(); }
  public boolean asBool()  { throw Tag.stub(); }
  public double asNum()    { throw Tag.stub(); }
}
