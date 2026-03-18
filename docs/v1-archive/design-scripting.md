# Design: Scripting & Automation

Make Zinc productive for quick scripts, automation, prototyping, and experimentation.

**Goal:** A developer should be able to reach for Zinc instead of Bash/Python for everyday tasks — file manipulation, CLI tools, data processing, automation — with the bonus of a fast native binary.

---

## 1. Script Mode (Implicit Main)

**Problem:** Every Zinc program requires a `main() { }` wrapper. For quick scripts, this is ceremony.

**Solution:** If a `.zn` file has no `main()` function, top-level statements run as an implicit main.

```zinc
// hello.zn — no main() needed
var name = args.first() ?? "world"
print("Hello, {name}!")
```

Transpiles to:

```csharp
public class Program {
    public static void Main(string[] args) {
        var name = args.Length > 0 ? args[0] : "world";
        Console.WriteLine($"Hello, {name}!");
    }
}
```

**Rules:**
- If `main()` exists, use it (backward compatible)
- If no `main()` exists, wrap all top-level statements in an implicit main
- Top-level `data`, `class`, `enum`, `interface`, and function declarations remain outside main (same as today)
- Only applies to single-file mode (`zinc <file.zn>`) — projects require `main()`

**CLI integration:**

```bash
zinc run script.zn              # transpile + compile + run
zinc run script.zn -- arg1 arg2 # pass args after --
```

---

## 2. Shebang Support

Run `.zn` files directly from the shell.

```zinc
#!/usr/bin/env zinc run
var files = listDir(".") or { print(err); exit(1) }
files.ForEach { print(it) }
```

```bash
chmod +x list.zn
./list.zn
```

**Implementation:** The lexer skips lines starting with `#!`. The `zinc run` command accepts a file path as first argument.

---

## 3. Command-Line Arguments

**`args`** — a built-in `List<String>` available in `main()` and script mode.

```zinc
main() {
    if args.Count() < 2 {
        print("usage: tool <command> [options]")
        exit(1)
    }
    var command = args[1]
    match command {
        case "greet" -> print("Hello!")
        case "count" -> print("Args: {args.Count()}")
        case _ -> print("Unknown command: {command}")
    }
}
```

**C# mapping:**
- `args` → the `string[] args` parameter (already generated but unused)
- Exposed as `List<string>` via `new List<string>(args)`
- `args[0]` is the program name, `args[1]` is the first user argument

---

## 4. Shell Command Execution

### `exec(cmd)` — Run a command, get output

Failable. Returns the command's stdout as a `String`.

```zinc
var output = exec("git status") or {
    print("git failed: {err}")
    exit(1)
}
print(output)
```

**C# mapping:**

```csharp
// exec("git status") →
var __proc = System.Diagnostics.Process.Start(new ProcessStartInfo {
    FileName = IsWindows ? "cmd" : "/bin/sh",
    Arguments = IsWindows ? "/c git status" : "-c \"git status\"",
    RedirectStandardOutput = true,
    RedirectStandardError = true,
    UseShellExecute = false,
    CreateNoWindow = true
});
__proc.WaitForExit();
if (__proc.ExitCode != 0) throw new Exception(__proc.StandardError.ReadToEnd());
var output = __proc.StandardOutput.ReadToEnd();
```

### `execSilent(cmd)` — Run a command, ignore output

Failable. Returns nothing — just runs the command. Useful for side effects.

```zinc
execSilent("docker compose up -d") or {
    print("Failed to start containers: {err}")
    exit(1)
}
```

### `execCode(cmd)` — Run a command, get exit code

Non-failable. Returns the `Int` exit code. Useful for conditional logic.

```zinc
var code = execCode("ping -c 1 google.com")
if code == 0 {
    print("Network is up")
} else {
    print("Network is down")
}
```

**C# mapping:** Same as `exec()` but returns `__proc.ExitCode` instead of stdout.

---

## 5. File System Operations

### `fileExists(path)` → `Bool`

```zinc
if fileExists("config.json") {
    var config = readFile("config.json") or { exit(1) }
    print(config)
}
```

**C# mapping:** `File.Exists(path)`

### `dirExists(path)` → `Bool`

```zinc
if !dirExists("output") {
    createDir("output")
}
```

**C# mapping:** `Directory.Exists(path)`

### `listDir(path)` → `List<String>`, failable

Returns file and directory names (not full paths) in the given directory.

```zinc
var entries = listDir(".") or { print(err); exit(1) }
var znFiles = entries.Where { it.endsWith(".zn") }
znFiles.ForEach { print(it) }
```

**C# mapping:** `Directory.GetFileSystemEntries(path).Select(Path.GetFileName).ToList()`

### `createDir(path)` — failable

Creates directory and all parent directories.

```zinc
createDir("output/reports/2026") or { print(err); exit(1) }
```

**C# mapping:** `Directory.CreateDirectory(path)`

### `deleteFile(path)` — failable

```zinc
deleteFile("temp.txt") or { print("cleanup failed: {err}") }
```

**C# mapping:** `File.Delete(path)`

### `copyFile(src, dest)` — failable

```zinc
copyFile("template.txt", "output.txt") or { print(err); exit(1) }
```

**C# mapping:** `File.Copy(src, dest, overwrite: true)`

### `moveFile(src, dest)` — failable

```zinc
moveFile("draft.txt", "final.txt") or { print(err); exit(1) }
```

**C# mapping:** `File.Move(src, dest, overwrite: true)`

### `appendFile(path, content)` — failable

```zinc
appendFile("log.txt", "{now()} — task complete\n") or { print(err) }
```

**C# mapping:** `File.AppendAllText(path, content)`

### `fileSize(path)` → `Int`, failable

```zinc
var size = fileSize("data.bin") or { print(err); exit(1) }
print("File is {size} bytes")
```

**C# mapping:** `new FileInfo(path).Length`

---

## 6. Path Utilities

### `pathJoin(parts...)` → `String`

```zinc
var configPath = pathJoin(getEnv("HOME"), ".config", "myapp", "settings.json")
```

**C# mapping:** `Path.Combine(parts)`

### `pathDir(path)` → `String`

```zinc
var dir = pathDir("/home/user/file.txt")  // "/home/user"
```

**C# mapping:** `Path.GetDirectoryName(path)`

### `pathBase(path)` → `String`

```zinc
var name = pathBase("/home/user/file.txt")  // "file.txt"
```

**C# mapping:** `Path.GetFileName(path)`

### `pathExt(path)` → `String`

```zinc
var ext = pathExt("photo.jpg")  // ".jpg"
```

**C# mapping:** `Path.GetExtension(path)`

### `cwd()` → `String`

```zinc
print("Running from: {cwd()}")
```

**C# mapping:** `Directory.GetCurrentDirectory()`

---

## 7. Standard Input (Piping)

### `readStdin()` → `String`

Reads all of stdin. Enables Unix piping.

```zinc
// cat data.txt | zinc run filter.zn
var input = readStdin()
var lines = input.split("\n")
lines.Where { it.contains("ERROR") }
     .ForEach { print(it) }
```

**C# mapping:** `Console.In.ReadToEnd()`

### `stdinLines()` → `List<String>`

Convenience — reads stdin and splits by newline.

```zinc
// ls | zinc run count.zn
var files = stdinLines()
print("Count: {files.Count()}")
```

**C# mapping:** reads line-by-line via `Console.ReadLine()` into a list.

### Pipe detection

```zinc
// hasStdin() → Bool — check if stdin has data (not a TTY)
if hasStdin() {
    var data = readStdin()
    processData(data)
} else {
    print("usage: echo data | tool")
}
```

**C# mapping:** `Console.IsInputRedirected`

---

## 8. HTTP Client (Extended)

Expand beyond `httpGet` for real-world scripting.

### `httpPost(url, body)` → `String`, failable

```zinc
var response = httpPost("https://api.example.com/data", jsonEncode(payload)) or {
    print("POST failed: {err}")
    exit(1)
}
```

### `httpPut(url, body)` → `String`, failable

### `httpDelete(url)` → `String`, failable

### `httpRequest(method, url, headers, body)` → `String`, failable

Full-control escape hatch with headers map.

```zinc
var headers = {"Authorization": "Bearer {token}", "Content-Type": "application/json"}
var resp = httpRequest("POST", "https://api.example.com/items", headers, jsonEncode(item)) or {
    print(err)
    exit(1)
}
var result = jsonDecode(resp)
```

**C# mapping:** `HttpClient` with `HttpRequestMessage`.

---

## 9. Regex

### `regexMatch(pattern, text)` → `Bool`

```zinc
if regexMatch("^\\d{3}-\\d{4}$", phone) {
    print("Valid phone number")
}
```

**C# mapping:** `Regex.IsMatch(text, pattern)`

### `regexFind(pattern, text)` → `List<String>`

Returns all matches.

```zinc
var emails = regexFind("[\\w.]+@[\\w.]+", fileContent)
emails.ForEach { print(it) }
```

**C# mapping:** `Regex.Matches(text, pattern).Select(m => m.Value).ToList()`

### `regexReplace(pattern, text, replacement)` → `String`

```zinc
var clean = regexReplace("\\s+", rawText, " ")
```

**C# mapping:** `Regex.Replace(text, pattern, replacement)`

---

## 10. Date & Time

### `now()` — already exists, returns formatted string

### `timestamp()` → `Int`

Unix epoch milliseconds. Useful for timing and unique IDs.

```zinc
var start = timestamp()
// ... do work ...
var elapsed = timestamp() - start
print("Took {elapsed}ms")
```

**C# mapping:** `DateTimeOffset.UtcNow.ToUnixTimeMilliseconds()`

### `formatDate(format)` → `String`

```zinc
var today = formatDate("yyyy-MM-dd")  // "2026-03-17"
var log = formatDate("HH:mm:ss")      // "14:30:00"
```

**C# mapping:** `DateTime.Now.ToString(format)`

---

## 11. Random

### `random()` → `Float`

Returns a random float in [0.0, 1.0).

```zinc
if random() > 0.5 { print("heads") } else { print("tails") }
```

**C# mapping:** `Random.Shared.NextDouble()`

### `randomInt(min, max)` → `Int`

Returns a random integer in [min, max].

```zinc
var roll = randomInt(1, 6)
print("You rolled a {roll}")
```

**C# mapping:** `Random.Shared.Next(min, max + 1)`

---

## 12. Convenience Builtins

### `prompt(message)` → `String`

Interactive input with a message. Wraps `readLine()` with a print.

```zinc
var name = prompt("What is your name? ")
print("Hello, {name}!")
```

**C# mapping:**

```csharp
Console.Write(message);
var __input = Console.ReadLine() ?? "";
```

### `confirm(message)` → `Bool`

Yes/no prompt for interactive scripts.

```zinc
if confirm("Delete all temp files?") {
    listDir("tmp") or { exit(1) }
        .ForEach { deleteFile(pathJoin("tmp", it)) or {} }
    print("Done.")
}
```

**C# mapping:**

```csharp
Console.Write(message + " [y/N] ");
var __answer = (Console.ReadLine() ?? "").Trim().ToLower();
var __result = __answer == "y" || __answer == "yes";
```

---

## Summary: New Builtins

| Category | Functions | Failable? |
|----------|-----------|-----------|
| **Args** | `args` | No |
| **Exec** | `exec(cmd)`, `execSilent(cmd)`, `execCode(cmd)` | exec/execSilent yes, execCode no |
| **File System** | `fileExists(p)`, `dirExists(p)`, `listDir(p)`, `createDir(p)`, `deleteFile(p)`, `copyFile(s,d)`, `moveFile(s,d)`, `appendFile(p,c)`, `fileSize(p)` | listDir/createDir/delete/copy/move/append/fileSize yes |
| **Paths** | `pathJoin(...)`, `pathDir(p)`, `pathBase(p)`, `pathExt(p)`, `cwd()` | No |
| **Stdin** | `readStdin()`, `stdinLines()`, `hasStdin()` | No |
| **HTTP** | `httpPost(u,b)`, `httpPut(u,b)`, `httpDelete(u)`, `httpRequest(m,u,h,b)` | Yes |
| **Regex** | `regexMatch(p,t)`, `regexFind(p,t)`, `regexReplace(p,t,r)` | No |
| **Time** | `timestamp()`, `formatDate(fmt)` | No |
| **Random** | `random()`, `randomInt(min,max)` | No |
| **Interactive** | `prompt(msg)`, `confirm(msg)` | No |

**Total: ~30 new builtins + script mode + shebang support**

---

## Implementation Order

### Phase 1 — Core Scripting (P2 scope)
1. `args` — expose command-line arguments
2. `exec(cmd)`, `execSilent(cmd)`, `execCode(cmd)` — shell execution
3. `fileExists(path)`, `dirExists(path)`, `listDir(path)` — filesystem checks

### Phase 2 — Script Ergonomics
4. Script mode (implicit main)
5. Shebang support
6. `readStdin()`, `stdinLines()`, `hasStdin()` — piping
7. `prompt(msg)`, `confirm(msg)` — interactive input

### Phase 3 — File Operations
8. `createDir`, `deleteFile`, `copyFile`, `moveFile`, `appendFile`, `fileSize`
9. `pathJoin`, `pathDir`, `pathBase`, `pathExt`, `cwd`

### Phase 4 — Extended Builtins
10. `httpPost`, `httpPut`, `httpDelete`, `httpRequest`
11. `regexMatch`, `regexFind`, `regexReplace`
12. `timestamp`, `formatDate`, `random`, `randomInt`

---

## Example: Real-World Script

A deploy script that shows many features working together:

```zinc
#!/usr/bin/env zinc run

// deploy.zn — build and deploy the app

if args.Count() < 2 {
    print("usage: deploy <environment>")
    exit(1)
}

var env = args[1]
if env != "staging" && env != "production" {
    print("Invalid environment: {env}. Use 'staging' or 'production'.")
    exit(1)
}

if env == "production" {
    if !confirm("Deploy to PRODUCTION?") {
        print("Aborted.")
        exit(0)
    }
}

print("[{formatDate("HH:mm:ss")}] Building...")
var buildOutput = exec("zinc build --release") or {
    print("Build failed: {err}")
    exit(1)
}
print(buildOutput)

print("[{formatDate("HH:mm:ss")}] Running tests...")
var testResult = execCode("zinc test")
if testResult != 0 {
    print("Tests failed. Aborting deploy.")
    exit(1)
}

print("[{formatDate("HH:mm:ss")}] Deploying to {env}...")
exec("scp build/app {env}-server:/opt/app/") or {
    print("Deploy failed: {err}")
    exit(1)
}

appendFile("deploy.log", "{formatDate("yyyy-MM-dd HH:mm:ss")} — deployed to {env}\n") or {}
print("Done! Deployed to {env}.")
```

## Example: Data Processing Pipeline

```zinc
#!/usr/bin/env zinc run

// process.zn — filter and transform CSV from stdin
// Usage: cat users.csv | zinc run process.zn

var lines = stdinLines()
var header = lines[0]
var rows = lines[1:]

print("name,email")
rows.Where { it.split(",").Count() >= 3 }
    .Where { it.split(",")[2].trim() == "active" }
    .Select { var cols = it.split(","); "{cols[0]},{cols[1]}" }
    .ForEach { print(it) }
```

## Example: Quick Prototype / Exploration

```zinc
// explore-api.zn — quickly test an API

var baseUrl = "https://jsonplaceholder.typicode.com"

var users = jsonDecode(httpGet("{baseUrl}/users") or { print(err); exit(1) })
print("Found {users.size()} users")

var posts = jsonDecode(httpGet("{baseUrl}/posts") or { print(err); exit(1) })
print("Found {posts.size()} posts")

// Find prolific authors
var authorCounts = Map<String, Int>()
// ... process and explore data interactively
```
