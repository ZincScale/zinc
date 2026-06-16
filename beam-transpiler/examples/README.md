# Examples — a reading path

These are the programs the test suite runs (`../e2e.sh`): each is transpiled, compiled with
`erlc`, run on a real BEAM, and its output asserted — so every one is known-good, runnable
zinc. New here? Read them roughly top to bottom; each section builds on the last. Pair them
with **[the guide](../docs/guide.md)** (section links below).

Run any of them by copying the file into a `zc new` project's `src/Main.zinc`, or read the
expected output in `../e2e.sh`.

## 1. Language basics ([guide §1–2, §4](../docs/guide.md))
| file | shows |
|------|-------|
| `sum_evens` | a `for` loop, mutation, `%`, an accumulator |
| `countdown` | a `while` loop with threaded mutable state |
| `first_over` | early `return` out of a for-each loop |
| `bools` | `&&` / `\|\|` / `!` and precedence (true *and* false results) |
| `elseif` | `if` / `else if` / `else` — every branch |
| `breakcont` | `break` and `continue` (continue still runs the for-update) |
| `floats` | int vs float division (`/` → `div` vs float) |
| `casts` | `(int)`/`(long)`/`(double)` casts + `Long.parseLong`/`Double.parseDouble` |
| `ternary` | the `?:` operator, nested |
| `strings` / `javastrings` | string literals, escapes, and the `String` facade |

## 2. Values & collections ([guide §2–3](../docs/guide.md))
| file | shows |
|------|-------|
| `structs` | `record`s (immutable, `p.x()`) |
| `switchenum` | `enum`s + arrow `switch` |
| `arrays` | `int[]` fixed-size arrays + index assignment |
| `arraylist` | `ArrayList<T>` (build/index; the O(n²)-regression guard) |
| `hashmap` / `javacollections` | `Map`/`HashMap` and the java.util facade |
| `mapiter` | walking a map: `entrySet()` for-each + `forEach` |
| `lambdas` | lambdas / higher-order functions |
| `interfaces` | interfaces + instance classes (dynamic `'$class'` dispatch) + SAM lambdas |
| `mathstr` | `Math.*`, `String.join/format`, `List`/`Map` facade methods |
| `warn_iget` | the O(n)-cost warning on `List.get`/`size` (with file:line) |
| `atoms_tuples` | `Tag.of` atoms, `Tuple`, `Erlang.ok` (the value-or-throw idiom) |
| `modifiers` | `private`/`final`/`static` — enforced, not decorative |

## 3. Errors & the failure ladder ([guide §4–5](../docs/guide.md))
| file | shows |
|------|-------|
| `trycatch` | `try`/`catch` — including the *transactional* revert |
| `exceptions` | typed exceptions, the catch-all, relay from an actor |
| `guards` | runtime type guards at the unknown→known boundary |

## 4. Actors & supervision — the differentiator ([guide §6](../docs/guide.md))
| file | shows |
|------|-------|
| `actor_counter` | a basic actor: `void` ⇒ cast, typed ⇒ call |
| `actor_args` | constructor args + passing handles |
| `actor_children` | composition = supervision (a nested child crashes & restarts) |
| `actor_selfheal` | **the headline:** crash → supervisor restart → same handle, fresh state |
| `close` | `public void close()` runs on orderly stop only |

## 5. Standard library ([guide §8](../docs/guide.md))
| file | shows |
|------|-------|
| `logging` | `Log.*` (logger stream) vs `System.out.println` (clean stdout) |
| `json` | derived record codecs + dynamic access; `proc_json` adds `decodeList` |
| `http_client` | the `zinc.http` client (and the exception ladder) |

(The HTTP *server* + `Router` + JSON is the [tutorial](../docs/tutorials.md) and
`../dogfood/webdemo`.)

## 6. Files & streaming, bounded memory ([guide §7](../docs/guide.md))
| file | shows |
|------|-------|
| `fileio` | whole-file read/write, dir ops, `getenv`, `IOException` (small files) |
| `filestream` | scoped streaming read (try-with-resources `Reader`, constant memory) |
| `filewrite` | scoped streaming write (`Writer`) |
| `proc_csv` / `proc_json` / `proc_binary` | end-to-end format processing |
| `httpstream` | streaming an HTTP response body in bounded memory |

## 7. Concurrency: channels & pipelines ([guide §7](../docs/guide.md))
| file | shows |
|------|-------|
| `channel` | `Channel<T>` — bounded backpressure between actors |
| `pipeline` | a 3-stage multi-process pipeline (`FileReader.pump` → transform → `FileWriter.drain`) |

## 8. FFI & projects ([guide §1, §9](../docs/guide.md))
| file | shows |
|------|-------|
| `ffi` | `import erlang.*` — the unchecked basement |
| `tcpserver` | a TCP line server over the `gen_tcp` FFI (acceptor + per-connection actors) |
| `multifile/` | a multi-file project: classes = modules, dirs = packages |

## Negative examples
`neg/` holds programs that **must fail to transpile** — they pin down what the language
*rejects* (type mismatches, modifier misuse, `new` on a pump, ...). Their expected error
messages are in `../e2e.sh` (`wanterr`).
