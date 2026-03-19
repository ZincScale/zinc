// FlowFile HTTP API Benchmark — .NET 10
// Simulates NiFi-style REST ingress/egress for FlowFiles.
// Uses ASP.NET Core minimal API for the server, HttpClient for the client.

using System.Buffers.Binary;
using System.Collections.Concurrent;
using System.Diagnostics;
using System.Net.Http.Headers;
using System.Text;
using System.Text.Json;
using System.Threading.Channels;

await Bench.RunAll();

// --- FlowFile V3 format ---

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
            buf[0] = 0xFF; buf[1] = 0xFF;
            BinaryPrimitives.WriteUInt32BigEndian(buf[2..], (uint)value);
            ms.Write(buf);
        }
    }

    static (int value, int newOffset) ReadFieldLength(byte[] data, int offset)
    {
        int val = BinaryPrimitives.ReadUInt16BigEndian(data.AsSpan(offset, 2));
        if (val < MaxValue2Bytes) return (val, offset + 2);
        int big = (int)BinaryPrimitives.ReadUInt32BigEndian(data.AsSpan(offset + 2, 4));
        return (big, offset + 6);
    }
}

// --- Server + Client benchmark ---

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

    public static async Task RunAll()
    {
        var host = "127.0.0.1";
        var port = 18081;
        var baseUrl = $"http://{host}:{port}";

        // --- Build and start server ---
        var queue = Channel.CreateBounded<byte[]>(50000);
        long receivedCount = 0;
        long receivedBytes = 0;

        var builder = WebApplication.CreateBuilder();
        builder.WebHost.UseUrls(baseUrl);
        builder.Logging.ClearProviders(); // quiet logs
        var app = builder.Build();

        app.MapPost("/flowfile", async (HttpContext ctx) =>
        {
            using var ms = new MemoryStream();
            await ctx.Request.Body.CopyToAsync(ms);
            var data = ms.ToArray();
            var (attrs, content, _) = FlowFileV3.Unpackage(data);
            Interlocked.Increment(ref receivedCount);
            Interlocked.Add(ref receivedBytes, content.Length);
            await queue.Writer.WriteAsync(data);
            ctx.Response.StatusCode = 200;
            await ctx.Response.WriteAsync("OK");
        });

        app.MapGet("/flowfile", async (HttpContext ctx) =>
        {
            if (queue.Reader.TryRead(out var data))
            {
                ctx.Response.ContentType = "application/flowfile-v3";
                await ctx.Response.Body.WriteAsync(data);
            }
            else
            {
                ctx.Response.StatusCode = 204;
            }
        });

        app.MapGet("/stats", () => new { receivedCount, receivedBytes, queueSize = queue.Reader.Count });

        await app.StartAsync();

        try
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
                ["1KB"] = 20_000,
                ["10KB"] = 10_000,
                ["100KB"] = 2_000,
                ["1MB"] = 500,
            };

            int[] concurrencies = [10, 50];
            var results = new List<BenchResult>();

            Console.WriteLine($".NET {Environment.Version}");
            Console.WriteLine($"Server: Kestrel @ {baseUrl}");
            Console.WriteLine($"CPUs: {Environment.ProcessorCount}");
            Console.WriteLine(new string('=', 70));

            using var httpClient = new HttpClient { BaseAddress = new Uri(baseUrl) };

            foreach (var (sizeLabel, sizeBytes) in sizes)
            {
                int count = counts[sizeLabel];
                int poolSize = Math.Min(count, 50);
                var flowfiles = Enumerable.Range(0, poolSize).Select(i => MakeFlowFile(sizeBytes, i)).ToArray();

                Console.WriteLine($"\n--- FlowFile size: {sizeLabel} | count: {count:N0} ---");

                foreach (var conc in concurrencies)
                {
                    // Reset
                    Interlocked.Exchange(ref receivedCount, 0);
                    Interlocked.Exchange(ref receivedBytes, 0);
                    while (queue.Reader.TryRead(out _)) { }

                    // POST benchmark
                    var r = await BenchPost(httpClient, flowfiles, count, conc, sizeLabel);
                    Console.WriteLine($"  POST c={conc,2}: {r.MsgsPerSec,10:N0} msgs/s  {r.MbPerSec,8:F1} MB/s  ({r.ElapsedSec}s)");
                    results.Add(r);

                    // Reset for round-trip
                    Interlocked.Exchange(ref receivedCount, 0);
                    Interlocked.Exchange(ref receivedBytes, 0);
                    while (queue.Reader.TryRead(out _)) { }

                    // Round-trip benchmark
                    r = await BenchRoundtrip(httpClient, flowfiles, count, conc, sizeLabel);
                    Console.WriteLine($"  RT   c={conc,2}: {r.MsgsPerSec,10:N0} msgs/s  {r.MbPerSec,8:F1} MB/s  ({r.ElapsedSec}s)");
                    results.Add(r);
                }
            }

            var json = JsonSerializer.Serialize(results, new JsonSerializerOptions { WriteIndented = true });
            var outPath = Path.Combine(AppContext.BaseDirectory, "..", "..", "..", "results.json");
            File.WriteAllText(outPath, json);
            Console.WriteLine($"\nResults written to {outPath}");
        }
        finally
        {
            await app.StopAsync();
        }
    }

    static async Task<BenchResult> BenchPost(HttpClient client, byte[][] flowfiles, int count, int concurrency, string label)
    {
        var sem = new SemaphoreSlim(concurrency);
        int posted = 0;
        long totalBytes = 0;

        var sw = Stopwatch.StartNew();

        var tasks = Enumerable.Range(0, count).Select(async i =>
        {
            await sem.WaitAsync();
            try
            {
                var ff = flowfiles[i % flowfiles.Length];
                var content = new ByteArrayContent(ff);
                content.Headers.ContentType = new MediaTypeHeaderValue("application/octet-stream");
                var resp = await client.PostAsync("/flowfile", content);
                resp.EnsureSuccessStatusCode();
                Interlocked.Increment(ref posted);
                Interlocked.Add(ref totalBytes, ff.Length);
            }
            finally
            {
                sem.Release();
            }
        }).ToArray();

        await Task.WhenAll(tasks);
        sw.Stop();

        double elapsed = sw.Elapsed.TotalSeconds;
        return new BenchResult
        {
            Test = $"http-post-c{concurrency}-{label}",
            Count = posted,
            ElapsedSec = Math.Round(elapsed, 3),
            MsgsPerSec = (long)(posted / elapsed),
            MbPerSec = Math.Round(totalBytes / elapsed / (1024.0 * 1024.0), 1),
        };
    }

    static async Task<BenchResult> BenchRoundtrip(HttpClient client, byte[][] flowfiles, int count, int concurrency, string label)
    {
        // Fill the queue first
        foreach (var i in Enumerable.Range(0, count))
        {
            var ff = flowfiles[i % flowfiles.Length];
            var content = new ByteArrayContent(ff);
            await client.PostAsync("/flowfile", content);
        }

        var sem = new SemaphoreSlim(concurrency);
        int completed = 0;
        long totalBytes = 0;

        var sw = Stopwatch.StartNew();

        var tasks = Enumerable.Range(0, count).Select(async _ =>
        {
            await sem.WaitAsync();
            try
            {
                var resp = await client.GetAsync("/flowfile");
                if (resp.StatusCode == System.Net.HttpStatusCode.OK)
                {
                    var data = await resp.Content.ReadAsByteArrayAsync();
                    var (attrs, contentBytes, _) = FlowFileV3.Unpackage(data);
                    Interlocked.Increment(ref completed);
                    Interlocked.Add(ref totalBytes, contentBytes.Length);
                }
            }
            finally
            {
                sem.Release();
            }
        }).ToArray();

        await Task.WhenAll(tasks);
        sw.Stop();

        double elapsed = sw.Elapsed.TotalSeconds;
        return new BenchResult
        {
            Test = $"http-roundtrip-c{concurrency}-{label}",
            Count = completed,
            ElapsedSec = Math.Round(elapsed, 3),
            MsgsPerSec = elapsed > 0 ? (long)(completed / elapsed) : 0,
            MbPerSec = elapsed > 0 ? Math.Round(totalBytes / elapsed / (1024.0 * 1024.0), 1) : 0,
        };
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
