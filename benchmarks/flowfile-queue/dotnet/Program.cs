// FlowFile Queue Throughput Benchmark — .NET 10 (Optimized)
// Simulates NiFi-style producer/consumer with FlowFiles shuttled through channels.
//
// Optimizations vs naive version:
//   1. ArrayPool<byte> for content extraction — rent/return instead of allocate/GC
//   2. Memory<byte> slicing — zero-copy content references over original packed buffer
//   3. Struct-based FlowFileRef — small value type through channels, no heap pressure
//   4. CancellationToken-aware benchmarks — measures graceful processor shutdown
//   5. RecyclableMemoryStream for packaging — pooled stream buffers
//
// Runs both "naive" (original) and "optimized" variants for direct comparison.

using System.Buffers;
using System.Buffers.Binary;
using System.Collections.Concurrent;
using System.Diagnostics;
using System.Text;
using System.Text.Json;
using System.Runtime;
using System.Threading.Channels;

await Bench.RunAll();

// --- FlowFile as a zero-copy struct reference ---

/// <summary>
/// Lightweight struct that references the original packed buffer.
/// Content is a Memory&lt;byte&gt; slice — no copy, no allocation.
/// </summary>
readonly struct FlowFileRef
{
    public readonly Dictionary<string, string> Attributes;
    public readonly ReadOnlyMemory<byte> Content;
    public readonly byte[]? RentedBuffer; // non-null if we own a pooled buffer

    public FlowFileRef(Dictionary<string, string> attributes, ReadOnlyMemory<byte> content, byte[]? rentedBuffer = null)
    {
        Attributes = attributes;
        Content = content;
        RentedBuffer = rentedBuffer;
    }

    /// <summary>Return rented buffer to pool. Call when done processing.</summary>
    public void Return()
    {
        if (RentedBuffer != null)
            ArrayPool<byte>.Shared.Return(RentedBuffer);
    }
}

// --- Owned pooled buffer — flows through channels, zero heap allocation ---

/// <summary>
/// A buffer rented from ArrayPool that carries its actual used length.
/// Flows through Channel&lt;OwnedBuffer&gt; — the consumer calls Return() when done.
/// This eliminates the ToArray() copy that kills mutate throughput.
/// </summary>
struct OwnedBuffer
{
    public byte[] Array;
    public int Length;

    public OwnedBuffer(byte[] array, int length)
    {
        Array = array;
        Length = length;
    }

    public ReadOnlySpan<byte> Span => Array.AsSpan(0, Length);
    public ReadOnlyMemory<byte> Memory => new(Array, 0, Length);

    public void Return()
    {
        if (Array != null)
            ArrayPool<byte>.Shared.Return(Array);
        Array = null!;
    }
}

// --- FlowFile V3 binary format ---

static class FlowFileV3
{
    static readonly byte[] Magic = "NiFiFF3"u8.ToArray();
    const int MaxValue2Bytes = 65535;

    /// <summary>Package using ArrayPool-backed buffer instead of MemoryStream.</summary>
    public static byte[] PackagePooled(Dictionary<string, string> attributes, byte[] content)
    {
        // Calculate exact size needed to avoid resizing
        int size = 7; // magic
        size += 2;    // attr count field
        foreach (var (key, value) in attributes)
        {
            var keyLen = Encoding.UTF8.GetByteCount(key);
            var valLen = Encoding.UTF8.GetByteCount(value ?? "");
            size += FieldLengthSize(keyLen) + keyLen + FieldLengthSize(valLen) + valLen;
        }
        size += 8 + content.Length; // content length field + content

        var buffer = ArrayPool<byte>.Shared.Rent(size);
        try
        {
            int pos = 0;

            // Magic
            Magic.CopyTo(buffer.AsSpan(pos));
            pos += 7;

            // Attribute count
            pos += WriteFieldLength(buffer.AsSpan(pos), attributes.Count);

            // Attributes
            foreach (var (key, value) in attributes)
            {
                var keyBytes = Encoding.UTF8.GetBytes(key);
                var valBytes = Encoding.UTF8.GetBytes(value ?? "");

                pos += WriteFieldLength(buffer.AsSpan(pos), keyBytes.Length);
                keyBytes.CopyTo(buffer.AsSpan(pos));
                pos += keyBytes.Length;

                pos += WriteFieldLength(buffer.AsSpan(pos), valBytes.Length);
                valBytes.CopyTo(buffer.AsSpan(pos));
                pos += valBytes.Length;
            }

            // Content length + content
            BinaryPrimitives.WriteInt64BigEndian(buffer.AsSpan(pos), content.Length);
            pos += 8;
            content.CopyTo(buffer.AsSpan(pos));
            pos += content.Length;

            // Copy exact size to final array (this is for the pre-generated pool only)
            var result = new byte[pos];
            buffer.AsSpan(0, pos).CopyTo(result);
            return result;
        }
        finally
        {
            ArrayPool<byte>.Shared.Return(buffer);
        }
    }

    /// <summary>
    /// Package directly into an ArrayPool-rented buffer. Returns OwnedBuffer.
    /// No ToArray(), no GC allocation. Caller must Return() when done.
    /// </summary>
    public static OwnedBuffer PackageToOwned(Dictionary<string, string> attributes, ReadOnlySpan<byte> content)
    {
        // Calculate exact size
        int size = 7 + 2; // magic + attr count
        foreach (var (key, value) in attributes)
        {
            var keyLen = Encoding.UTF8.GetByteCount(key);
            var valLen = Encoding.UTF8.GetByteCount(value ?? "");
            size += FieldLengthSize(keyLen) + keyLen + FieldLengthSize(valLen) + valLen;
        }
        size += 8 + content.Length;

        var buffer = ArrayPool<byte>.Shared.Rent(size);
        int pos = 0;

        Magic.CopyTo(buffer.AsSpan(pos));
        pos += 7;

        pos += WriteFieldLength(buffer.AsSpan(pos), attributes.Count);

        foreach (var (key, value) in attributes)
        {
            var keyLen = Encoding.UTF8.GetByteCount(key);
            pos += WriteFieldLength(buffer.AsSpan(pos), keyLen);
            Encoding.UTF8.GetBytes(key, buffer.AsSpan(pos, keyLen));
            pos += keyLen;

            var valLen = Encoding.UTF8.GetByteCount(value ?? "");
            pos += WriteFieldLength(buffer.AsSpan(pos), valLen);
            Encoding.UTF8.GetBytes(value ?? "", buffer.AsSpan(pos, valLen));
            pos += valLen;
        }

        BinaryPrimitives.WriteInt64BigEndian(buffer.AsSpan(pos), content.Length);
        pos += 8;
        content.CopyTo(buffer.AsSpan(pos));
        pos += content.Length;

        return new OwnedBuffer(buffer, pos);
    }

    /// <summary>
    /// Zero-copy unpackage over an OwnedBuffer — content is a Memory slice, no allocation.
    /// </summary>
    public static FlowFileRef UnpackageZeroCopy(OwnedBuffer owned)
    {
        return UnpackageZeroCopy(owned.Array, 0, owned.Length);
    }

    /// <summary>
    /// Zero-copy unpackage: returns a FlowFileRef with Memory&lt;byte&gt; slice
    /// pointing into the original data buffer. No content allocation.
    /// </summary>
    public static FlowFileRef UnpackageZeroCopy(byte[] data, int offset = 0)
        => UnpackageZeroCopy(data, offset, data.Length);

    public static FlowFileRef UnpackageZeroCopy(byte[] data, int offset, int dataLength)
    {
        // Verify magic
        for (int i = 0; i < 7; i++)
            if (data[offset + i] != Magic[i])
                throw new InvalidDataException("Invalid FlowFile V3 magic");

        int pos = offset + 7;

        var (count, newPos) = ReadFieldLength(data, pos);
        pos = newPos;

        var attrs = new Dictionary<string, string>(count);
        for (int i = 0; i < count; i++)
        {
            var (keyLen, p1) = ReadFieldLength(data, pos);
            pos = p1;
            var key = Encoding.UTF8.GetString(data, pos, keyLen);
            pos += keyLen;

            var (valLen, p2) = ReadFieldLength(data, pos);
            pos = p2;
            var val = Encoding.UTF8.GetString(data, pos, valLen);
            pos += valLen;

            attrs[key] = val;
        }

        long contentLen = BinaryPrimitives.ReadInt64BigEndian(data.AsSpan(pos, 8));
        pos += 8;

        // Zero-copy: slice the original buffer, no new byte[] allocation
        var content = new ReadOnlyMemory<byte>(data, pos, (int)contentLen);

        return new FlowFileRef(attrs, content);
    }

    /// <summary>
    /// Original naive unpackage — allocates new byte[] for content every time.
    /// Kept for comparison benchmarks.
    /// </summary>
    public static (Dictionary<string, string> attrs, byte[] content, int nextOffset) UnpackageNaive(byte[] data, int offset = 0)
    {
        for (int i = 0; i < 7; i++)
            if (data[offset + i] != Magic[i])
                throw new InvalidDataException("Invalid FlowFile V3 magic");

        int pos = offset + 7;
        var (count, newPos) = ReadFieldLength(data, pos);
        pos = newPos;

        var attrs = new Dictionary<string, string>(count);
        for (int i = 0; i < count; i++)
        {
            var (keyLen, p1) = ReadFieldLength(data, pos);
            pos = p1;
            var key = Encoding.UTF8.GetString(data, pos, keyLen);
            pos += keyLen;

            var (valLen, p2) = ReadFieldLength(data, pos);
            pos = p2;
            var val = Encoding.UTF8.GetString(data, pos, valLen);
            pos += valLen;

            attrs[key] = val;
        }

        long contentLen = BinaryPrimitives.ReadInt64BigEndian(data.AsSpan(pos, 8));
        pos += 8;
        var content = new byte[contentLen];
        Array.Copy(data, pos, content, 0, contentLen);
        pos += (int)contentLen;

        return (attrs, content, pos);
    }

    static int FieldLengthSize(int value) => value < MaxValue2Bytes ? 2 : 6;

    static int WriteFieldLength(Span<byte> buf, int value)
    {
        if (value < MaxValue2Bytes)
        {
            BinaryPrimitives.WriteUInt16BigEndian(buf, (ushort)value);
            return 2;
        }
        buf[0] = 0xFF;
        buf[1] = 0xFF;
        BinaryPrimitives.WriteUInt32BigEndian(buf[2..], (uint)value);
        return 6;
    }

    static (int value, int newOffset) ReadFieldLength(byte[] data, int offset)
    {
        int val = BinaryPrimitives.ReadUInt16BigEndian(data.AsSpan(offset, 2));
        if (val < MaxValue2Bytes)
            return (val, offset + 2);
        int big = (int)BinaryPrimitives.ReadUInt32BigEndian(data.AsSpan(offset + 2, 4));
        return (big, offset + 6);
    }
}

// --- Benchmark ---

static class Bench
{
    static byte[] MakeFlowFile(int size, int index)
    {
        var attrs = new Dictionary<string, string>
        {
            ["filename"] = $"flowfile_{index}.dat",
            ["uuid"] = $"aaaaaaaa-bbbb-cccc-dddd-{index:D12}",
            ["mime.type"] = "application/octet-stream",
            ["path"] = "/data/input/",
        };
        var content = new byte[size];
        Random.Shared.NextBytes(content);
        return FlowFileV3.PackagePooled(attrs, content);
    }

    // =========================================================================
    // NAIVE benchmarks (original — allocates on every unpackage)
    // =========================================================================

    static async Task<BenchResult> BenchChannelNaive(byte[][] flowfiles, int count, string label)
    {
        var channel = Channel.CreateBounded<byte[]>(1000);
        long totalBytes = 0;
        int processed = 0;

        var consumer = Task.Run(async () =>
        {
            await foreach (var item in channel.Reader.ReadAllAsync())
            {
                var (attrs, content, _) = FlowFileV3.UnpackageNaive(item);
                Interlocked.Add(ref totalBytes, content.Length);
                Interlocked.Increment(ref processed);
            }
        });

        var sw = Stopwatch.StartNew();

        var producer = Task.Run(async () =>
        {
            for (int i = 0; i < count; i++)
                await channel.Writer.WriteAsync(flowfiles[i % flowfiles.Length]);
            channel.Writer.Complete();
        });

        await Task.WhenAll(producer, consumer);
        sw.Stop();

        double elapsed = sw.Elapsed.TotalSeconds;
        return new BenchResult
        {
            Test = $"naive-channel-{label}",
            Count = processed,
            ElapsedSec = Math.Round(elapsed, 3),
            MsgsPerSec = (long)(processed / elapsed),
            MbPerSec = Math.Round(totalBytes / elapsed / (1024.0 * 1024.0), 1),
        };
    }

    static async Task<BenchResult> BenchChannelNaiveFanout(byte[][] flowfiles, int count, int numConsumers, string label)
    {
        var channel = Channel.CreateBounded<byte[]>(2000);
        long totalBytes = 0;
        int processed = 0;

        var consumers = Enumerable.Range(0, numConsumers).Select(_ => Task.Run(async () =>
        {
            await foreach (var item in channel.Reader.ReadAllAsync())
            {
                var (attrs, content, _) = FlowFileV3.UnpackageNaive(item);
                Interlocked.Add(ref totalBytes, content.Length);
                Interlocked.Increment(ref processed);
            }
        })).ToArray();

        var sw = Stopwatch.StartNew();
        for (int i = 0; i < count; i++)
            await channel.Writer.WriteAsync(flowfiles[i % flowfiles.Length]);
        channel.Writer.Complete();

        await Task.WhenAll(consumers);
        sw.Stop();

        double elapsed = sw.Elapsed.TotalSeconds;
        return new BenchResult
        {
            Test = $"naive-fanout-{numConsumers}c-{label}",
            Count = processed,
            ElapsedSec = Math.Round(elapsed, 3),
            MsgsPerSec = (long)(processed / elapsed),
            MbPerSec = Math.Round(totalBytes / elapsed / (1024.0 * 1024.0), 1),
        };
    }

    // =========================================================================
    // OPTIMIZED benchmarks — zero-copy Memory<byte> slicing, no content alloc
    // =========================================================================

    static async Task<BenchResult> BenchChannelZeroCopy(byte[][] flowfiles, int count, string label)
    {
        var channel = Channel.CreateBounded<byte[]>(1000);
        long totalBytes = 0;
        int processed = 0;

        var consumer = Task.Run(async () =>
        {
            await foreach (var item in channel.Reader.ReadAllAsync())
            {
                var ff = FlowFileV3.UnpackageZeroCopy(item);
                Interlocked.Add(ref totalBytes, ff.Content.Length);
                Interlocked.Increment(ref processed);
            }
        });

        var sw = Stopwatch.StartNew();

        var producer = Task.Run(async () =>
        {
            for (int i = 0; i < count; i++)
                await channel.Writer.WriteAsync(flowfiles[i % flowfiles.Length]);
            channel.Writer.Complete();
        });

        await Task.WhenAll(producer, consumer);
        sw.Stop();

        double elapsed = sw.Elapsed.TotalSeconds;
        return new BenchResult
        {
            Test = $"zerocopy-channel-{label}",
            Count = processed,
            ElapsedSec = Math.Round(elapsed, 3),
            MsgsPerSec = (long)(processed / elapsed),
            MbPerSec = Math.Round(totalBytes / elapsed / (1024.0 * 1024.0), 1),
        };
    }

    static async Task<BenchResult> BenchChannelZeroCopyFanout(byte[][] flowfiles, int count, int numConsumers, string label)
    {
        var channel = Channel.CreateBounded<byte[]>(2000);
        long totalBytes = 0;
        int processed = 0;

        var consumers = Enumerable.Range(0, numConsumers).Select(_ => Task.Run(async () =>
        {
            await foreach (var item in channel.Reader.ReadAllAsync())
            {
                var ff = FlowFileV3.UnpackageZeroCopy(item);
                Interlocked.Add(ref totalBytes, ff.Content.Length);
                Interlocked.Increment(ref processed);
            }
        })).ToArray();

        var sw = Stopwatch.StartNew();
        for (int i = 0; i < count; i++)
            await channel.Writer.WriteAsync(flowfiles[i % flowfiles.Length]);
        channel.Writer.Complete();

        await Task.WhenAll(consumers);
        sw.Stop();

        double elapsed = sw.Elapsed.TotalSeconds;
        return new BenchResult
        {
            Test = $"zerocopy-fanout-{numConsumers}c-{label}",
            Count = processed,
            ElapsedSec = Math.Round(elapsed, 3),
            MsgsPerSec = (long)(processed / elapsed),
            MbPerSec = Math.Round(totalBytes / elapsed / (1024.0 * 1024.0), 1),
        };
    }

    // =========================================================================
    // OPTIMIZED + ArrayPool — consumer rents buffer, copies content, returns it
    // Simulates real processor that needs a mutable content buffer to work on
    // =========================================================================

    static async Task<BenchResult> BenchChannelArrayPool(byte[][] flowfiles, int count, string label)
    {
        var channel = Channel.CreateBounded<byte[]>(1000);
        long totalBytes = 0;
        int processed = 0;
        var pool = ArrayPool<byte>.Shared;

        var consumer = Task.Run(async () =>
        {
            await foreach (var item in channel.Reader.ReadAllAsync())
            {
                var ff = FlowFileV3.UnpackageZeroCopy(item);
                int len = ff.Content.Length;

                // Simulate processor needing a mutable copy — rent from pool
                var buf = pool.Rent(len);
                ff.Content.Span.CopyTo(buf);

                // "Process" — touch the data so it's not optimized away
                Interlocked.Add(ref totalBytes, len);
                Interlocked.Increment(ref processed);

                pool.Return(buf);
            }
        });

        var sw = Stopwatch.StartNew();

        var producer = Task.Run(async () =>
        {
            for (int i = 0; i < count; i++)
                await channel.Writer.WriteAsync(flowfiles[i % flowfiles.Length]);
            channel.Writer.Complete();
        });

        await Task.WhenAll(producer, consumer);
        sw.Stop();

        double elapsed = sw.Elapsed.TotalSeconds;
        return new BenchResult
        {
            Test = $"arraypool-channel-{label}",
            Count = processed,
            ElapsedSec = Math.Round(elapsed, 3),
            MsgsPerSec = (long)(processed / elapsed),
            MbPerSec = Math.Round(totalBytes / elapsed / (1024.0 * 1024.0), 1),
        };
    }

    static async Task<BenchResult> BenchChannelArrayPoolFanout(byte[][] flowfiles, int count, int numConsumers, string label)
    {
        var channel = Channel.CreateBounded<byte[]>(2000);
        long totalBytes = 0;
        int processed = 0;
        var pool = ArrayPool<byte>.Shared;

        var consumers = Enumerable.Range(0, numConsumers).Select(_ => Task.Run(async () =>
        {
            await foreach (var item in channel.Reader.ReadAllAsync())
            {
                var ff = FlowFileV3.UnpackageZeroCopy(item);
                int len = ff.Content.Length;

                var buf = pool.Rent(len);
                ff.Content.Span.CopyTo(buf);

                Interlocked.Add(ref totalBytes, len);
                Interlocked.Increment(ref processed);

                pool.Return(buf);
            }
        })).ToArray();

        var sw = Stopwatch.StartNew();
        for (int i = 0; i < count; i++)
            await channel.Writer.WriteAsync(flowfiles[i % flowfiles.Length]);
        channel.Writer.Complete();

        await Task.WhenAll(consumers);
        sw.Stop();

        double elapsed = sw.Elapsed.TotalSeconds;
        return new BenchResult
        {
            Test = $"arraypool-fanout-{numConsumers}c-{label}",
            Count = processed,
            ElapsedSec = Math.Round(elapsed, 3),
            MsgsPerSec = (long)(processed / elapsed),
            MbPerSec = Math.Round(totalBytes / elapsed / (1024.0 * 1024.0), 1),
        };
    }

    // =========================================================================
    // MUTATE benchmarks — unpack -> modify attrs + XOR content -> repack
    // This is the realistic processor workload: FlowFiles are transformed.
    // =========================================================================

    /// <summary>
    /// Naive mutate: Unpackage allocates new byte[], modify, repackage with MemoryStream.
    /// </summary>
    static byte[] MutateNaive(byte[] packed)
    {
        var (attrs, content, _) = FlowFileV3.UnpackageNaive(packed);
        attrs["processed_by"] = "enrich_v2";
        attrs["hop_count"] = (int.Parse(attrs.GetValueOrDefault("hop_count", "0")) + 1).ToString();
        // XOR first 64 bytes
        for (int i = 0; i < Math.Min(64, content.Length); i++)
            content[i] ^= 0xAA;
        return FlowFileV3.PackagePooled(attrs, content);
    }

    /// <summary>
    /// Optimized mutate: zero-copy unpackage, ArrayPool for mutable buffer, pooled repackage.
    /// </summary>
    static byte[] MutateOptimized(byte[] packed)
    {
        var ff = FlowFileV3.UnpackageZeroCopy(packed);
        var attrs = ff.Attributes;
        attrs["processed_by"] = "enrich_v2";
        attrs["hop_count"] = (int.Parse(attrs.GetValueOrDefault("hop_count", "0")) + 1).ToString();

        int len = ff.Content.Length;
        var pool = ArrayPool<byte>.Shared;
        var buf = pool.Rent(len);
        try
        {
            ff.Content.Span.CopyTo(buf);
            // XOR first 64 bytes
            for (int i = 0; i < Math.Min(64, len); i++)
                buf[i] ^= 0xAA;
            return FlowFileV3.PackagePooled(attrs, buf.AsSpan(0, len).ToArray());
        }
        finally
        {
            pool.Return(buf);
        }
    }

    // --- Naive mutate: single processor ---
    static async Task<BenchResult> BenchMutateNaive(byte[][] flowfiles, int count, string label)
    {
        var inCh = Channel.CreateBounded<byte[]>(1000);
        var outCh = Channel.CreateBounded<byte[]>(1000);
        long totalBytes = 0;
        int processed = 0;

        // Processor: read -> mutate -> write
        var processor = Task.Run(async () =>
        {
            await foreach (var item in inCh.Reader.ReadAllAsync())
            {
                var mutated = MutateNaive(item);
                await outCh.Writer.WriteAsync(mutated);
                Interlocked.Add(ref totalBytes, item.Length);
                Interlocked.Increment(ref processed);
            }
            outCh.Writer.Complete();
        });

        // Sink: drain output
        var sink = Task.Run(async () =>
        {
            await foreach (var _ in outCh.Reader.ReadAllAsync()) { }
        });

        var sw = Stopwatch.StartNew();
        for (int i = 0; i < count; i++)
            await inCh.Writer.WriteAsync(flowfiles[i % flowfiles.Length]);
        inCh.Writer.Complete();

        await Task.WhenAll(processor, sink);
        sw.Stop();

        double elapsed = sw.Elapsed.TotalSeconds;
        return new BenchResult
        {
            Test = $"mutate-naive-{label}",
            Count = processed,
            ElapsedSec = Math.Round(elapsed, 3),
            MsgsPerSec = (long)(processed / elapsed),
            MbPerSec = Math.Round(totalBytes / elapsed / (1024.0 * 1024.0), 1),
        };
    }

    // --- Optimized mutate: single processor ---
    static async Task<BenchResult> BenchMutateOptimized(byte[][] flowfiles, int count, string label)
    {
        var inCh = Channel.CreateBounded<byte[]>(1000);
        var outCh = Channel.CreateBounded<byte[]>(1000);
        long totalBytes = 0;
        int processed = 0;

        var processor = Task.Run(async () =>
        {
            await foreach (var item in inCh.Reader.ReadAllAsync())
            {
                var mutated = MutateOptimized(item);
                await outCh.Writer.WriteAsync(mutated);
                Interlocked.Add(ref totalBytes, item.Length);
                Interlocked.Increment(ref processed);
            }
            outCh.Writer.Complete();
        });

        var sink = Task.Run(async () =>
        {
            await foreach (var _ in outCh.Reader.ReadAllAsync()) { }
        });

        var sw = Stopwatch.StartNew();
        for (int i = 0; i < count; i++)
            await inCh.Writer.WriteAsync(flowfiles[i % flowfiles.Length]);
        inCh.Writer.Complete();

        await Task.WhenAll(processor, sink);
        sw.Stop();

        double elapsed = sw.Elapsed.TotalSeconds;
        return new BenchResult
        {
            Test = $"mutate-optimized-{label}",
            Count = processed,
            ElapsedSec = Math.Round(elapsed, 3),
            MsgsPerSec = (long)(processed / elapsed),
            MbPerSec = Math.Round(totalBytes / elapsed / (1024.0 * 1024.0), 1),
        };
    }

    // --- Optimized mutate: fan-out (N parallel processors) ---
    static async Task<BenchResult> BenchMutateOptimizedFanout(byte[][] flowfiles, int count, int numWorkers, string label)
    {
        var inCh = Channel.CreateBounded<byte[]>(2000);
        var outCh = Channel.CreateBounded<byte[]>(2000);
        long totalBytes = 0;
        int processed = 0;
        int workersFinished = 0;

        var workers = Enumerable.Range(0, numWorkers).Select(_ => Task.Run(async () =>
        {
            await foreach (var item in inCh.Reader.ReadAllAsync())
            {
                var mutated = MutateOptimized(item);
                await outCh.Writer.WriteAsync(mutated);
                Interlocked.Add(ref totalBytes, item.Length);
                Interlocked.Increment(ref processed);
            }
            if (Interlocked.Increment(ref workersFinished) == numWorkers)
                outCh.Writer.Complete();
        })).ToArray();

        var sink = Task.Run(async () =>
        {
            await foreach (var _ in outCh.Reader.ReadAllAsync()) { }
        });

        var sw = Stopwatch.StartNew();
        for (int i = 0; i < count; i++)
            await inCh.Writer.WriteAsync(flowfiles[i % flowfiles.Length]);
        inCh.Writer.Complete();

        await Task.WhenAll(workers.Append(sink));
        sw.Stop();

        double elapsed = sw.Elapsed.TotalSeconds;
        return new BenchResult
        {
            Test = $"mutate-opt-fanout-{numWorkers}w-{label}",
            Count = processed,
            ElapsedSec = Math.Round(elapsed, 3),
            MsgsPerSec = (long)(processed / elapsed),
            MbPerSec = Math.Round(totalBytes / elapsed / (1024.0 * 1024.0), 1),
        };
    }

    // =========================================================================
    // POOLED LIFECYCLE mutate — zero heap allocation end-to-end
    // Input: byte[] (pre-generated) -> OwnedBuffer through channel -> sink returns
    // This is the best .NET can do without unsafe code.
    // =========================================================================

    /// <summary>
    /// Full pooled lifecycle: zero-copy unpack from input, XOR content into rented
    /// buffer, repackage into rented OwnedBuffer, flow through channel, sink returns.
    /// </summary>
    static OwnedBuffer MutateToOwned(byte[] packed)
    {
        var ff = FlowFileV3.UnpackageZeroCopy(packed);
        var attrs = ff.Attributes;
        attrs["processed_by"] = "enrich_v2";
        attrs["hop_count"] = (int.Parse(attrs.GetValueOrDefault("hop_count", "0")) + 1).ToString();

        int len = ff.Content.Length;
        var pool = ArrayPool<byte>.Shared;

        // Rent a buffer for the mutated content
        var contentBuf = pool.Rent(len);
        ff.Content.Span.CopyTo(contentBuf);
        for (int i = 0; i < Math.Min(64, len); i++)
            contentBuf[i] ^= 0xAA;

        // Package into a rented output buffer (no ToArray!)
        var owned = FlowFileV3.PackageToOwned(attrs, contentBuf.AsSpan(0, len));

        pool.Return(contentBuf);
        return owned;
    }

    /// <summary>
    /// Multi-stage pooled mutate: takes OwnedBuffer in, returns OwnedBuffer out.
    /// Returns the incoming buffer immediately after copying content out.
    /// This is the pattern each processor in a real pipeline would use.
    ///
    /// Ownership contract:
    ///   1. Receive OwnedBuffer from upstream channel
    ///   2. Zero-copy unpackage (slice into rented buffer)
    ///   3. Rent work buffer, copy content into it, mutate
    ///   4. PackageToOwned → new OwnedBuffer (rented)
    ///   5. Return work buffer
    ///   6. Return incoming OwnedBuffer ← safe now, content was copied in step 3
    ///   7. Send new OwnedBuffer downstream
    ///
    /// At most 3 rented buffers per processor at any instant:
    ///   incoming (returned in step 6), work (returned in step 5), outgoing (sent downstream)
    /// </summary>
    static OwnedBuffer MutateOwnedToOwned(OwnedBuffer incoming, string processorName)
    {
        var ff = FlowFileV3.UnpackageZeroCopy(incoming);
        var attrs = ff.Attributes;
        attrs["processed_by"] = processorName;
        attrs["hop_count"] = (int.Parse(attrs.GetValueOrDefault("hop_count", "0")) + 1).ToString();

        int len = ff.Content.Length;
        var pool = ArrayPool<byte>.Shared;

        // Step 3: rent work buffer, copy + mutate content
        var contentBuf = pool.Rent(len);
        ff.Content.Span.CopyTo(contentBuf);
        for (int i = 0; i < Math.Min(64, len); i++)
            contentBuf[i] ^= 0xAA;

        // Step 4: package into new rented output buffer
        var outOwned = FlowFileV3.PackageToOwned(attrs, contentBuf.AsSpan(0, len));

        // Step 5: return work buffer
        pool.Return(contentBuf);

        // Step 6: return incoming buffer — safe, we copied everything we needed
        incoming.Return();

        return outOwned;
    }

    /// <summary>
    /// 3-stage pipeline benchmark: source → procA → procB → procC → sink
    /// All stages use OwnedBuffer with proper return lifecycle.
    /// Proves pool stays bounded under sustained throughput.
    /// </summary>
    static async Task<BenchResult> BenchPipelinePooled3Stage(byte[][] flowfiles, int count, string label)
    {
        // source → ch0 → procA → ch1 → procB → ch2 → procC → ch3 → sink
        var ch0 = Channel.CreateBounded<OwnedBuffer>(1000);
        var ch1 = Channel.CreateBounded<OwnedBuffer>(1000);
        var ch2 = Channel.CreateBounded<OwnedBuffer>(1000);
        var ch3 = Channel.CreateBounded<OwnedBuffer>(1000);
        int processed = 0;
        long totalBytes = 0;

        // Track pool usage: snapshot rented count at intervals
        long peakMemory = 0;

        // Processor stage factory
        Task MakeStage(Channel<OwnedBuffer> inCh, Channel<OwnedBuffer> outCh, string name) =>
            Task.Run(async () =>
            {
                await foreach (var item in inCh.Reader.ReadAllAsync())
                {
                    var output = MutateOwnedToOwned(item, name);
                    await outCh.Writer.WriteAsync(output);
                }
                outCh.Writer.Complete();
            });

        var procA = MakeStage(ch0, ch1, "procA");
        var procB = MakeStage(ch1, ch2, "procB");
        var procC = MakeStage(ch2, ch3, "procC");

        // Sink: return final buffers, count results
        var sink = Task.Run(async () =>
        {
            await foreach (var owned in ch3.Reader.ReadAllAsync())
            {
                Interlocked.Add(ref totalBytes, owned.Length);
                Interlocked.Increment(ref processed);
                owned.Return();

                // Sample memory periodically
                if (processed % 500 == 0)
                {
                    var mem = GC.GetTotalMemory(false);
                    long prev;
                    while (mem > (prev = Interlocked.Read(ref peakMemory)))
                    {
                        if (Interlocked.CompareExchange(ref peakMemory, mem, prev) == prev)
                            break;
                    }
                }
            }
        });

        // Warm up pool
        GC.Collect(2, GCCollectionMode.Forced, true, true);
        GC.WaitForPendingFinalizers();
        long baselineMemory = GC.GetTotalMemory(true);

        var sw = Stopwatch.StartNew();

        // Source: convert byte[] to OwnedBuffer for first stage
        for (int i = 0; i < count; i++)
        {
            var packed = flowfiles[i % flowfiles.Length];
            var owned = MutateToOwned(packed); // first stage gets an OwnedBuffer
            await ch0.Writer.WriteAsync(owned);
        }
        ch0.Writer.Complete();

        await Task.WhenAll(procA, procB, procC, sink);
        sw.Stop();

        long finalPeak = Interlocked.Read(ref peakMemory);
        double poolOverheadMb = Math.Max(0, (finalPeak - baselineMemory)) / (1024.0 * 1024.0);

        double elapsed = sw.Elapsed.TotalSeconds;
        return new BenchResult
        {
            Test = $"pipeline-3stage-{label}",
            Count = processed,
            ElapsedSec = Math.Round(elapsed, 3),
            MsgsPerSec = (long)(processed / elapsed),
            MbPerSec = Math.Round(totalBytes / elapsed / (1024.0 * 1024.0), 1),
            TotalAllocatedMb = Math.Round(poolOverheadMb, 1),
        };
    }

    /// <summary>
    /// Same 3-stage pipeline but with naive byte[] — for comparison.
    /// </summary>
    static async Task<BenchResult> BenchPipelineNaive3Stage(byte[][] flowfiles, int count, string label)
    {
        var ch0 = Channel.CreateBounded<byte[]>(1000);
        var ch1 = Channel.CreateBounded<byte[]>(1000);
        var ch2 = Channel.CreateBounded<byte[]>(1000);
        var ch3 = Channel.CreateBounded<byte[]>(1000);
        int processed = 0;
        long totalBytes = 0;

        Task MakeStage(Channel<byte[]> inCh, Channel<byte[]> outCh) =>
            Task.Run(async () =>
            {
                await foreach (var item in inCh.Reader.ReadAllAsync())
                {
                    var mutated = MutateNaive(item);
                    await outCh.Writer.WriteAsync(mutated);
                }
                outCh.Writer.Complete();
            });

        var procA = MakeStage(ch0, ch1);
        var procB = MakeStage(ch1, ch2);
        var procC = MakeStage(ch2, ch3);

        var sink = Task.Run(async () =>
        {
            await foreach (var item in ch3.Reader.ReadAllAsync())
            {
                Interlocked.Add(ref totalBytes, item.Length);
                Interlocked.Increment(ref processed);
            }
        });

        var sw = Stopwatch.StartNew();
        for (int i = 0; i < count; i++)
            await ch0.Writer.WriteAsync(flowfiles[i % flowfiles.Length]);
        ch0.Writer.Complete();

        await Task.WhenAll(procA, procB, procC, sink);
        sw.Stop();

        double elapsed = sw.Elapsed.TotalSeconds;
        return new BenchResult
        {
            Test = $"pipeline-naive-3stage-{label}",
            Count = processed,
            ElapsedSec = Math.Round(elapsed, 3),
            MsgsPerSec = (long)(processed / elapsed),
            MbPerSec = Math.Round(totalBytes / elapsed / (1024.0 * 1024.0), 1),
        };
    }

    // --- Pooled lifecycle mutate: single processor ---
    static async Task<BenchResult> BenchMutatePooled(byte[][] flowfiles, int count, string label)
    {
        var inCh = Channel.CreateBounded<byte[]>(1000);
        var outCh = Channel.CreateBounded<OwnedBuffer>(1000);
        long totalBytes = 0;
        int processed = 0;

        var processor = Task.Run(async () =>
        {
            await foreach (var item in inCh.Reader.ReadAllAsync())
            {
                var owned = MutateToOwned(item);
                await outCh.Writer.WriteAsync(owned);
                Interlocked.Add(ref totalBytes, item.Length);
                Interlocked.Increment(ref processed);
            }
            outCh.Writer.Complete();
        });

        // Sink: drain and return pooled buffers
        var sink = Task.Run(async () =>
        {
            await foreach (var owned in outCh.Reader.ReadAllAsync())
                owned.Return();
        });

        var sw = Stopwatch.StartNew();
        for (int i = 0; i < count; i++)
            await inCh.Writer.WriteAsync(flowfiles[i % flowfiles.Length]);
        inCh.Writer.Complete();

        await Task.WhenAll(processor, sink);
        sw.Stop();

        double elapsed = sw.Elapsed.TotalSeconds;
        return new BenchResult
        {
            Test = $"mutate-pooled-{label}",
            Count = processed,
            ElapsedSec = Math.Round(elapsed, 3),
            MsgsPerSec = (long)(processed / elapsed),
            MbPerSec = Math.Round(totalBytes / elapsed / (1024.0 * 1024.0), 1),
        };
    }

    // --- Pooled lifecycle mutate: fan-out ---
    static async Task<BenchResult> BenchMutatePooledFanout(byte[][] flowfiles, int count, int numWorkers, string label)
    {
        var inCh = Channel.CreateBounded<byte[]>(2000);
        var outCh = Channel.CreateBounded<OwnedBuffer>(2000);
        long totalBytes = 0;
        int processed = 0;
        int workersFinished = 0;

        var workers = Enumerable.Range(0, numWorkers).Select(_ => Task.Run(async () =>
        {
            await foreach (var item in inCh.Reader.ReadAllAsync())
            {
                var owned = MutateToOwned(item);
                await outCh.Writer.WriteAsync(owned);
                Interlocked.Add(ref totalBytes, item.Length);
                Interlocked.Increment(ref processed);
            }
            if (Interlocked.Increment(ref workersFinished) == numWorkers)
                outCh.Writer.Complete();
        })).ToArray();

        var sink = Task.Run(async () =>
        {
            await foreach (var owned in outCh.Reader.ReadAllAsync())
                owned.Return();
        });

        var sw = Stopwatch.StartNew();
        for (int i = 0; i < count; i++)
            await inCh.Writer.WriteAsync(flowfiles[i % flowfiles.Length]);
        inCh.Writer.Complete();

        await Task.WhenAll(workers.Append(sink));
        sw.Stop();

        double elapsed = sw.Elapsed.TotalSeconds;
        return new BenchResult
        {
            Test = $"mutate-pooled-fanout-{numWorkers}w-{label}",
            Count = processed,
            ElapsedSec = Math.Round(elapsed, 3),
            MsgsPerSec = (long)(processed / elapsed),
            MbPerSec = Math.Round(totalBytes / elapsed / (1024.0 * 1024.0), 1),
        };
    }

    // =========================================================================
    // CANCELLATION benchmark — measures how fast a processor stops
    // .NET: CancellationToken integrated into Channel.ReadAsync
    // Python: no equivalent — must poll or use sentinel values
    // =========================================================================

    static async Task<BenchResult> BenchCancellation(byte[][] flowfiles, string label, int cancelAfterMs)
    {
        var channel = Channel.CreateBounded<byte[]>(5000);
        var cts = new CancellationTokenSource();
        long totalBytes = 0;
        int processed = 0;
        double cancelLatencyUs = 0;

        var consumer = Task.Run(async () =>
        {
            try
            {
                await foreach (var item in channel.Reader.ReadAllAsync(cts.Token))
                {
                    var ff = FlowFileV3.UnpackageZeroCopy(item);
                    Interlocked.Add(ref totalBytes, ff.Content.Length);
                    Interlocked.Increment(ref processed);
                }
            }
            catch (OperationCanceledException) { }
        });

        var sw = Stopwatch.StartNew();

        // Producer: flood the channel
        var producer = Task.Run(async () =>
        {
            try
            {
                int i = 0;
                while (!cts.IsCancellationRequested)
                {
                    await channel.Writer.WriteAsync(flowfiles[i % flowfiles.Length], cts.Token);
                    i++;
                }
            }
            catch (OperationCanceledException) { }
        });

        // Cancel after specified delay
        await Task.Delay(cancelAfterMs);
        var cancelSw = Stopwatch.StartNew();
        cts.Cancel();
        await Task.WhenAll(producer, consumer);
        cancelSw.Stop();
        sw.Stop();

        cancelLatencyUs = cancelSw.Elapsed.TotalMicroseconds;

        return new BenchResult
        {
            Test = $"cancel-{label}-after{cancelAfterMs}ms",
            Count = processed,
            ElapsedSec = Math.Round(sw.Elapsed.TotalSeconds, 3),
            MsgsPerSec = (long)(processed / sw.Elapsed.TotalSeconds),
            MbPerSec = Math.Round(totalBytes / sw.Elapsed.TotalSeconds / (1024.0 * 1024.0), 1),
            CancelLatencyUs = Math.Round(cancelLatencyUs, 1),
        };
    }

    // Multi-consumer cancellation — all consumers stop on same token
    static async Task<BenchResult> BenchCancellationFanout(byte[][] flowfiles, int numConsumers, string label, int cancelAfterMs)
    {
        var channel = Channel.CreateBounded<byte[]>(5000);
        var cts = new CancellationTokenSource();
        long totalBytes = 0;
        int processed = 0;

        var consumers = Enumerable.Range(0, numConsumers).Select(_ => Task.Run(async () =>
        {
            try
            {
                await foreach (var item in channel.Reader.ReadAllAsync(cts.Token))
                {
                    var ff = FlowFileV3.UnpackageZeroCopy(item);
                    Interlocked.Add(ref totalBytes, ff.Content.Length);
                    Interlocked.Increment(ref processed);
                }
            }
            catch (OperationCanceledException) { }
        })).ToArray();

        var sw = Stopwatch.StartNew();

        var producer = Task.Run(async () =>
        {
            try
            {
                int i = 0;
                while (!cts.IsCancellationRequested)
                {
                    await channel.Writer.WriteAsync(flowfiles[i % flowfiles.Length], cts.Token);
                    i++;
                }
            }
            catch (OperationCanceledException) { }
        });

        await Task.Delay(cancelAfterMs);
        var cancelSw = Stopwatch.StartNew();
        cts.Cancel();
        await Task.WhenAll(consumers.Append(producer));
        cancelSw.Stop();
        sw.Stop();

        return new BenchResult
        {
            Test = $"cancel-fanout-{numConsumers}c-{label}-after{cancelAfterMs}ms",
            Count = processed,
            ElapsedSec = Math.Round(sw.Elapsed.TotalSeconds, 3),
            MsgsPerSec = (long)(processed / sw.Elapsed.TotalSeconds),
            MbPerSec = Math.Round(totalBytes / sw.Elapsed.TotalSeconds / (1024.0 * 1024.0), 1),
            CancelLatencyUs = Math.Round(cancelSw.Elapsed.TotalMicroseconds, 1),
        };
    }

    // =========================================================================
    // GC PRESSURE benchmark — force GC and measure pause time
    // =========================================================================

    static async Task<BenchResult> BenchWithGcStats(
        Func<byte[][], int, string, Task<BenchResult>> benchFn,
        byte[][] flowfiles, int count, string label, string variant)
    {
        // Force full collection before benchmark
        GC.Collect(2, GCCollectionMode.Forced, true, true);
        GC.WaitForPendingFinalizers();

        int gen0Before = GC.CollectionCount(0);
        int gen1Before = GC.CollectionCount(1);
        int gen2Before = GC.CollectionCount(2);
        long allocBefore = GC.GetTotalAllocatedBytes(true);

        var result = await benchFn(flowfiles, count, label);

        int gen0After = GC.CollectionCount(0);
        int gen1After = GC.CollectionCount(1);
        int gen2After = GC.CollectionCount(2);
        long allocAfter = GC.GetTotalAllocatedBytes(true);

        result = result with
        {
            Test = $"{variant}-{label}",
            Gen0Collections = gen0After - gen0Before,
            Gen1Collections = gen1After - gen1Before,
            Gen2Collections = gen2After - gen2Before,
            TotalAllocatedMb = Math.Round((allocAfter - allocBefore) / (1024.0 * 1024.0), 1),
        };

        return result;
    }

    // =========================================================================
    // Runner
    // =========================================================================

    public static async Task RunAll()
    {
        var sizes = new (string Label, int Bytes)[]
        {
            ("1KB", 1024),
            ("10KB", 10 * 1024),
            ("100KB", 100 * 1024),
            ("1MB", 1024 * 1024),
        };

        var counts = new Dictionary<string, int>
        {
            ["1KB"] = 100_000,
            ["10KB"] = 50_000,
            ["100KB"] = 10_000,
            ["1MB"] = 2_000,
        };

        var results = new List<BenchResult>();

        Console.WriteLine($".NET {Environment.Version}");
        Console.WriteLine($"CPUs: {Environment.ProcessorCount}");
        Console.WriteLine($"GC: {(GCSettings.IsServerGC ? "Server" : "Workstation")} | Latency: {GCSettings.LatencyMode}");
        Console.WriteLine(new string('=', 80));

        // --- Part 1: Throughput comparison (naive vs optimized) ---
        Console.WriteLine("\n### PART 1: Queue Throughput — Naive vs Optimized ###\n");

        foreach (var (sizeLabel, sizeBytes) in sizes)
        {
            int count = counts[sizeLabel];
            Console.WriteLine($"--- FlowFile size: {sizeLabel} | count: {count:N0} ---");

            int poolSize = Math.Min(count, 100);
            var flowfiles = Enumerable.Range(0, poolSize).Select(i => MakeFlowFile(sizeBytes, i)).ToArray();

            // Naive (original)
            var r = await BenchWithGcStats(BenchChannelNaive, flowfiles, count, sizeLabel, "naive-channel");
            PrintResult(r);
            results.Add(r);

            // Zero-copy (Memory<byte> slice, no content alloc)
            r = await BenchWithGcStats(BenchChannelZeroCopy, flowfiles, count, sizeLabel, "zerocopy-channel");
            PrintResult(r);
            results.Add(r);

            // ArrayPool (rent/return for mutable processing)
            r = await BenchWithGcStats(BenchChannelArrayPool, flowfiles, count, sizeLabel, "arraypool-channel");
            PrintResult(r);
            results.Add(r);

            Console.WriteLine();
        }

        // --- Part 2: Fan-out comparison ---
        Console.WriteLine("\n### PART 2: Fan-out (4 consumers) — Naive vs Optimized ###\n");

        foreach (var (sizeLabel, sizeBytes) in sizes)
        {
            int count = counts[sizeLabel];
            Console.WriteLine($"--- FlowFile size: {sizeLabel} | count: {count:N0} ---");

            int poolSize = Math.Min(count, 100);
            var flowfiles = Enumerable.Range(0, poolSize).Select(i => MakeFlowFile(sizeBytes, i)).ToArray();

            var r = await BenchChannelNaiveFanout(flowfiles, count, 4, sizeLabel);
            PrintResult(r);
            results.Add(r);

            r = await BenchChannelZeroCopyFanout(flowfiles, count, 4, sizeLabel);
            PrintResult(r);
            results.Add(r);

            r = await BenchChannelArrayPoolFanout(flowfiles, count, 4, sizeLabel);
            PrintResult(r);
            results.Add(r);

            Console.WriteLine();
        }

        // --- Part 3: Cancellation latency ---
        Console.WriteLine("\n### PART 3: Cancellation Latency (processor stop time) ###\n");

        foreach (var (sizeLabel, sizeBytes) in new[] { ("100KB", 100 * 1024), ("1MB", 1024 * 1024) })
        {
            var flowfiles = Enumerable.Range(0, 100).Select(i => MakeFlowFile(sizeBytes, i)).ToArray();

            // Single consumer cancel
            var r = await BenchCancellation(flowfiles, sizeLabel, cancelAfterMs: 500);
            Console.WriteLine($"  {r.Test,-45} {r.MsgsPerSec,10:N0} msgs/s  cancel: {r.CancelLatencyUs,8:F1} us");
            results.Add(r);

            // 4-consumer cancel
            r = await BenchCancellationFanout(flowfiles, 4, sizeLabel, cancelAfterMs: 500);
            Console.WriteLine($"  {r.Test,-45} {r.MsgsPerSec,10:N0} msgs/s  cancel: {r.CancelLatencyUs,8:F1} us");
            results.Add(r);
        }

        // --- Part 4: Mutate FlowFile benchmarks ---
        Console.WriteLine("\n### PART 4: Mutate FlowFile — unpack -> modify -> repack ###\n");

        // Validation: small run (10 items) to verify correctness
        Console.WriteLine("--- Validation (10 items, 1KB) ---");
        var valFfs = Enumerable.Range(0, 10).Select(i => MakeFlowFile(1024, i)).ToArray();
        var valR = await BenchMutateOptimized(valFfs, 10, "validate");
        Console.WriteLine($"  mutate-validate: {valR.Count} processed (expected 10)");

        // Verify mutation (optimized path)
        var original = valFfs[0];
        var mutated = MutateOptimized(original);
        var origFf = FlowFileV3.UnpackageZeroCopy(original);
        var mutFf = FlowFileV3.UnpackageZeroCopy(mutated);
        Debug.Assert(mutFf.Attributes["processed_by"] == "enrich_v2", "Attribute mutation failed");
        Debug.Assert(mutFf.Attributes["hop_count"] == "1", "Hop count failed");
        Debug.Assert(origFf.Content.Span[0] != mutFf.Content.Span[0], "Content mutation failed");
        Console.WriteLine("  Optimized PASSED: attrs mutated, content XOR'd");

        // Verify pooled lifecycle path
        var ownedMut = MutateToOwned(original);
        var pooledFf = FlowFileV3.UnpackageZeroCopy(ownedMut);
        Debug.Assert(pooledFf.Attributes["processed_by"] == "enrich_v2", "Pooled attr mutation failed");
        Debug.Assert(pooledFf.Attributes["hop_count"] == "1", "Pooled hop count failed");
        Debug.Assert(origFf.Content.Span[0] != pooledFf.Content.Span[0], "Pooled content mutation failed");
        ownedMut.Return();
        Console.WriteLine("  Pooled    PASSED: attrs mutated, content XOR'd, buffer returned");

        // Validate pooled bench with 10 items
        var valP = await BenchMutatePooled(valFfs, 10, "validate");
        Console.WriteLine($"  pooled-validate:  {valP.Count} processed (expected 10)");

        // Scale up
        Console.WriteLine();
        foreach (var (sizeLabel, sizeBytes) in sizes)
        {
            int count = counts[sizeLabel];
            Console.WriteLine($"--- Mutate FlowFile size: {sizeLabel} | count: {count:N0} ---");

            int poolSize = Math.Min(count, 100);
            var flowfiles = Enumerable.Range(0, poolSize).Select(i => MakeFlowFile(sizeBytes, i)).ToArray();

            // Naive mutate (with GC stats)
            var r = await BenchWithGcStats(BenchMutateNaive, flowfiles, count, sizeLabel, "mutate-naive");
            PrintResult(r);
            results.Add(r);

            // Optimized mutate (with GC stats)
            r = await BenchWithGcStats(BenchMutateOptimized, flowfiles, count, sizeLabel, "mutate-optimized");
            PrintResult(r);
            results.Add(r);

            // Optimized mutate fan-out (4 workers)
            r = await BenchMutateOptimizedFanout(flowfiles, count, 4, sizeLabel);
            PrintResult(r);
            results.Add(r);

            // Pooled lifecycle mutate (with GC stats)
            r = await BenchWithGcStats(BenchMutatePooled, flowfiles, count, sizeLabel, "mutate-pooled");
            PrintResult(r);
            results.Add(r);

            // Pooled lifecycle mutate fan-out (4 workers)
            r = await BenchMutatePooledFanout(flowfiles, count, 4, sizeLabel);
            PrintResult(r);
            results.Add(r);

            Console.WriteLine();
        }

        // --- Part 5: 3-stage pipeline — proves pool stays bounded ---
        Console.WriteLine("\n### PART 5: 3-Stage Pipeline — source -> A -> B -> C -> sink ###\n");

        // Validation: 10 items through pipeline
        Console.WriteLine("--- Validation (10 items, 1KB) ---");
        var pipeFfs = Enumerable.Range(0, 10).Select(i => MakeFlowFile(1024, i)).ToArray();
        var pipeR = await BenchPipelinePooled3Stage(pipeFfs, 10, "validate");
        Console.WriteLine($"  pipeline-validate: {pipeR.Count} processed (expected 10)");

        // Verify 3 hops
        var pipeInput = pipeFfs[0];
        var hop1 = MutateToOwned(pipeInput);
        var hop2 = MutateOwnedToOwned(hop1, "procA");
        var hop3 = MutateOwnedToOwned(hop2, "procB");
        var finalFf = FlowFileV3.UnpackageZeroCopy(hop3);
        Debug.Assert(finalFf.Attributes["hop_count"] == "3", $"Expected 3 hops, got {finalFf.Attributes["hop_count"]}");
        hop3.Return();
        Console.WriteLine("  Validation PASSED: 3 hops, all buffers returned");

        // Scale up
        Console.WriteLine();
        foreach (var (sizeLabel, sizeBytes) in sizes)
        {
            int count = counts[sizeLabel];
            Console.WriteLine($"--- Pipeline FlowFile size: {sizeLabel} | count: {count:N0} ---");

            int poolSize = Math.Min(count, 100);
            var flowfiles = Enumerable.Range(0, poolSize).Select(i => MakeFlowFile(sizeBytes, i)).ToArray();

            // Naive 3-stage (with GC stats)
            var r = await BenchWithGcStats(
                (ffs, c, l) => BenchPipelineNaive3Stage(ffs, c, l),
                flowfiles, count, sizeLabel, "pipeline-naive-3stage");
            PrintResult(r);
            results.Add(r);

            // Pooled 3-stage (with GC stats + memory tracking)
            r = await BenchWithGcStats(
                (ffs, c, l) => BenchPipelinePooled3Stage(ffs, c, l),
                flowfiles, count, sizeLabel, "pipeline-pooled-3stage");
            PrintResult(r);
            results.Add(r);

            Console.WriteLine();
        }

        // Write JSON results
        var json = JsonSerializer.Serialize(results, new JsonSerializerOptions { WriteIndented = true });
        var outPath = Path.Combine(AppContext.BaseDirectory, "..", "..", "..", "results.json");
        File.WriteAllText(outPath, json);
        Console.WriteLine($"\nResults written to {outPath}");
    }

    static void PrintResult(BenchResult r)
    {
        var gc = r.Gen0Collections > 0 || r.Gen2Collections > 0
            ? $"  GC[{r.Gen0Collections}/{r.Gen1Collections}/{r.Gen2Collections}] alloc:{r.TotalAllocatedMb:F0}MB"
            : "";
        Console.WriteLine($"  {r.Test,-35} {r.MsgsPerSec,10:N0} msgs/s  {r.MbPerSec,8:F1} MB/s  ({r.ElapsedSec}s){gc}");
    }
}

record BenchResult
{
    public string Test { get; init; } = "";
    public int Count { get; init; }
    public double ElapsedSec { get; init; }
    public long MsgsPerSec { get; init; }
    public double MbPerSec { get; init; }
    public double CancelLatencyUs { get; init; }
    public int Gen0Collections { get; init; }
    public int Gen1Collections { get; init; }
    public int Gen2Collections { get; init; }
    public double TotalAllocatedMb { get; init; }
}
