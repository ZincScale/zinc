// Copyright 2026 victorybhg
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.Runtime.CompilerServices;

/// <summary>
/// Benchmark comparing C# AOT vs Go for collection processing.
/// Mirrors the Go benchmarks in benchmarks/python-strategies/bench_go_test.go exactly.
///
/// Build AOT:  dotnet publish -c Release -r linux-x64 /p:PublishAot=true
/// Run:        ./bin/Release/net10.0/linux-x64/publish/csharp-aot
/// </summary>
public class Program
{
    static int[] MakeData(int n)
    {
        var data = new int[n];
        for (int i = 0; i < n; i++)
            data[i] = i + 1;
        return data;
    }

    // Prevent dead-code elimination
    [MethodImpl(MethodImplOptions.NoInlining)]
    static void Consume<T>(T value) { }

    // --- Benchmarks (mirror Go exactly) ---

    static long BenchWhereSelect(int[] data, int iterations)
    {
        int threshold = data.Length / 2;
        var sw = Stopwatch.StartNew();
        for (int iter = 0; iter < iterations; iter++)
        {
            var result = new List<int>();
            for (int j = 0; j < data.Length; j++)
            {
                int x = data[j];
                if (x > threshold)
                    result.Add(x * 2);
            }
            Consume(result);
        }
        sw.Stop();
        return sw.ElapsedTicks * 1_000_000_000 / (Stopwatch.Frequency * iterations);
    }

    static long BenchFirst(int[] data, int iterations)
    {
        int threshold = data.Length - 10;
        var sw = Stopwatch.StartNew();
        for (int iter = 0; iter < iterations; iter++)
        {
            int first = 0;
            for (int j = 0; j < data.Length; j++)
            {
                if (data[j] > threshold)
                {
                    first = data[j];
                    break;
                }
            }
            Consume(first);
        }
        sw.Stop();
        return sw.ElapsedTicks * 1_000_000_000 / (Stopwatch.Frequency * iterations);
    }

    static long BenchAggregate(int[] data, int iterations)
    {
        var sw = Stopwatch.StartNew();
        for (int iter = 0; iter < iterations; iter++)
        {
            int acc = 0;
            for (int j = 0; j < data.Length; j++)
                acc = acc + data[j];
            Consume(acc);
        }
        sw.Stop();
        return sw.ElapsedTicks * 1_000_000_000 / (Stopwatch.Frequency * iterations);
    }

    static long BenchTake(int[] data, int iterations)
    {
        var sw = Stopwatch.StartNew();
        for (int iter = 0; iter < iterations; iter++)
        {
            var result = new List<int>();
            int taken = 0;
            for (int j = 0; j < data.Length; j++)
            {
                if (taken >= 10) break;
                int x = data[j];
                if (x > 5)
                {
                    result.Add(x * 2);
                    taken++;
                }
            }
            Consume(result);
        }
        sw.Stop();
        return sw.ElapsedTicks * 1_000_000_000 / (Stopwatch.Frequency * iterations);
    }

    static long BenchComplex(int[] data, int iterations)
    {
        var sw = Stopwatch.StartNew();
        for (int iter = 0; iter < iterations; iter++)
        {
            int acc = 0;
            for (int j = 0; j < data.Length; j++)
            {
                int x = data[j];
                if (x % 2 == 0)
                    acc = acc + x * x;
            }
            Consume(acc);
        }
        sw.Stop();
        return sw.ElapsedTicks * 1_000_000_000 / (Stopwatch.Frequency * iterations);
    }

    // --- Startup benchmark ---
    static void BenchStartup()
    {
        // Already running — just measure how fast we got here
        var proc = Process.GetCurrentProcess();
        var elapsed = DateTime.UtcNow - proc.StartTime.ToUniversalTime();
        Console.WriteLine($"Startup:          {elapsed.TotalMilliseconds:F1} ms");
    }

    // --- Binary size ---
    static void PrintBinarySize()
    {
        var path = Environment.ProcessPath;
        if (path != null)
        {
            var info = new System.IO.FileInfo(path);
            Console.WriteLine($"Binary size:      {info.Length / 1024.0 / 1024.0:F1} MB");
        }
    }

    static string FormatNs(long ns)
    {
        if (ns < 1_000) return $"{ns} ns";
        if (ns < 1_000_000) return $"{ns / 1000.0:F1} µs";
        if (ns < 1_000_000_000) return $"{ns / 1_000_000.0:F1} ms";
        return $"{ns / 1_000_000_000.0:F2} s";
    }

    public static void Main(string[] args)
    {
        Console.WriteLine("C# AOT Benchmark — Collection Processing");
        Console.WriteLine($"Runtime: {System.Runtime.InteropServices.RuntimeInformation.FrameworkDescription}");
        Console.WriteLine($"AOT: {!System.Runtime.CompilerServices.RuntimeFeature.IsDynamicCodeSupported}");
        Console.WriteLine();

        BenchStartup();
        PrintBinarySize();
        Console.WriteLine();

        int[] sizes = { 1_000, 10_000, 100_000, 1_000_000, 10_000_000 };
        string[] benchNames = { "Where+Select", "First", "Aggregate", "Take(10)", "Complex" };

        // Header
        Console.Write("Benchmark".PadRight(18));
        foreach (var s in sizes)
            Console.Write((" N=" + s).PadLeft(13));
        Console.WriteLine();
        Console.WriteLine(new string('-', 18 + sizes.Length * 13));

        foreach (var name in benchNames)
        {
            Console.Write(name.PadRight(18));
            foreach (var n in sizes)
            {
                var data = MakeData(n);

                // Warmup
                int warmup = n <= 10_000 ? 1000 : (n <= 100_000 ? 100 : 10);
                int iters = n <= 10_000 ? 10000 : (n <= 100_000 ? 1000 : (n <= 1_000_000 ? 100 : 10));

                long ns = 0;
                switch (name)
                {
                    case "Where+Select":
                        BenchWhereSelect(data, warmup);
                        ns = BenchWhereSelect(data, iters);
                        break;
                    case "First":
                        BenchFirst(data, warmup);
                        ns = BenchFirst(data, iters);
                        break;
                    case "Aggregate":
                        BenchAggregate(data, warmup);
                        ns = BenchAggregate(data, iters);
                        break;
                    case "Take(10)":
                        BenchTake(data, warmup);
                        ns = BenchTake(data, iters);
                        break;
                    case "Complex":
                        BenchComplex(data, warmup);
                        ns = BenchComplex(data, iters);
                        break;
                }

                Console.Write(FormatNs(ns).PadLeft(13));
            }
            Console.WriteLine();
        }

        Console.WriteLine();
        Console.WriteLine("Go results (from bench_go_test.go, ns/op):");
        Console.Write("Benchmark".PadRight(18));
        foreach (var s in sizes)
            Console.Write((" N=" + s).PadLeft(13));
        Console.WriteLine();
        Console.WriteLine(new string('-', 18 + sizes.Length * 13));

        // Hardcoded Go results for comparison
        long[,] goResults = {
            { 5273, 49969, 943795, 7798675, 44053823 },   // Where+Select
            { 425, 3698, 39066, 399668, 5294944 },         // First
            { 341, 3689, 40645, 352750, 3957854 },          // Aggregate
            { 242, 253, 265, 246, 179 },                     // Take(10)
            { 357, 4030, 39443, 372030, 3683331 },          // Complex
        };
        for (int b = 0; b < benchNames.Length; b++)
        {
            Console.Write(benchNames[b].PadRight(18));
            for (int s = 0; s < sizes.Length; s++)
                Console.Write(FormatNs(goResults[b, s]).PadLeft(13));
            Console.WriteLine();
        }
    }
}
