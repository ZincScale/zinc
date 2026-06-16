package zinc;

/** A bounded backpressure channel between Actors (NiFi connection / BlockingQueue). The
 *  producer's put blocks when full, the consumer's hasNext blocks until an item is
 *  available or the channel is closed -- so a pipeline runs in parallel AND in bounded
 *  memory. Scanner-style consumer (no null): while (ch.hasNext()) ch.take(). Prelude stub. */
public final class Channel<T> {
  public Channel(int capacity) { throw Tag.stub(); }
  public void put(T item) { throw Tag.stub(); }   // blocks when full -> backpressure
  public void close() { throw Tag.stub(); }        // no more items
  public boolean hasNext() { throw Tag.stub(); }   // blocks until item or closed
  public T take() { throw Tag.stub(); }
}
