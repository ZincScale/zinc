// FlowFile Queue Throughput Benchmark — Java 25
// Simulates NiFi-style producer/consumer with FlowFiles shuttled through queues.
//
// Tests:
//   Part 1: Read-only queue throughput (naive vs pooled)
//   Part 2: Mutate FlowFile (unpack -> modify -> repack) naive vs pooled
//   Part 3: 3-stage pipeline (source -> A -> B -> C -> sink)
//   Part 4: Cancellation latency (virtual thread interrupt)
//
// Uses: Virtual threads, ArrayBlockingQueue, manual byte[] pool.

import java.nio.ByteBuffer;
import java.nio.charset.StandardCharsets;
import java.util.*;
import java.util.concurrent.*;
import java.util.concurrent.atomic.*;

public class QueueBench {

    // --- FlowFile V3 binary format ---

    static final byte[] MAGIC = "NiFiFF3".getBytes(StandardCharsets.UTF_8);
    static final int MAX_VALUE_2_BYTES = 65535;

    static byte[] packageFlowFile(Map<String, String> attributes, byte[] content) {
        int size = 7 + 2; // magic + attr count
        for (var e : attributes.entrySet()) {
            int kl = e.getKey().getBytes(StandardCharsets.UTF_8).length;
            int vl = e.getValue().getBytes(StandardCharsets.UTF_8).length;
            size += fieldLenSize(kl) + kl + fieldLenSize(vl) + vl;
        }
        size += 8 + content.length;

        byte[] buf = new byte[size];
        int pos = 0;

        System.arraycopy(MAGIC, 0, buf, pos, 7);
        pos += 7;

        pos += writeFieldLength(buf, pos, attributes.size());

        for (var e : attributes.entrySet()) {
            byte[] kb = e.getKey().getBytes(StandardCharsets.UTF_8);
            byte[] vb = e.getValue().getBytes(StandardCharsets.UTF_8);
            pos += writeFieldLength(buf, pos, kb.length);
            System.arraycopy(kb, 0, buf, pos, kb.length);
            pos += kb.length;
            pos += writeFieldLength(buf, pos, vb.length);
            System.arraycopy(vb, 0, buf, pos, vb.length);
            pos += vb.length;
        }

        // content length (big-endian long)
        for (int i = 7; i >= 0; i--)
            buf[pos++] = (byte) ((content.length >> (i * 8)) & 0xFF);

        System.arraycopy(content, 0, buf, pos, content.length);
        return buf;
    }

    record FlowFileRef(Map<String, String> attributes, byte[] data, int contentOffset, int contentLength) {
        byte[] copyContent() {
            byte[] c = new byte[contentLength];
            System.arraycopy(data, contentOffset, c, 0, contentLength);
            return c;
        }
    }

    static FlowFileRef unpackageZeroCopy(byte[] data) {
        int pos = 7; // skip magic
        int count = readFieldLength(data, pos);
        pos += fieldLenBytes(data, pos);

        var attrs = new HashMap<String, String>(count);
        for (int i = 0; i < count; i++) {
            int kl = readFieldLength(data, pos);
            pos += fieldLenBytes(data, pos);
            String key = new String(data, pos, kl, StandardCharsets.UTF_8);
            pos += kl;
            int vl = readFieldLength(data, pos);
            pos += fieldLenBytes(data, pos);
            String val = new String(data, pos, vl, StandardCharsets.UTF_8);
            pos += vl;
            attrs.put(key, val);
        }

        long contentLen = 0;
        for (int i = 0; i < 8; i++)
            contentLen = (contentLen << 8) | (data[pos++] & 0xFF);

        return new FlowFileRef(attrs, data, pos, (int) contentLen);
    }

    static int fieldLenSize(int value) { return value < MAX_VALUE_2_BYTES ? 2 : 6; }

    static int writeFieldLength(byte[] buf, int offset, int value) {
        if (value < MAX_VALUE_2_BYTES) {
            buf[offset] = (byte) ((value >> 8) & 0xFF);
            buf[offset + 1] = (byte) (value & 0xFF);
            return 2;
        }
        buf[offset] = (byte) 0xFF;
        buf[offset + 1] = (byte) 0xFF;
        buf[offset + 2] = (byte) ((value >> 24) & 0xFF);
        buf[offset + 3] = (byte) ((value >> 16) & 0xFF);
        buf[offset + 4] = (byte) ((value >> 8) & 0xFF);
        buf[offset + 5] = (byte) (value & 0xFF);
        return 6;
    }

    static int readFieldLength(byte[] data, int offset) {
        int val = ((data[offset] & 0xFF) << 8) | (data[offset + 1] & 0xFF);
        if (val < MAX_VALUE_2_BYTES) return val;
        return ((data[offset + 2] & 0xFF) << 24) | ((data[offset + 3] & 0xFF) << 16)
             | ((data[offset + 4] & 0xFF) << 8) | (data[offset + 5] & 0xFF);
    }

    static int fieldLenBytes(byte[] data, int offset) {
        int val = ((data[offset] & 0xFF) << 8) | (data[offset + 1] & 0xFF);
        return val < MAX_VALUE_2_BYTES ? 2 : 6;
    }

    // --- Simple byte[] pool (equivalent to .NET ArrayPool) ---

    static class ByteArrayPool {
        // Bucket per size class: 1KB, 2KB, 4KB, ... up to 2MB
        private static final int NUM_BUCKETS = 12; // 1KB to 2MB
        @SuppressWarnings("unchecked")
        private final ConcurrentLinkedDeque<byte[]>[] buckets = new ConcurrentLinkedDeque[NUM_BUCKETS];

        ByteArrayPool() {
            for (int i = 0; i < NUM_BUCKETS; i++)
                buckets[i] = new ConcurrentLinkedDeque<>();
        }

        private int bucketIndex(int minLength) {
            int size = 1024; // start at 1KB
            for (int i = 0; i < NUM_BUCKETS; i++) {
                if (size >= minLength) return i;
                size <<= 1;
            }
            return -1; // too large for pool
        }

        byte[] rent(int minLength) {
            int idx = bucketIndex(minLength);
            if (idx >= 0) {
                byte[] buf = buckets[idx].pollFirst();
                if (buf != null) return buf;
                return new byte[1024 << idx];
            }
            return new byte[minLength]; // too large, just allocate
        }

        void returnBuf(byte[] buf) {
            int idx = bucketIndex(buf.length);
            if (idx >= 0 && buf.length == (1024 << idx)) {
                buckets[idx].offerFirst(buf);
            }
        }
    }

    static final ByteArrayPool POOL = new ByteArrayPool();

    // --- OwnedBuffer (equivalent to .NET OwnedBuffer struct) ---

    record OwnedBuffer(byte[] array, int length) {
        void returnToPool() { POOL.returnBuf(array); }
    }

    // --- Packaging with pooled output ---

    static OwnedBuffer packageToOwned(Map<String, String> attributes, byte[] content, int contentOffset, int contentLength) {
        int size = 7 + 2;
        for (var e : attributes.entrySet()) {
            int kl = e.getKey().getBytes(StandardCharsets.UTF_8).length;
            int vl = e.getValue().getBytes(StandardCharsets.UTF_8).length;
            size += fieldLenSize(kl) + kl + fieldLenSize(vl) + vl;
        }
        size += 8 + contentLength;

        byte[] buf = POOL.rent(size);
        int pos = 0;

        System.arraycopy(MAGIC, 0, buf, pos, 7);
        pos += 7;
        pos += writeFieldLength(buf, pos, attributes.size());

        for (var e : attributes.entrySet()) {
            byte[] kb = e.getKey().getBytes(StandardCharsets.UTF_8);
            byte[] vb = e.getValue().getBytes(StandardCharsets.UTF_8);
            pos += writeFieldLength(buf, pos, kb.length);
            System.arraycopy(kb, 0, buf, pos, kb.length);
            pos += kb.length;
            pos += writeFieldLength(buf, pos, vb.length);
            System.arraycopy(vb, 0, buf, pos, vb.length);
            pos += vb.length;
        }

        for (int i = 7; i >= 0; i--)
            buf[pos++] = (byte) ((contentLength >> (i * 8)) & 0xFF);

        System.arraycopy(content, contentOffset, buf, pos, contentLength);
        pos += contentLength;

        return new OwnedBuffer(buf, pos);
    }

    // --- Benchmark helpers ---

    static final Random RNG = new Random(42);

    static byte[] makeFlowFile(int size, int index) {
        var attrs = new LinkedHashMap<String, String>();
        attrs.put("filename", "flowfile_" + index + ".dat");
        attrs.put("uuid", String.format("aaaaaaaa-bbbb-cccc-dddd-%012d", index));
        attrs.put("mime.type", "application/octet-stream");
        attrs.put("path", "/data/input/");
        byte[] content = new byte[size];
        RNG.nextBytes(content);
        return packageFlowFile(attrs, content);
    }

    record BenchResult(String test, int count, double elapsedSec, long msgsPerSec, double mbPerSec,
                       double cancelLatencyUs, int gcCount, long allocMb) {}

    static void printResult(BenchResult r) {
        String gc = r.gcCount > 0 ? String.format("  GC:%d alloc:%dMB", r.gcCount, r.allocMb) : "";
        System.out.printf("  %-40s %,10d msgs/s  %8.1f MB/s  (%.3fs)%s%n",
            r.test, r.msgsPerSec, r.mbPerSec, r.elapsedSec, gc);
    }

    // =========================================================================
    // Part 1: Read-only queue throughput
    // =========================================================================

    static BenchResult benchQueueNaive(byte[][] flowfiles, int count, String label) throws Exception {
        var queue = new ArrayBlockingQueue<byte[]>(1000);
        var totalBytes = new AtomicLong();
        var processed = new AtomicInteger();
        var done = new CountDownLatch(1);

        Thread.startVirtualThread(() -> {
            try {
                while (true) {
                    byte[] item = queue.take();
                    if (item.length == 0) break; // sentinel
                    var ff = unpackageZeroCopy(item);
                    totalBytes.addAndGet(ff.contentLength());
                    processed.incrementAndGet();
                }
            } catch (InterruptedException e) { Thread.currentThread().interrupt(); }
            done.countDown();
        });

        long start = System.nanoTime();
        for (int i = 0; i < count; i++)
            queue.put(flowfiles[i % flowfiles.length]);
        queue.put(new byte[0]); // sentinel
        done.await();
        double elapsed = (System.nanoTime() - start) / 1e9;

        return new BenchResult("queue-naive-" + label, processed.get(), Math.round(elapsed * 1000.0) / 1000.0,
            (long)(processed.get() / elapsed),
            totalBytes.get() / elapsed / (1024.0 * 1024.0), 0, 0, 0);
    }

    static BenchResult benchQueueFanout(byte[][] flowfiles, int count, int numConsumers, String label) throws Exception {
        var queue = new ArrayBlockingQueue<byte[]>(2000);
        var totalBytes = new AtomicLong();
        var processed = new AtomicInteger();
        var done = new CountDownLatch(numConsumers);

        for (int c = 0; c < numConsumers; c++) {
            Thread.startVirtualThread(() -> {
                try {
                    while (true) {
                        byte[] item = queue.take();
                        if (item.length == 0) { queue.put(item); break; } // re-post sentinel
                        var ff = unpackageZeroCopy(item);
                        totalBytes.addAndGet(ff.contentLength());
                        processed.incrementAndGet();
                    }
                } catch (InterruptedException e) { Thread.currentThread().interrupt(); }
                done.countDown();
            });
        }

        long start = System.nanoTime();
        for (int i = 0; i < count; i++)
            queue.put(flowfiles[i % flowfiles.length]);
        queue.put(new byte[0]);
        done.await();
        double elapsed = (System.nanoTime() - start) / 1e9;

        return new BenchResult("queue-fanout-" + numConsumers + "c-" + label, processed.get(),
            Math.round(elapsed * 1000.0) / 1000.0, (long)(processed.get() / elapsed),
            totalBytes.get() / elapsed / (1024.0 * 1024.0), 0, 0, 0);
    }

    // =========================================================================
    // Part 2: Mutate FlowFile
    // =========================================================================

    static byte[] mutateNaive(byte[] packed) {
        var ff = unpackageZeroCopy(packed);
        var attrs = ff.attributes();
        attrs.put("processed_by", "enrich_v2");
        attrs.put("hop_count", String.valueOf(Integer.parseInt(attrs.getOrDefault("hop_count", "0")) + 1));
        byte[] content = ff.copyContent();
        for (int i = 0; i < Math.min(64, content.length); i++)
            content[i] ^= (byte) 0xAA;
        return packageFlowFile(attrs, content);
    }

    static OwnedBuffer mutateToOwned(byte[] packed) {
        var ff = unpackageZeroCopy(packed);
        var attrs = ff.attributes();
        attrs.put("processed_by", "enrich_v2");
        attrs.put("hop_count", String.valueOf(Integer.parseInt(attrs.getOrDefault("hop_count", "0")) + 1));

        int len = ff.contentLength();
        byte[] work = POOL.rent(len);
        System.arraycopy(ff.data(), ff.contentOffset(), work, 0, len);
        for (int i = 0; i < Math.min(64, len); i++)
            work[i] ^= (byte) 0xAA;

        var owned = packageToOwned(attrs, work, 0, len);
        POOL.returnBuf(work);
        return owned;
    }

    static OwnedBuffer mutateOwnedToOwned(OwnedBuffer incoming, String processorName) {
        // Zero-copy unpackage over the owned buffer
        var ff = unpackageZeroCopy(incoming.array());
        var attrs = ff.attributes();
        attrs.put("processed_by", processorName);
        attrs.put("hop_count", String.valueOf(Integer.parseInt(attrs.getOrDefault("hop_count", "0")) + 1));

        int len = ff.contentLength();
        byte[] work = POOL.rent(len);
        System.arraycopy(ff.data(), ff.contentOffset(), work, 0, len);
        for (int i = 0; i < Math.min(64, len); i++)
            work[i] ^= (byte) 0xAA;

        var owned = packageToOwned(attrs, work, 0, len);
        POOL.returnBuf(work);
        incoming.returnToPool();
        return owned;
    }

    static BenchResult benchMutateNaive(byte[][] flowfiles, int count, String label) throws Exception {
        var inQ = new ArrayBlockingQueue<byte[]>(1000);
        var outQ = new ArrayBlockingQueue<byte[]>(1000);
        var totalBytes = new AtomicLong();
        var processed = new AtomicInteger();
        var done = new CountDownLatch(2);

        // Processor
        Thread.startVirtualThread(() -> {
            try {
                while (true) {
                    byte[] item = inQ.take();
                    if (item.length == 0) { outQ.put(item); break; }
                    outQ.put(mutateNaive(item));
                    totalBytes.addAndGet(item.length);
                    processed.incrementAndGet();
                }
            } catch (InterruptedException e) { Thread.currentThread().interrupt(); }
            done.countDown();
        });

        // Sink
        Thread.startVirtualThread(() -> {
            try {
                while (true) {
                    byte[] item = outQ.take();
                    if (item.length == 0) break;
                }
            } catch (InterruptedException e) { Thread.currentThread().interrupt(); }
            done.countDown();
        });

        long start = System.nanoTime();
        for (int i = 0; i < count; i++)
            inQ.put(flowfiles[i % flowfiles.length]);
        inQ.put(new byte[0]);
        done.await();
        double elapsed = (System.nanoTime() - start) / 1e9;

        return new BenchResult("mutate-naive-" + label, processed.get(),
            Math.round(elapsed * 1000.0) / 1000.0, (long)(processed.get() / elapsed),
            totalBytes.get() / elapsed / (1024.0 * 1024.0), 0, 0, 0);
    }

    static BenchResult benchMutatePooled(byte[][] flowfiles, int count, String label) throws Exception {
        var inQ = new ArrayBlockingQueue<byte[]>(1000);
        var outQ = new ArrayBlockingQueue<OwnedBuffer>(1000);
        var totalBytes = new AtomicLong();
        var processed = new AtomicInteger();
        var done = new CountDownLatch(2);

        Thread.startVirtualThread(() -> {
            try {
                while (true) {
                    byte[] item = inQ.take();
                    if (item.length == 0) break;
                    outQ.put(mutateToOwned(item));
                    totalBytes.addAndGet(item.length);
                    processed.incrementAndGet();
                }
            } catch (InterruptedException e) { Thread.currentThread().interrupt(); }
            try { outQ.put(new OwnedBuffer(new byte[0], 0)); } catch (InterruptedException e) {} // sentinel
            done.countDown();
        });

        Thread.startVirtualThread(() -> {
            try {
                while (true) {
                    var owned = outQ.take();
                    if (owned.length() == 0) break;
                    owned.returnToPool();
                }
            } catch (InterruptedException e) { Thread.currentThread().interrupt(); }
            done.countDown();
        });

        long start = System.nanoTime();
        for (int i = 0; i < count; i++)
            inQ.put(flowfiles[i % flowfiles.length]);
        inQ.put(new byte[0]);
        done.await();
        double elapsed = (System.nanoTime() - start) / 1e9;

        return new BenchResult("mutate-pooled-" + label, processed.get(),
            Math.round(elapsed * 1000.0) / 1000.0, (long)(processed.get() / elapsed),
            totalBytes.get() / elapsed / (1024.0 * 1024.0), 0, 0, 0);
    }

    // =========================================================================
    // Part 3: 3-Stage Pipeline
    // =========================================================================

    static void startNaiveStage(ArrayBlockingQueue<byte[]> in_, ArrayBlockingQueue<byte[]> out_, CountDownLatch done) {
        Thread.startVirtualThread(() -> {
            try {
                while (true) {
                    byte[] item = in_.take();
                    if (item.length == 0) { out_.put(item); break; }
                    out_.put(mutateNaive(item));
                }
            } catch (InterruptedException e) { Thread.currentThread().interrupt(); }
            done.countDown();
        });
    }

    static void startPooledStage(ArrayBlockingQueue<OwnedBuffer> in_, ArrayBlockingQueue<OwnedBuffer> out_, String name, CountDownLatch done) {
        Thread.startVirtualThread(() -> {
            try {
                while (true) {
                    var item = in_.take();
                    if (item.length() == 0) { out_.put(item); break; }
                    out_.put(mutateOwnedToOwned(item, name));
                }
            } catch (InterruptedException e) { Thread.currentThread().interrupt(); }
            done.countDown();
        });
    }

    static BenchResult benchPipelineNaive3Stage(byte[][] flowfiles, int count, String label) throws Exception {
        var q0 = new ArrayBlockingQueue<byte[]>(1000);
        var q1 = new ArrayBlockingQueue<byte[]>(1000);
        var q2 = new ArrayBlockingQueue<byte[]>(1000);
        var q3 = new ArrayBlockingQueue<byte[]>(1000);
        var totalBytes = new AtomicLong();
        var processed = new AtomicInteger();
        var done = new CountDownLatch(4);

        startNaiveStage(q0, q1, done);
        startNaiveStage(q1, q2, done);
        startNaiveStage(q2, q3, done);

        // Sink
        Thread.startVirtualThread(() -> {
            try {
                while (true) {
                    byte[] item = q3.take();
                    if (item.length == 0) break;
                    totalBytes.addAndGet(item.length);
                    processed.incrementAndGet();
                }
            } catch (InterruptedException e) { Thread.currentThread().interrupt(); }
            done.countDown();
        });

        long start = System.nanoTime();
        for (int i = 0; i < count; i++)
            q0.put(flowfiles[i % flowfiles.length]);
        q0.put(new byte[0]);
        done.await();
        double elapsed = (System.nanoTime() - start) / 1e9;

        return new BenchResult("pipeline-naive-3stage-" + label, processed.get(),
            Math.round(elapsed * 1000.0) / 1000.0, (long)(processed.get() / elapsed),
            totalBytes.get() / elapsed / (1024.0 * 1024.0), 0, 0, 0);
    }

    static BenchResult benchPipelinePooled3Stage(byte[][] flowfiles, int count, String label) throws Exception {
        var q0 = new ArrayBlockingQueue<OwnedBuffer>(1000);
        var q1 = new ArrayBlockingQueue<OwnedBuffer>(1000);
        var q2 = new ArrayBlockingQueue<OwnedBuffer>(1000);
        var q3 = new ArrayBlockingQueue<OwnedBuffer>(1000);
        var totalBytes = new AtomicLong();
        var processed = new AtomicInteger();
        var done = new CountDownLatch(4);
        var SENTINEL = new OwnedBuffer(new byte[0], 0);

        startPooledStage(q0, q1, "procA", done);
        startPooledStage(q1, q2, "procB", done);
        startPooledStage(q2, q3, "procC", done);

        Thread.startVirtualThread(() -> {
            try {
                while (true) {
                    var item = q3.take();
                    if (item.length() == 0) break;
                    totalBytes.addAndGet(item.length());
                    processed.incrementAndGet();
                    item.returnToPool();
                }
            } catch (InterruptedException e) { Thread.currentThread().interrupt(); }
            done.countDown();
        });

        long start = System.nanoTime();
        for (int i = 0; i < count; i++) {
            var owned = mutateToOwned(flowfiles[i % flowfiles.length]);
            q0.put(owned);
        }
        q0.put(SENTINEL);
        done.await();
        double elapsed = (System.nanoTime() - start) / 1e9;

        return new BenchResult("pipeline-pooled-3stage-" + label, processed.get(),
            Math.round(elapsed * 1000.0) / 1000.0, (long)(processed.get() / elapsed),
            totalBytes.get() / elapsed / (1024.0 * 1024.0), 0, 0, 0);
    }

    // =========================================================================
    // Part 4: Cancellation latency (virtual thread interrupt)
    // =========================================================================

    static BenchResult benchCancellation(byte[][] flowfiles, String label, int cancelAfterMs) throws Exception {
        var queue = new ArrayBlockingQueue<byte[]>(5000);
        var totalBytes = new AtomicLong();
        var processed = new AtomicInteger();

        var consumer = Thread.startVirtualThread(() -> {
            try {
                while (!Thread.currentThread().isInterrupted()) {
                    byte[] item = queue.take();
                    var ff = unpackageZeroCopy(item);
                    totalBytes.addAndGet(ff.contentLength());
                    processed.incrementAndGet();
                }
            } catch (InterruptedException e) { /* expected */ }
        });

        var producer = Thread.startVirtualThread(() -> {
            try {
                int i = 0;
                while (!Thread.currentThread().isInterrupted()) {
                    queue.put(flowfiles[i % flowfiles.length]);
                    i++;
                }
            } catch (InterruptedException e) { /* expected */ }
        });

        long startNs = System.nanoTime();
        Thread.sleep(cancelAfterMs);
        long cancelStart = System.nanoTime();
        producer.interrupt();
        consumer.interrupt();
        producer.join();
        consumer.join();
        long cancelEnd = System.nanoTime();
        double elapsed = (cancelEnd - startNs) / 1e9;
        double cancelUs = (cancelEnd - cancelStart) / 1000.0;

        return new BenchResult("cancel-" + label + "-after" + cancelAfterMs + "ms",
            processed.get(), Math.round(elapsed * 1000.0) / 1000.0,
            (long)(processed.get() / elapsed),
            totalBytes.get() / elapsed / (1024.0 * 1024.0),
            cancelUs, 0, 0);
    }

    // =========================================================================
    // Runner
    // =========================================================================

    public static void main(String[] args) throws Exception {
        System.out.printf("Java %s (%s)%n", System.getProperty("java.version"), System.getProperty("java.vm.name"));
        System.out.printf("CPUs: %d%n", Runtime.getRuntime().availableProcessors());
        System.out.println("=".repeat(80));

        var sizes = new Object[][] {
            {"1KB", 1024}, {"10KB", 10*1024}, {"100KB", 100*1024}, {"1MB", 1024*1024}
        };
        var counts = Map.of("1KB", 100_000, "10KB", 50_000, "100KB", 10_000, "1MB", 2_000);

        // --- Part 1: Read-only queue throughput ---
        System.out.println("\n### PART 1: Queue Throughput ###\n");
        for (var s : sizes) {
            String label = (String) s[0];
            int sizeBytes = (int) s[1];
            int count = counts.get(label);
            System.out.printf("--- FlowFile size: %s | count: %,d ---%n", label, count);

            int poolSize = Math.min(count, 100);
            byte[][] ffs = new byte[poolSize][];
            for (int i = 0; i < poolSize; i++) ffs[i] = makeFlowFile(sizeBytes, i);

            printResult(benchQueueNaive(ffs, count, label));
            printResult(benchQueueFanout(ffs, count, 4, label));
            System.out.println();
        }

        // --- Part 2: Mutate ---
        System.out.println("\n### PART 2: Mutate FlowFile ###\n");

        // Validation
        System.out.println("--- Validation (10 items, 1KB) ---");
        byte[][] valFfs = new byte[10][];
        for (int i = 0; i < 10; i++) valFfs[i] = makeFlowFile(1024, i);
        var valR = benchMutatePooled(valFfs, 10, "validate");
        System.out.printf("  mutate-validate: %d processed (expected 10)%n", valR.count());

        var orig = valFfs[0];
        var mutated = mutateToOwned(orig);
        var mutFf = unpackageZeroCopy(mutated.array());
        assert mutFf.attributes().get("processed_by").equals("enrich_v2") : "Attr mutation failed";
        assert mutFf.attributes().get("hop_count").equals("1") : "Hop count failed";
        mutated.returnToPool();
        System.out.println("  Validation PASSED");
        System.out.println();

        for (var s : sizes) {
            String label = (String) s[0];
            int sizeBytes = (int) s[1];
            int count = counts.get(label);
            System.out.printf("--- Mutate FlowFile size: %s | count: %,d ---%n", label, count);

            int poolSize = Math.min(count, 100);
            byte[][] ffs = new byte[poolSize][];
            for (int i = 0; i < poolSize; i++) ffs[i] = makeFlowFile(sizeBytes, i);

            printResult(benchMutateNaive(ffs, count, label));
            printResult(benchMutatePooled(ffs, count, label));
            System.out.println();
        }

        // --- Part 3: 3-Stage Pipeline ---
        System.out.println("\n### PART 3: 3-Stage Pipeline ###\n");

        System.out.println("--- Validation (10 items, 1KB) ---");
        var pipeR = benchPipelinePooled3Stage(valFfs, 10, "validate");
        System.out.printf("  pipeline-validate: %d processed (expected 10)%n", pipeR.count());
        System.out.println();

        for (var s : sizes) {
            String label = (String) s[0];
            int sizeBytes = (int) s[1];
            int count = counts.get(label);
            System.out.printf("--- Pipeline FlowFile size: %s | count: %,d ---%n", label, count);

            int poolSize = Math.min(count, 100);
            byte[][] ffs = new byte[poolSize][];
            for (int i = 0; i < poolSize; i++) ffs[i] = makeFlowFile(sizeBytes, i);

            printResult(benchPipelineNaive3Stage(ffs, count, label));
            printResult(benchPipelinePooled3Stage(ffs, count, label));
            System.out.println();
        }

        // --- Part 4: Cancellation ---
        System.out.println("\n### PART 4: Cancellation Latency ###\n");
        for (var s : new Object[][] {{"100KB", 100*1024}, {"1MB", 1024*1024}}) {
            String label = (String) s[0];
            int sizeBytes = (int) s[1];
            byte[][] ffs = new byte[100][];
            for (int i = 0; i < 100; i++) ffs[i] = makeFlowFile(sizeBytes, i);

            var r = benchCancellation(ffs, label, 500);
            System.out.printf("  %-45s %,10d msgs/s  cancel: %8.1f us%n", r.test(), r.msgsPerSec(), r.cancelLatencyUs());
        }
    }
}
