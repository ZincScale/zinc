# Built-in Functions

Zinc provides built-in functions that map to C#'s standard library. No imports needed — the transpiler adds them automatically.

## I/O

| Zinc            | C# equivalent | Notes |
|-------------------|---------------|-------|
| `print(x)`        | `Console.WriteLine(x)` | |
| `printf(fmt, ...)` | *(planned)* | |
| `readLine()`      | `Console.ReadLine()` | |
| `readFile(path)`  | `File.ReadAllText(path)` | **Failable** — use `or { }` to handle errors |
| `writeFile(path, content)` | `File.WriteAllText(path, content)` | **Failable** — use `or { }` to handle errors |

## Type Conversions

| Zinc            | C# equivalent |
|-------------------|---------------|
| `toString(x)`     | `(x).ToString()` |
| `parseInt(s)`     | `int.Parse(s)` |
| `toInt(s)`        | `int.Parse(s)` |
| `parseFloat(s)`   | `double.Parse(s)` |
| `toFloat(s)`      | `double.Parse(s)` |
| `toBool(s)`       | `bool.Parse(s)` |
| `typeOf(x)`       | `(x).GetType().Name` |

## Collections

| Zinc            | Description |
|-------------------|-------------|
| `list.add(items...)` | Appends one or more items; supports spread (`other...`) |
| `map.remove(key)` | Removes key from map |
| `x.size()`        | Works on lists, maps, strings |
| `list.clone()`    | Deep-copies a list |
| `list.sort()`     | Sorts list in-place |
| `list.join(sep)`  | Join elements into string |
| `map.keys()`      | Returns list of keys |
| `map.values()`    | Returns list of values |
| `x[low:high]`     | Slice with bracket syntax; either bound optional |
| `x.slice(start, end)` | OO slice method; `end` optional |
| `map.containsKey(k)` | Returns Bool |
| `List<T>()`       | Create empty typed list |
| `Map<K,V>()`      | Create empty typed map |

## Math

| Zinc            | C# equivalent |
|-------------------|---------------|
| `abs(x)`          | `Math.Abs(x)` |
| `sqrt(x)`         | `Math.Sqrt(x)` |
| `pow(x, y)`       | `Math.Pow(x, y)` |
| `floor(x)`        | `Math.Floor(x)` |
| `ceil(x)`         | `Math.Ceiling(x)` |
| `round(x)`        | `Math.Round(x)` |
| `max(a, b)`       | `Math.Max(a, b)` |
| `min(a, b)`       | `Math.Min(a, b)` |

## Strings

| Zinc            | Description |
|-------------------|-------------|
| `s.upper()` / `s.lower()` | Convert case |
| `s.contains(x)`   | Check if string contains substring |
| `s.startsWith(x)` / `s.endsWith(x)` | Check prefix / suffix |
| `s.trim()`         | Remove leading/trailing whitespace |
| `s.split(sep)`     | Split into list |
| `s.replace(a, b)`  | Replace all occurrences |
| `list.join(sep)`   | Join elements into string |
| `sprintf(fmt, ...)` | Format string |

> **Note:** `sprintf` uses C#-style placeholders (`{0}`, `{1}`).

## JSON

| Zinc            | C# equivalent | Notes |
|-------------------|---------------|-------|
| `jsonEncode(val)` | `JsonSerializer.Serialize(val)` | Returns `String` |
| `jsonDecode(str)` | `JsonSerializer.Deserialize<object>(str)` | Returns `Map<String, Any>` |
| `jsonDecode<T>(str)` | `JsonSerializer.Deserialize<T>(str)` | Returns `T` |

## HTTP

| Zinc            | C# equivalent | Notes |
|-------------------|---------------|-------|
| `httpGet(url)`    | `new HttpClient().GetStringAsync(url).Result` | **Failable** — use `or { }` to handle errors |

## Environment & Time

| Zinc            | C# equivalent |
|-------------------|---------------|
| `getEnv(key)`     | `Environment.GetEnvironmentVariable(key)` |
| `setEnv(key, val)` | `Environment.SetEnvironmentVariable(key, val)` |
| `now()`           | `DateTime.Now.ToString()` |
| `sleep(ms)`       | `Thread.Sleep(ms)` |

## Control

| Zinc            | C# equivalent |
|-------------------|---------------|
| `panic(msg)`      | `throw new Exception(msg)` |
| `exit(code)`      | `Environment.Exit(code)` |

## Error Handling with Failable Builtins

Failable builtins (`readFile`, `writeFile`, `httpGet`) can fail at runtime. Use `or { }` to handle errors — the `err` variable is automatically available inside the handler:

```zinc
// Read a file with error handling
var content = readFile("data.txt") or {
    print("Error: {err}")
    exit(1)
}
print(content)

// Write a file with error handling
writeFile("output.txt", "hello") or {
    print("Write failed: {err}")
}

// HTTP request with error handling
var body = httpGet("https://api.example.com/data") or {
    print("Request failed: {err}")
    exit(1)
}
```

`or { }` maps to `try/catch (Exception)` — the `err` variable receives the exception message.

If the handler ends with `exit()` or `panic()`, the error is not re-thrown. Otherwise, the error auto-propagates after the handler runs.

> See [Error Handling](error-handling.md) for the full error handling guide.
