//! Phase 0.5 allocator for pluto-zig.
//!
//! Backed by the Boehm-Demers-Weiser conservative GC (libgc.so).
//! Same C ABI as phase 0 — Zig side untouched. Phase 0.7 swaps the
//! bodies for mmtk-core's GenImmix; bdwgc holds the real-GC fort
//! until then.
//!
//! bdwgc is conservative: it scans the stack and registers for
//! pointer-shaped values to find roots, and walks heap memory the
//! same way for tracing. We don't need to enumerate roots or scan
//! objects — the GC infers everything from memory contents. That's
//! the headline tradeoff: zero binding work, modest retention bugs
//! at scale.

#![no_std]

use core::ffi::c_void;
use core::sync::atomic::{AtomicUsize, Ordering};

// libgc (Boehm GC) C API. We declare only what we use; the full
// surface is in /usr/include/gc.h on systems with gc-devel installed.
extern "C" {
    fn GC_init();
    fn GC_malloc(size: usize) -> *mut c_void;
    // Pointer-free variant — phase 1 will use this for leaf objects
    // (strings, byte buffers) so the collector doesn't waste cycles
    // scanning their interior for pointers that aren't there.
    #[allow(dead_code)]
    fn GC_malloc_atomic(size: usize) -> *mut c_void;
    fn GC_get_heap_size() -> usize;
    fn GC_get_total_bytes() -> usize;
    fn GC_get_gc_no() -> usize;
    fn GC_gcollect();
    fn abort() -> !;
}

// Per-allocator-call counters (visible on the Zig side as a sanity
// check that calls are flowing). bdwgc has its own stats too which
// we expose separately below.
static OBJECTS_ALLOCATED: AtomicUsize = AtomicUsize::new(0);

#[repr(C)]
pub struct PlutoHeap {
    sentinel: usize,
}

/// Initialize the GC. `heap_bytes` is informational — bdwgc grows on
/// demand and ignores the hint at this layer (you can preset the
/// initial heap size via GC_set_initial_heap_size on a real binding).
#[no_mangle]
pub extern "C" fn pluto_heap_init(_heap_bytes: usize) -> *mut PlutoHeap {
    unsafe {
        GC_init();
        // Allocate the handle through the GC itself — it'll never be
        // freed in phase 0.5 because the Zig side holds the reference
        // for the program's lifetime, but threading it through GC_malloc
        // proves the path is alive even before the first object alloc.
        let p = GC_malloc(core::mem::size_of::<PlutoHeap>()) as *mut PlutoHeap;
        if !p.is_null() {
            (*p).sentinel = 0xC0DE_DEAD_BEEF_F00Dusize;
        }
        p
    }
}

/// bdwgc has no concept of teardown. The handle drops with the
/// process. Kept as an FFI symbol so the Zig side doesn't have to
/// know which collector backs it.
#[no_mangle]
pub extern "C" fn pluto_heap_shutdown(_heap: *mut PlutoHeap) {
    // no-op
}

/// Allocate a GC-managed block. Pointer is heap-tracked; release is
/// automatic at the next collection cycle once nothing points at it.
///
/// Note: we use GC_malloc (the scanning variant) so any pointers we
/// later store inside this object are followed during marking. For
/// leaf objects with no internal pointers (strings, byte buffers),
/// GC_malloc_atomic is faster — but phase 0.5 doesn't distinguish
/// yet. Phase 1 (when TValue + Table arrive) will pick per-type.
#[no_mangle]
pub extern "C" fn pluto_alloc(
    _heap: *mut PlutoHeap,
    size: usize,
    _align: usize,
) -> *mut c_void {
    let ptr = unsafe { GC_malloc(size) };
    if !ptr.is_null() {
        OBJECTS_ALLOCATED.fetch_add(1, Ordering::Relaxed);
    }
    ptr
}

/// bdwgc has no post-alloc hook. mmtk does — keep the symbol so the
/// API shape is stable across collectors.
#[no_mangle]
pub extern "C" fn pluto_post_alloc(
    _heap: *mut PlutoHeap,
    _obj: *mut c_void,
    _size: usize,
) {
    // no-op
}

/// Phase-0 vestige. bdwgc reclaims via marking, not explicit free —
/// the symbol stays for ABI compat but does nothing.
#[no_mangle]
pub extern "C" fn pluto_dealloc_stub(_obj: *mut c_void, _size: usize, _align: usize) {
    // no-op — bdwgc owns reclamation
}

/// Force a full GC cycle. Useful for tests / spike demos that want
/// to observe collection behavior. Real workloads should never call
/// this; the collector triggers itself.
#[no_mangle]
pub extern "C" fn pluto_force_gc() {
    unsafe { GC_gcollect() };
}

#[no_mangle]
pub extern "C" fn pluto_stat_bytes_allocated() -> usize {
    // bdwgc's own counter — total bytes ever allocated through the GC.
    unsafe { GC_get_total_bytes() }
}

#[no_mangle]
pub extern "C" fn pluto_stat_objects_allocated() -> usize {
    OBJECTS_ALLOCATED.load(Ordering::Relaxed)
}

/// Current heap size as reported by bdwgc. Reflects actual reserved
/// memory; shrinks after collection of unreachable objects.
#[no_mangle]
pub extern "C" fn pluto_stat_heap_size() -> usize {
    unsafe { GC_get_heap_size() }
}

/// Number of full GC cycles that have run since GC_init.
#[no_mangle]
pub extern "C" fn pluto_stat_gc_count() -> usize {
    unsafe { GC_get_gc_no() }
}

#[panic_handler]
fn panic(_info: &core::panic::PanicInfo) -> ! {
    unsafe { abort() }
}

// Glibc / linker compatibility shims. Same rationale as phase 0.
#[no_mangle]
pub extern "C" fn __libc_csu_init() {}

#[no_mangle]
pub extern "C" fn __libc_csu_fini() {}

#[no_mangle]
pub extern "C" fn rust_eh_personality() {}
