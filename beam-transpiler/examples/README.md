# Examples — a reading path

These are the programs the test suite runs (`../e2e.sh`): each is transpiled, compiled with
`erlc`, run on a real BEAM, and its output asserted — so every one is known-good, runnable
zinc. New here? Read them roughly top to bottom; each section builds on the last. Pair them
with **[the guide](../docs/guide.md)** (section links below). The programs live in
[`programs/`](programs); the click-throughs below open each file.

Run any of them by copying the file into a `zc new` project's `src/Main.zinc`, or read the
expected output in `../e2e.sh`.

## 1. Language basics ([guide §1–2, §4](../docs/guide.md))
| file | shows |
|------|-------|
| [`sum_evens`](programs/sum_evens.zinc) | a `for` loop, mutation, `%`, an accumulator |
| [`countdown`](programs/countdown.zinc) | a `while` loop with threaded mutable state |
| [`first_over`](programs/first_over.zinc) | early `return` out of a for-each loop |
| [`bools`](programs/bools.zinc) | `&&` / `\|\|` / `!` and precedence (true *and* false results) |
| [`elseif`](programs/elseif.zinc) | `if` / `else if` / `else` — every branch |
| [`breakcont`](programs/breakcont.zinc) | `break` and `continue` (continue still runs the for-update) |
| [`floats`](programs/floats.zinc) | int vs float division (`/` → `div` vs float) |
| [`casts`](programs/casts.zinc) | `(int)`/`(long)`/`(double)` casts + `Long.parseLong`/`Double.parseDouble` |
| [`ternary`](programs/ternary.zinc) | the `?:` operator, nested |
| [`strings`](programs/strings.zinc) / [`javastrings`](programs/javastrings.zinc) | string literals, escapes, and the `String` facade |

## 2. Values & collections ([guide §2–3](../docs/guide.md))
| file | shows |
|------|-------|
| [`structs`](programs/structs.zinc) | `record`s (immutable, `p.x()`) |
| [`switchenum`](programs/switchenum.zinc) | `enum`s + arrow `switch` |
| [`arrays`](programs/arrays.zinc) | `int[]` fixed-size arrays + index assignment |
| [`arraylist`](programs/arraylist.zinc) | `ArrayList<T>` (build/index; the O(n²)-regression guard) |
| [`hashmap`](programs/hashmap.zinc) / [`javacollections`](programs/javacollections.zinc) | `Map`/`HashMap` and the java.util facade |
| [`mapiter`](programs/mapiter.zinc) | walking a map: `entrySet()` for-each + `forEach` |
| [`lambdas`](programs/lambdas.zinc) | lambdas / higher-order functions |
| [`interfaces`](programs/interfaces.zinc) | interfaces + instance classes (dynamic `'$class'` dispatch) + SAM lambdas |
| [`mathstr`](programs/mathstr.zinc) | `Math.*`, `String.join/format`, `List`/`Map` facade methods |
| [`warn_iget`](programs/warn_iget.zinc) | the O(n)-cost warning on `List.get`/`size` (with file:line) |
| [`atoms_tuples`](programs/atoms_tuples.zinc) | `Tag.of` atoms, `Tuple`, `Erlang.ok` (the value-or-throw idiom) |
| [`modifiers`](programs/modifiers.zinc) | `private`/`final`/`static` — enforced, not decorative |

## 3. Errors & the failure ladder ([guide §4–5](../docs/guide.md))
| file | shows |
|------|-------|
| [`trycatch`](programs/trycatch.zinc) | `try`/`catch` — including the *transactional* revert |
| [`exceptions`](programs/exceptions.zinc) | typed exceptions, the catch-all, relay from an actor |
| [`guards`](programs/guards.zinc) | runtime type guards at the unknown→known boundary |

## 4. Actors & supervision — the differentiator ([guide §6](../docs/guide.md))
| file | shows |
|------|-------|
| [`actor_counter`](programs/actor_counter.zinc) | a basic actor: `void` ⇒ cast, typed ⇒ call |
| [`actor_args`](programs/actor_args.zinc) | constructor args + passing handles |
| [`actor_children`](programs/actor_children.zinc) | composition = supervision (a nested child crashes & restarts) |
| [`actor_selfheal`](programs/actor_selfheal.zinc) | **the headline:** crash → supervisor restart → same handle, fresh state |
| [`close`](programs/close.zinc) | `public void close()` runs on orderly stop only |

## 5. Standard library ([guide §8](../docs/guide.md))
| file | shows |
|------|-------|
| [`logging`](programs/logging.zinc) | `Log.*` (logger stream) vs `System.out.println` (clean stdout) |
| [`json`](programs/json.zinc) | derived record codecs + dynamic access (`proc_json` adds `decodeList`) |
| [`http_client`](programs/http_client.zinc) | the `zinc.http` client builders + the exception ladder (refused connection) |

(The HTTP *server* + `Router` + JSON — and the client streaming a response body into a file
in bounded memory (`openStream`) — is the [tutorial](../docs/tutorials.md) and
[`../dogfood/webdemo`](../dogfood/webdemo): it needs cowboy, which the e2e set has no deps for.)

## 6. Files & streaming, bounded memory ([guide §7](../docs/guide.md))
| file | shows |
|------|-------|
| [`fileio`](programs/fileio.zinc) | whole-file read/write, dir ops, `getenv`, `IOException` (small files) |
| [`filestream`](programs/filestream.zinc) | scoped streaming read (try-with-resources `Reader`, constant memory) |
| [`filewrite`](programs/filewrite.zinc) | scoped streaming write (`Writer`) |
| [`proc_csv`](programs/proc_csv.zinc) / [`proc_json`](programs/proc_json.zinc) / [`proc_binary`](programs/proc_binary.zinc) | end-to-end format processing |

## 7. Concurrency: channels & pipelines ([guide §7](../docs/guide.md))
| file | shows |
|------|-------|
| [`channel`](programs/channel.zinc) | `Channel<T>` — bounded backpressure between actors |
| [`pipeline`](programs/pipeline.zinc) | a 3-stage multi-process pipeline (`FileReader.pump` → transform → `FileWriter.drain`) |

## 8. FFI & projects ([guide §1, §9](../docs/guide.md))
| file | shows |
|------|-------|
| [`ffi`](programs/ffi.zinc) | `import erlang.*` — the unchecked basement |
| [`tcpserver`](programs/tcpserver.zinc) | a TCP line server over the `gen_tcp` FFI (acceptor + per-connection actors) |
| [`multifile/`](programs/multifile) | a multi-file project: classes = modules, dirs = packages |

## Negative examples
[`neg/`](neg) holds programs that **must fail to transpile** — they pin down what the
language *rejects* (type mismatches, modifier misuse, `new` on a pump, ...). Their expected
error messages are in `../e2e.sh` (`wanterr`).
