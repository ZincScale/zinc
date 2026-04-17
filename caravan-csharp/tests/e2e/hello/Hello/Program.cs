Console.WriteLine("Hello from caravan-csharp!");
Console.WriteLine($"Runtime: {System.Runtime.InteropServices.RuntimeInformation.FrameworkDescription}");
Console.WriteLine($"AOT: {!System.Runtime.CompilerServices.RuntimeFeature.IsDynamicCodeSupported}");
