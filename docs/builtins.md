# Built-in Functions

Zinc provides a set of built-in functions that map directly to Go standard library calls. No imports needed — the transpiler adds them automatically.

## I/O

| Zinc            | Go equivalent              | Notes |
|-------------------|----------------------------|-------|
| `print(x)`        | `fmt.Println(x)`           | |
| `printf(fmt, ...)` | `fmt.Printf(fmt, ...)`   | |
| `readLine()`      | `bufio.NewReader(os.Stdin).ReadString('\n')` | |
| `readFile(path)`  | `os.ReadFile(path)`        | **Failable** — errors auto-propagate |
| `writeFile(path, content)` | `os.WriteFile(path, []byte(content), 0644)` | **Failable** — errors auto-propagate |

## Type Conversions

| Zinc            | Go equivalent              | Notes |
|-------------------|----------------------------|-------|
| `toString(x)`     | `fmt.Sprintf("%v", x)`     | |
| `parseInt(s)`     | `strconv.Atoi(s)`          | |
| `toInt(s)`        | `strconv.Atoi(s)`          | Alias for `parseInt` |
| `parseFloat(s)`   | `strconv.ParseFloat(s,64)` | |
| `toFloat(s)`      | `strconv.ParseFloat(s,64)` | Alias for `parseFloat` |
| `toBool(s)`       | `strconv.ParseBool(s)`     | |
| `typeOf(x)`       | `fmt.Sprintf("%T", x)`     | |

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
| `List<T>.new()`   | `[]T{}`                    | |
| `Map<K,V>.new()`  | `map[K]V{}`                | |
| `Chan<T>.new(n)`  | `make(chan T, n)`           | |
| `ch.send(val)`    | `ch <- val`                 | Send value to channel |
| `ch.receive()`    | `<-ch`                      | Receive value from channel |

## Math

| Zinc            | Go equivalent              |
|-------------------|----------------------------|
| `abs(x)`          | `math.Abs(x)`              |
| `sqrt(x)`         | `math.Sqrt(x)`             |
| `pow(x, y)`       | `math.Pow(x, y)`           |
| `floor(x)` / `ceil(x)` / `round(x)` | `math.Floor` / `Ceil` / `Round` |
| `max(a, b)` / `min(a, b)` | `math.Max` / `math.Min` |

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

## JSON

| Zinc            | Go equivalent              | Notes |
|-------------------|----------------------------|-------|
| `jsonEncode(val)` | `json.Marshal(val)`        | Returns `String` |
| `jsonDecode(str)` | `json.Unmarshal(str, &m)`  | Returns `Map<String, Any>` |
| `jsonDecode<T>(str)` | `json.Unmarshal(str, &target)` | Returns `T` |

## HTTP

| Zinc            | Go equivalent              | Notes |
|-------------------|----------------------------|-------|
| `httpGet(url)`    | `http.Get(url)` + read body | **Failable** — errors auto-propagate |

## Environment & Time

| Zinc            | Go equivalent              |
|-------------------|----------------------------|
| `getEnv(key)`     | `os.Getenv(key)`           |
| `setEnv(key, val)` | `os.Setenv(key, val)`    |
| `now()`           | `time.Now()`               |
| `sleep(ms)`       | `time.Sleep(ms * time.Millisecond)` |

## Control

| Zinc            | Go equivalent              |
|-------------------|----------------------------|
| `panic(msg)`      | `panic(msg)`               |
| `exit(code)`      | `os.Exit(code)`            |
