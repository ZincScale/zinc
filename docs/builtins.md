# Built-in Functions

Zinc provides built-in functions that map to the target backend's standard library. No imports needed — the transpiler adds them automatically.

## I/O

| Zinc            | Go equivalent              | C# equivalent | Notes |
|-------------------|----------------------------|---------------|-------|
| `print(x)`        | `fmt.Println(x)`           | `Console.WriteLine(x)` | |
| `printf(fmt, ...)` | `fmt.Printf(fmt, ...)`   | *(not yet in C#)* | |
| `readLine()`      | `bufio.NewReader(os.Stdin).ReadString('\n')` | `Console.ReadLine()` | |
| `readFile(path)`  | `os.ReadFile(path)`        | `File.ReadAllText(path)` | **Failable** — use `or { }` to handle errors |
| `writeFile(path, content)` | `os.WriteFile(path, []byte(content), 0644)` | `File.WriteAllText(path, content)` | **Failable** — use `or { }` to handle errors |

## Type Conversions

| Zinc            | Go equivalent              | C# equivalent |
|-------------------|----------------------------|---------------|
| `toString(x)`     | `fmt.Sprintf("%v", x)`     | `(x).ToString()` |
| `parseInt(s)`     | `strconv.Atoi(s)`          | `int.Parse(s)` |
| `toInt(s)`        | `strconv.Atoi(s)`          | `int.Parse(s)` |
| `parseFloat(s)`   | `strconv.ParseFloat(s,64)` | `double.Parse(s)` |
| `toFloat(s)`      | `strconv.ParseFloat(s,64)` | `double.Parse(s)` |
| `toBool(s)`       | `strconv.ParseBool(s)`     | `bool.Parse(s)` |
| `typeOf(x)`       | `fmt.Sprintf("%T", x)`     | `(x).GetType().Name` |

## Collections

| Zinc            | Go equivalent              | Notes |
|-------------------|----------------------------|-------|
| `list.add(items...)` | `list = append(list, items...)` | Appends one or more items; supports spread (`other...`) |
| `map.remove(key)` | `delete(map, key)`          | Removes key from map |
| `x.size()`        | `len(x)`                    | Works on lists, maps, strings |
| `list.clone()`    | `append(list[:0:0], list...)`| Deep-copies a list |
| `list.sort()`     | `sort.Slice(list, ...)`    | Sorts list in-place |
| `list.join(sep)`  | `strings.Join(list, sep)`  | Join elements into string |
| `map.keys()`      | *(IIFE collecting keys)*   | Returns list of keys |
| `map.values()`    | *(IIFE collecting values)* | Returns list of values |
| `x[low:high]`       | `x[low:high]`            | Slice with bracket syntax; either bound optional |
| `x.slice(start, end)` | `x[start:end]`         | OO slice method; `end` optional |
| `map.containsKey(k)` | `_, ok := map[k]`      | Returns Bool |
| `List<T>()`       | `[]T{}`                    | |
| `Map<K,V>()`      | `map[K]V{}`                | |
| `Chan<T>(n)`      | `make(chan T, n)`           | |
| `ch.send(val)`    | `ch <- val`                 | Send value to channel |
| `ch.receive()`    | `<-ch`                      | Receive value from channel |

## Math

| Zinc            | Go equivalent              | C# equivalent |
|-------------------|----------------------------|---------------|
| `abs(x)`          | `math.Abs(x)`              | `Math.Abs(x)` |
| `sqrt(x)`         | `math.Sqrt(x)`             | `Math.Sqrt(x)` |
| `pow(x, y)`       | `math.Pow(x, y)`           | `Math.Pow(x, y)` |
| `floor(x)`        | `math.Floor(x)`            | `Math.Floor(x)` |
| `ceil(x)`         | `math.Ceil(x)`             | `Math.Ceiling(x)` |
| `round(x)`        | `math.Round(x)`            | `Math.Round(x)` |
| `max(a, b)`       | `math.Max(a, b)`           | `Math.Max(a, b)` |
| `min(a, b)`       | `math.Min(a, b)`           | `Math.Min(a, b)` |

## Strings

| Zinc            | Go equivalent              |
|-------------------|----------------------------|
| `s.upper()` / `s.lower()` | `strings.ToUpper(s)` / `ToLower(s)` |
| `s.contains(x)`   | `strings.Contains(s, x)`  |
| `s.startsWith(x)` / `s.endsWith(x)` | `strings.HasPrefix(s, x)` / `HasSuffix(s, x)` |
| `s.trim()`         | `strings.TrimSpace(s)`     |
| `s.split(sep)`     | `strings.Split(s, sep)`    |
| `s.replace(a, b)`  | `strings.ReplaceAll(s, a, b)` |
| `list.join(sep)`   | `strings.Join(list, sep)`  |
| `sprintf(fmt, ...)` | `fmt.Sprintf(fmt, ...)`  |

> **Note:** `sprintf` uses Go-style format verbs (`%s`, `%d`) on the Go backend and C#-style placeholders (`{0}`, `{1}`) on the C# backend.

## JSON

| Zinc            | Go equivalent              | C# equivalent | Notes |
|-------------------|----------------------------|---------------|-------|
| `jsonEncode(val)` | `json.Marshal(val)`        | `JsonSerializer.Serialize(val)` | Returns `String` |
| `jsonDecode(str)` | `json.Unmarshal(str, &m)`  | `JsonSerializer.Deserialize<object>(str)` | Returns `Map<String, Any>` |
| `jsonDecode<T>(str)` | `json.Unmarshal(str, &target)` | `JsonSerializer.Deserialize<T>(str)` | Returns `T` |

## HTTP

| Zinc            | Go equivalent              | C# equivalent | Notes |
|-------------------|----------------------------|---------------|-------|
| `httpGet(url)`    | `http.Get(url)` + read body | `new HttpClient().GetStringAsync(url).Result` | **Failable** — use `or { }` to handle errors |

## Environment & Time

| Zinc            | Go equivalent              | C# equivalent |
|-------------------|----------------------------|---------------|
| `getEnv(key)`     | `os.Getenv(key)`           | `Environment.GetEnvironmentVariable(key)` |
| `setEnv(key, val)` | `os.Setenv(key, val)`    | `Environment.SetEnvironmentVariable(key, val)` |
| `now()`           | `time.Now()`               | `DateTime.Now.ToString()` |
| `sleep(ms)`       | `time.Sleep(ms * time.Millisecond)` | `Thread.Sleep(ms)` |

## Control

| Zinc            | Go equivalent              | C# equivalent |
|-------------------|----------------------------|---------------|
| `panic(msg)`      | `panic(msg)`               | `throw new Exception(msg)` |
| `exit(code)`      | `os.Exit(code)`            | `Environment.Exit(code)` |

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

On the **Go backend**, `or { }` maps to Go's `if err != nil { }` pattern.
On the **C# backend**, `or { }` maps to `try/catch (Exception)` — the `err` variable receives the exception message.

If the handler ends with `exit()` or `panic()`, the error is not re-thrown. Otherwise, the error auto-propagates after the handler runs.

> See [Error Handling](error-handling.md) for the full error handling guide.
