# Known Issues — Compiler Bugs and Missing Features

> **Last updated**: 2026-03-28
> **Found during**: Python backend Phase 3 (concurrency cleanup)

## Java Transformer Bugs

### ~~1. Bounded parallel for~~ — REMOVED (parallel for removed from language)

### 2. return Error() inside spawn → unreachable statement
**Severity**: Bug
**Location**: `Transformer.java` — `transformSpawn()`

When a spawn block contains `return Error("msg")`, the Transformer generates:
```java
throw new RuntimeException("msg");
_f.complete(null);  // unreachable — javac error
```
The `_f.complete(null)` should not be emitted after a `throw`.

### ~~3. Script-mode variables not effectively final for lambdas~~ — FIXED (5a6993f)

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

### ~~6. Concurrent result binding~~ — REMOVED (concurrent block removed from language)

### 7. No formal Zinc stdlib
**Severity**: Design gap

Functions like `sleep()`, `parseInt()`, `print()` are mapped ad-hoc in the Transformer and PythonEmitter via if-chains. No formal stdlib definition that both backends share.

**Impact**: Adding new stdlib functions requires editing both Transformer.java and PythonEmitter.java.
**Fix**: Define a `ZincStdlib` class/config that declares functions with their mappings per target. Both emitters read from it. Part of the TargetRuntime evolution.

## Python Emitter Gaps

### ~~8-10. Parallel for, concurrent, timeout stubs~~ — REMOVED (features removed from language)

### ~~11. No zinc_runtime.py concurrency primitives~~ — FIXED
ZincFuture, ZincChannel, zinc_sleep added to zinc_runtime.py.

### ~~12. sleep() not mapped in PythonEmitter~~ — FIXED
`sleep(ms)` now emits `zinc_sleep(ms)` which converts ms to seconds.

### ~~13. Channel type not mapped in PythonEmitter~~ — FIXED
`new Channel(10)` now emits `ZincChannel(10)` via mapTypeName on constructor calls.

## Roadmap

### 14. Zinc error line numbers
**Severity**: Feature
**Priority**: High

Compiler errors should reference the `.zn` source file and line number. Currently errors lack source location context, making debugging difficult.

### 15. Cross-file type checking for Python
**Severity**: Feature
**Priority**: High

Python backend has no type info shared between files. When file A imports a class from file B, the emitter has no knowledge of B's field types or method signatures.

### 16. Native binary via Nuitka/PyInstaller
**Severity**: Feature
**Priority**: Medium

`zinc build --python --native` — compile Python output to a standalone binary. Evaluate Nuitka (ahead-of-time compilation) and PyInstaller (bundled interpreter). Nuitka preferred for performance.

### 17. Docker support for Python
**Severity**: Feature
**Priority**: Low (trivial once 14-16 are done)

`zinc build --python --docker` — generate a Dockerfile for Python apps. Java backend already has Docker support via `BuildTools.buildDocker()`.

### 18. Fix Java stdlib — Math.max/min stream method collision
**Severity**: Bug
**Priority**: Medium

`Math.max(a, b)` and `Math.min(a, b)` are misidentified as stream methods in `TransformExpr.isStreamMethod()`, causing incorrect `.stream().max()` wrapping. Also: clean up remaining hardcoded method mappings in TransformExpr to match the declarative approach used on the Python side (PythonStdlibMapping).

## Status

| # | Issue | Severity | Status |
|---|-------|----------|--------|
| 1 | Bounded parallel for bug | Bug | **REMOVED** — parallel for removed to simplify compiler |
| 2 | return Error in spawn | Bug | **FIXED** (4c3df27) — skip _f.complete after throw |
| 3 | Effectively final capture | Bug | **FIXED** (5a6993f) — Object[]/int[] holder wrapping |
| 4 | sortBy(it) Comparator | Bug | **FIXED** (4c3df27) — detect identity, use natural order |
| 5 | lock keyword | Feature | **FIXED** (f66b2e3) — LOCK token, LockStmt AST, parser, both backends |
| 6 | Concurrent result binding | Feature | **REMOVED** — concurrent block removed to simplify compiler |
| 7 | Formal stdlib | Design | OPEN |
| 8-10 | Python concurrency stubs | Phase 3 | **REMOVED** — parallel for, concurrent, timeout removed from both backends |
| 11 | zinc_runtime.py primitives | Phase 3 | **FIXED** — ZincFuture, ZincChannel, zinc_sleep |
| 12 | sleep() mapping | Phase 3 | **FIXED** — sleep(ms) → zinc_sleep(ms) |
| 13 | Channel mapping | Phase 3 | **FIXED** — new Channel(n) → ZincChannel(n) |
| 14 | Zinc error line numbers | Feature | **FIXED** — filename:line:col error format, line info on key Expr nodes |
| 15 | Cross-file type checking (Python) | Feature | **FIXED** — TypeRegistry, two-pass compilation on both backends |
| 16 | Native binary (Nuitka/PyInstaller) | Feature | TODO |
| 17 | Docker for Python | Feature | TODO |
| 18 | Java stdlib Math.max/min bug | Bug | **FIXED** — JavaStdlibMapping, static calls bypass stream dispatch |
