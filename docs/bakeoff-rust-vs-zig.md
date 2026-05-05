# Zinc Backend Bake-off: Rust vs Zig (with bdwgc)

Hand-written transliteration of three `zinc-go/examples/*.zn` files into what a Zinc→Rust and Zinc→Zig transpiler **would** emit, plus a tiny runtime in each. No real codegen. Both targets link the system Boehm GC (`/lib64/libgc.so.1`) for class-instance allocation.

## Results

Both spikes pass: `hello`, `enums`, `error_return` outputs match `zinc-go/expected/*.txt` (sorted).

| metric | Rust 1.x (release, opt-level=z, lto, strip, panic=abort) | Zig 0.16.0 (ReleaseSmall, -fstrip) |
|---|---|---|
| binary size, stripped | **319,944 B** (~313 KB) | **140,768 B** (~138 KB) |
| cold start, best of 10 | **2.40 – 2.52 ms** | **3.21 – 3.26 ms** |
| build time, 3 bins cold | 4.35 s (single `cargo build --release`) | 11.3 s (3× `zig build-exe`, no shared cache) |
| build time, 3 bins warm | 3.05 s | ~6 s |
| runtime LoC (shared module) | 35 | 42 |
| `hello.zn` per-program LoC | 14 | 14 |
| `enums.zn` per-program LoC | 26 | 30 |
| `error_return.zn` per-program LoC | 73 | 70 |
| total LoC | 151 (+ 3 line build.rs) | 156 |

## Verdict (one sentence each)

**Smallest binary**: Zig wins, 2.3× smaller. **Lower friction in the codegen mapping**: roughly tied — both required identical structural moves (manual vtable for the `error` interface, GC_malloc-allocated structs, parent-fields-first inheritance layout). **Strategic pick**: **Rust**, because the contributor-pool argument the customers gave outweighs Zig's binary-size win; Zig becomes a watch-item until it hits 1.0 and the std API stabilizes.

## Frictions log

### Rust

- `#[link(name = "gc")]` couldn't find `libgc.so` (system has `libgc.so.1` only). Fixed with a 1-line `build.rs` that adds a userland `-L` to a symlink dir. **Real friction** for distributing a Rust target — every customer needs `gc-devel` or equivalent.
- Dead-code warnings on enum variants that aren't used in a particular `.zn` file forced `#[allow(dead_code)]` noise in transpiler output. Mechanical codegen needs to either emit `#[allow(unused)]` per item or `#![allow(dead_code)]` at crate root.
- The borrow checker stayed out of the way because the codegen pattern is "heap-allocate everything via raw pointer, never borrow" — exactly what GC integration requires. If a smarter codegen ever wanted to express ownership, lifetimes would surface.
- `Option<ZError>` for the `(T, error)` tuple is natural and zero-cost.
- 320 KB even at `opt-level="z"` is dominated by libstd formatting (`format!`, `println!`) and panic-handler stubs. With `panic = "abort"` we save little; the real win would be `#![no_std]` + a custom format helper, which is much more codegen complexity.

### Zig

- 0.16 std API churn nearly bit me. I avoided `std.io.getStdOut().writer()` (location has shifted between 0.13/0.14/0.16) by going direct to `extern fn write(1, …)`. **Real friction** for a Zinc backend: pinning Zig minor versions or repeatedly chasing std reorganizations.
- `_ = unused;` boilerplate to silence unused-binding errors is at least as noisy as Rust's `#[allow(dead_code)]`.
- `@ptrCast(@alignCast(raw))` for every `GC_malloc` → typed pointer cast is verbose. Mechanical, but ugly.
- No multi-binary project notion in `build-exe`. Each program is a separate invocation; no shared incremental cache. A `build.zig` would fix this but adds API churn surface.
- Optional + payload-capturing `if` (`if (r.err) |err| { … }`) maps onto Zinc's `or { }` block more cleanly than anything in Rust — `err` is bound exactly the way Zinc binds it.
- GC-allocated heap strings via `rt_format` are architecturally cleaner than the Rust spike, where Rust's `String` backing buffer lives in libc-malloc and silently leaks across GC cycles. (Both are fine for these millisecond programs; Rust would need a custom `GcString` for a real backend.)

## Surface-area assessment ("fewer concepts" in generated code)

A user stepping through the generated code in a debugger sees:

- **Rust**: `Option<T>`, raw-pointer `*mut T`, `&'static`, struct + impl + free functions, `match`, `format!` macro, the `Drop` non-runs (because we never run drop on GC-allocated structs — invisible footgun).
- **Zig**: `?T` optional, `*T` pointer, struct + free functions, `switch`, `std.fmt.bufPrint`. No traits, no impl blocks, no lifetimes, no macros.

Zig wins on concept count in the generated code. Rust wins on familiarity for engineers who've seen any Rust before.

## Honest blockers / caveats

- **`gc.h` headers absent** on the host (`gc-devel` not installed). Both spikes declare `GC_init` / `GC_malloc` as `extern` — this is fine for a real backend (you can vendor or auto-declare), but a real install pipeline would want `gc-devel`.
- **String memory model is incomplete in both spikes.** Rust uses `std::string::String` (libc-malloc, leaks under GC). Zig uses GC-allocated bytes for `rt_format` outputs but not for string literals (which live in `.rodata`, fine). A real backend needs a unified GC-managed string type. This was out of scope.
- **No concurrency.** `spawn` / `parallel for` from `concurrency.zn` were intentionally excluded — Rust async vs Zig single-threaded vs Zig async are very different and would dominate findings. Re-run with concurrency once a v1 backend exists.
- **Zinc-go behavior matched, not validated to spec.** I treated zinc-go's "enum prints as ordinal" as the contract. If the language spec says otherwise, the codegen needs to follow the spec, not the existing backend.
- **One host, one run.** Cold-start medians vary across cold/warm filesystem caches; treat the deltas as directional, not benchmark-grade.

## Recommendation

1. **Pick Rust as the primary new target.** Hireability + crates ecosystem + std stability outweigh the 180 KB Zig wins on binary size.
2. **Keep Zig on the watch list.** Re-evaluate after Zig 1.0 lands and the std API stops moving. The codegen mapping is genuinely simpler in Zig and the binary size advantage is real.
3. **Pre-work for the real Rust backend**: design a GC-managed `ZString` type before writing the codegen — the Rust spike's libc-allocated strings are the one thing here that genuinely doesn't scale.
