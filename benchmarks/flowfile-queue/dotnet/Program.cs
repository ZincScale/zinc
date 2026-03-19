// FlowFile Queue Throughput Benchmark — .NET 10
// Simulates NiFi-style producer/consumer with FlowFiles shuttled through channels.
// Uses System.Threading.Channels (high-perf bounded queues).

using System.Buffers.Binary;
using System.Diagnostics;
using System.Text;
using System.Text.Json;
using System.Threading.Channels;

await Bench.RunAll();

// --- FlowFile V3 binary format ---

static class FlowFileV3
{
    static readonly byte[] Magic = "NiFiFF3"u8.ToArray();
    const int MaxValue2Bytes = 65535;

    public static byte[] Package(Dictionary<string, string> attributes, byte[] content)
    {
        using var ms = new MemoryStream();
        ms.Write(Magic);

        WriteFieldLength(ms, attributes.Count);
        foreach (var (key, value) in attributes)
        {
            var keyBytes = Encoding.UTF8.GetBytes(key);
            var valBytes = Encoding.UTF8.GetBytes(value ?? "");
            WriteFieldLength(ms, keyBytes.Length);
            ms.Write(keyBytes);
            WriteFieldLength(ms, valBytes.Length);
            ms.Write(valBytes);
        }

        Span<byte> lenBuf = stackalloc byte[8];
        BinaryPrimitives.WriteInt64BigEndian(lenBuf, content.Length);
        ms.Write(lenBuf);
        ms.Write(content);

        return ms.ToArray();
    }

    public static (Dictionary<string, string> attrs, byte[] content, int nextOffset) Unpackage(byte[] data, int offset = 0)
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
        var content = new byte[contentLen];
        Array.Copy(data, pos, content, 0, contentLen);
        pos += (int)contentLen;

        return (attrs, content, pos);
    }

    static void WriteFieldLength(MemoryStream ms, int value)
    {
        Span<byte> buf = stackalloc byte[6];
        if (value < MaxValue2Bytes)
        {
            BinaryPrimitives.WriteUInt16BigEndian(buf, (ushort)value);
            ms.Write(buf[..2]);
        }
        else
        {
            buf[0] = 0xFF;
            buf[1] = 0xFF;
            BinaryPrimitives.WriteUInt32BigEndian(buf[2..], (uint)value);
            ms.Write(buf);
        }
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
        return FlowFileV3.Package(attrs, content);
    }

    // --- Channel-based (System.Threading.Channels) ---
    static async Task<BenchResult> BenchChannel(byte[][] flowfiles, int count, string label)
    {
        var channel = Channel.CreateBounded<byte[]>(1000);

        long totalBytes = 0;
        int processed = 0;

        var consumer = Task.Run(async () =>
        {
            await foreach (var item in channel.Reader.ReadAllAsync())
            {
                var (attrs, content, _) = FlowFileV3.Unpackage(item);
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
            Test = $"channel-{label}",
            Count = processed,
            ElapsedSec = Math.Round(elapsed, 3),
            MsgsPerSec = (long)(processed / elapsed),
            MbPerSec = Math.Round(totalBytes / elapsed / (1024.0 * 1024.0), 1),
        };
    }

    // --- Multi-consumer channel (fan-out) ---
    static async Task<BenchResult> BenchChannelFanout(byte[][] flowfiles, int count, int numConsumers, string label)
    {
        var channel = Channel.CreateBounded<byte[]>(2000);

        long totalBytes = 0;
        int processed = 0;

        var consumers = Enumerable.Range(0, numConsumers).Select(_ => Task.Run(async () =>
        {
            await foreach (var item in channel.Reader.ReadAllAsync())
            {
                var (attrs, content, _) = FlowFileV3.Unpackage(item);
                Interlocked.Add(ref totalBytes, content.Length);
                Interlocked.Increment(ref processed);
            }
        })).ToArray();

        var sw = Stopwatch.StartNew();

        // Producer
        for (int i = 0; i < count; i++)
            await channel.Writer.WriteAsync(flowfiles[i % flowfiles.Length]);
        channel.Writer.Complete();

        await Task.WhenAll(consumers);
        sw.Stop();

        double elapsed = sw.Elapsed.TotalSeconds;
        return new BenchResult
        {
            Test = $"channel-fanout-{numConsumers}c-{label}",
            Count = processed,
            ElapsedSec = Math.Round(elapsed, 3),
            MsgsPerSec = (long)(processed / elapsed),
            MbPerSec = Math.Round(totalBytes / elapsed / (1024.0 * 1024.0), 1),
        };
    }

    // --- Thread + BlockingCollection (comparable to Python threading.Queue) ---
    static BenchResult BenchBlockingCollection(byte[][] flowfiles, int count, string label)
    {
        var bc = new System.Collections.Concurrent.BlockingCollection<byte[]>(1000);

        long totalBytes = 0;
        int processed = 0;

        var consumer = new Thread(() =>
        {
            foreach (var item in bc.GetConsumingEnumerable())
            {
                var (attrs, content, _) = FlowFileV3.Unpackage(item);
                totalBytes += content.Length;
                processed++;
            }
        });

        var sw = Stopwatch.StartNew();
        consumer.Start();

        for (int i = 0; i < count; i++)
            bc.Add(flowfiles[i % flowfiles.Length]);
        bc.CompleteAdding();

        consumer.Join();
        sw.Stop();

        double elapsed = sw.Elapsed.TotalSeconds;
        return new BenchResult
        {
            Test = $"blocking-coll-{label}",
            Count = processed,
            ElapsedSec = Math.Round(elapsed, 3),
            MsgsPerSec = (long)(processed / elapsed),
            MbPerSec = Math.Round(totalBytes / elapsed / (1024.0 * 1024.0), 1),
        };
    }

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
        Console.WriteLine(new string('=', 70));

        foreach (var (sizeLabel, sizeBytes) in sizes)
        {
            int count = counts[sizeLabel];
            Console.WriteLine($"\n--- FlowFile size: {sizeLabel} | count: {count:N0} ---");

            int poolSize = Math.Min(count, 100);
            var flowfiles = Enumerable.Range(0, poolSize).Select(i => MakeFlowFile(sizeBytes, i)).ToArray();

            // Channel (async, high-perf)
            var r = await BenchChannel(flowfiles, count, sizeLabel);
            Console.WriteLine($"  channel:         {r.MsgsPerSec,10:N0} msgs/s  {r.MbPerSec,8:F1} MB/s  ({r.ElapsedSec}s)");
            results.Add(r);

            // BlockingCollection (thread-based)
            r = BenchBlockingCollection(flowfiles, count, sizeLabel);
            Console.WriteLine($"  blocking-coll:   {r.MsgsPerSec,10:N0} msgs/s  {r.MbPerSec,8:F1} MB/s  ({r.ElapsedSec}s)");
            results.Add(r);

            // Channel fan-out (4 consumers)
            r = await BenchChannelFanout(flowfiles, count, 4, sizeLabel);
            Console.WriteLine($"  fanout (4 cons): {r.MsgsPerSec,10:N0} msgs/s  {r.MbPerSec,8:F1} MB/s  ({r.ElapsedSec}s)");
            results.Add(r);
        }

        // Write JSON results
        var json = JsonSerializer.Serialize(results, new JsonSerializerOptions { WriteIndented = true });
        var outPath = Path.Combine(AppContext.BaseDirectory, "..", "..", "..", "results.json");
        File.WriteAllText(outPath, json);
        Console.WriteLine($"\nResults written to {outPath}");
    }
}

record BenchResult
{
    public string Test { get; init; } = "";
    public int Count { get; init; }
    public double ElapsedSec { get; init; }
    public long MsgsPerSec { get; init; }
    public double MbPerSec { get; init; }
}
