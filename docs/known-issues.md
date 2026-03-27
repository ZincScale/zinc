# Known Issues — Compiler Bugs and Missing Features

> **Last updated**: 2026-03-27
> **Found during**: Python backend Phase 3 (concurrency cleanup)

## Java Transformer Bugs

### 1. Bounded parallel for loses items
**Severity**: Bug
**Location**: `Transformer.java` — `transformParallelFor()` with `max > 0`

`parallel(max: 2) for i in numbers { items.add(i) }` produces sum of 11 instead of 15. The `StructuredTaskScope` + `Semaphore` pattern drops items — likely the semaphore prevents some forks from executing before `_scope.join()` returns.

**Reproduction**: `concurrency.zn` — "parallel bounded sum" test
**Expected**: 15, **Actual**: 11

### 2. return Error() inside spawn → unreachable statement
**Severity**: Bug
**Location**: `Transformer.java` — `transformSpawn()`

When a spawn block contains `return Error("msg")`, the Transformer generates:
```java
throw new RuntimeException("msg");
_f.complete(null);  // unreachable — javac error
```
The `_f.complete(null)` should not be emitted after a `throw`.

### 3. Script-mode variables not effectively final for lambdas
**Severity**: Bug
**Location**: `Transformer.java` — script mode variable capture

Variables declared in script mode (`var spawnResult = -1`) and mutated inside a spawn/parallel lambda fail:
```java
spawnResult = -1;  // error: must be final or effectively final
```
The Transformer should wrap mutable captured variables in `AtomicReference<>` or an array holder (`var _holder = new Object[]{initialValue}`) when they're captured by lambdas.

### 4. sortBy(it) generates invalid Comparator
**Severity**: Bug
**Location**: `Transformer.java` — `transformStreamChain()` sortBy handling

`items.sortBy(it)` generates `Comparator.comparing((_it) -> _it)` which fails type inference. Should emit `Comparator.naturalOrder()` when the key extractor is identity, or `(Comparable x) -> x` with explicit cast.

## Missing Language Features

### 5. `lock` keyword not implemented
**Severity**: Feature gap
**Location**: Lexer, Parser, AST

Designed in `design-zinc-concurrency.md` as:
```zinc
lock mu {
    counter = counter + 1
}
```
Not in the lexer keyword list, no AST node, no parser rule. Concurrency tests can't use Zinc-native locking.

**Impact**: Shared mutable state in parallel for requires workarounds (thread-safe collection ops).
**Fix**: Add `LOCK` token, `LockStmt(Expr mutex, BlockStmt body)` AST node, parser rule, Transformer mapping to `ReentrantLock.lock()/unlock()` try-finally, PythonEmitter mapping to `threading.Lock()` context manager.

### 6. Concurrent result binding not implemented
**Severity**: Feature gap
**Location**: Parser — `parseConcurrentStmt()`

Designed syntax:
```zinc
var (user, orders, prefs) = concurrent {
    fetchUser(id)
    fetchOrders(id)
    fetchPrefs(id)
}
```
The parser creates `ConcurrentStmt` with `names = List.of()` — the result-binding `var (a, b) = concurrent { }` syntax isn't parsed. The `ConcurrentStmt` AST record has the `names` field but it's always empty.

**Impact**: Can't collect results from concurrent fan-out. Workaround: use side effects (mutate shared state) or return from functions.

### 7. No formal Zinc stdlib
**Severity**: Design gap

Functions like `sleep()`, `parseInt()`, `print()` are mapped ad-hoc in the Transformer and PythonEmitter via if-chains. No formal stdlib definition that both backends share.

**Impact**: Adding new stdlib functions requires editing both Transformer.java and PythonEmitter.java.
**Fix**: Define a `ZincStdlib` class/config that declares functions with their mappings per target. Both emitters read from it. Part of the TargetRuntime evolution.

## Python Emitter Gaps

### 8. Spawn emission is stub
**Severity**: Phase 3 incomplete
**Location**: `PythonEmitter.java` — `emitSpawnExpr()`

Emits `_executor.submit(lambda: None)` placeholder. Needs real `threading.Thread` or `ThreadPoolExecutor` emission with the `ZincScope` lifecycle manager.

### 9. Parallel for emission is stub
**Severity**: Phase 3 incomplete
**Location**: `PythonEmitter.java` — `emitParallelForStmt()`

Emits a simplified `executor.map()` pattern that doesn't work correctly. Needs `ZincScope` with proper structured lifecycle.

### 10. Concurrent emission is stub
**Severity**: Phase 3 incomplete
**Location**: `PythonEmitter.java` — `emitConcurrentStmt()`

Emits basic future submission without structured cancellation. Needs `ZincScope` integration.

### 11. No zinc_runtime.py concurrency primitives
**Severity**: Phase 3 incomplete
**Location**: `test/python/zinc_runtime.py`

Only `ZincError` exists. Missing:
- `ZincScope` — structured concurrency scope (executor + future tracking + cancellation)
- `ZincChannel` — `queue.Queue` wrapper with `close()` and iteration
- `ZincTimeout` — executor + `future.result(timeout=N)` + cancel
- `zinc_main()` — entry point wrapper with signal handlers + scope cleanup
- `sleep()` — mapped to `time.sleep(ms / 1000)`

### 12. sleep() not mapped in PythonEmitter
**Severity**: Phase 3 incomplete
**Location**: `PythonEmitter.java`

`sleep(100)` emits as-is. Should emit `time.sleep(0.1)` (Python uses seconds, Zinc uses milliseconds).

### 13. Channel type not mapped in PythonEmitter
**Severity**: Phase 3 incomplete
**Location**: `PythonEmitter.java`

`new Channel(10)` emits as `Channel(10)`. Should emit `ZincChannel(maxsize=10)` from zinc_runtime.

## Status

| # | Issue | Severity | Status |
|---|-------|----------|--------|
| 1 | Bounded parallel for bug | Bug | **FIXED** (4c3df27) — semaphore release moved inside forked lambda |
| 2 | return Error in spawn | Bug | **FIXED** (4c3df27) — skip _f.complete after throw |
| 3 | Effectively final capture | Bug | OPEN — needs AtomicReference/holder wrapping |
| 4 | sortBy(it) Comparator | Bug | **FIXED** (4c3df27) — detect identity, use natural order |
| 5 | lock keyword | Feature | **FIXED** (f66b2e3) — LOCK token, LockStmt AST, parser, both backends |
| 6 | Concurrent result binding | Feature | OPEN |
| 7 | Formal stdlib | Design | OPEN |
| 8-13 | Python concurrency | Phase 3 | OPEN — stubs in PythonEmitter, need zinc_runtime.py |
