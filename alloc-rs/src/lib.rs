//! Phase 0 allocator stub for pluto-zig.
//!
//! Exposes the C ABI the Zig side will consume. The implementation is
//! malloc-backed (via libc) for now — the point of phase 0 is to prove
//! the Zig <-> Rust plumbing, not to ship a real GC. Phase 0.5 replaces
//! the bodies with mmtk-core calls; the C ABI does not change.
//!
//! `#![no_std]` keeps the static archive free of std::fs / std::net /
//! backtrace_rs / glibc-CSU symbols Zig's linker would otherwise have
//! to resolve. The mmtk swap lives entirely on the C ABI surface.

#![no_std]

use core::ffi::c_void;
use core::sync::atomic::{AtomicUsize, Ordering};

extern "C" {
    fn malloc(size: usize) -> *mut c_void;
    fn free(ptr: *mut c_void);
    fn aligned_alloc(alignment: usize, size: usize) -> *mut c_void;
    fn abort() -> !;
}

// Simple stats so the spike can prove allocations are flowing.
static BYTES_ALLOCATED: AtomicUsize = AtomicUsize::new(0);
static OBJECTS_ALLOCATED: AtomicUsize = AtomicUsize::new(0);

#[repr(C)]
pub struct PlutoHeap {
    // Phase 0: opaque sentinel. Phase 0.5: holds the MMTK<VM> handle.
    sentinel: usize,
}

/// Initialize the heap with `heap_bytes` of capacity. Returns an
/// opaque heap handle — the Zig side passes this back on every call.
///
/// Phase 0: capacity is informational (malloc has no hard cap).
/// Phase 0.5: this is where mmtk_init + plan selection happens.
#[no_mangle]
pub extern "C" fn pluto_heap_init(_heap_bytes: usize) -> *mut PlutoHeap {
    unsafe {
        let p = malloc(core::mem::size_of::<PlutoHeap>()) as *mut PlutoHeap;
        if !p.is_null() {
            (*p).sentinel = 0xC0DE_DEAD_BEEF_F00Dusize;
        }
        p
    }
}

/// Tear down the heap. Frees the handle.
#[no_mangle]
pub extern "C" fn pluto_heap_shutdown(heap: *mut PlutoHeap) {
    if !heap.is_null() {
        unsafe { free(heap as *mut c_void) };
    }
}

/// Allocate `size` bytes with `align` alignment. Returns a raw pointer
/// the caller must initialize before the next safepoint.
///
/// Phase 0: aligned_alloc + bookkeeping.
/// Phase 0.5: routes to mmtk_alloc with a chosen Allocator + AllocSemantics.
#[no_mangle]
pub extern "C" fn pluto_alloc(
    _heap: *mut PlutoHeap,
    size: usize,
    align: usize,
) -> *mut c_void {
    let align = if align == 0 { 8 } else { align };
    // aligned_alloc requires size % align == 0.
    let rounded = (size + align - 1) & !(align - 1);
    let ptr = unsafe { aligned_alloc(align, rounded) };
    if !ptr.is_null() {
        BYTES_ALLOCATED.fetch_add(rounded, Ordering::Relaxed);
        OBJECTS_ALLOCATED.fetch_add(1, Ordering::Relaxed);
    }
    ptr
}

/// Post-allocation hook. mmtk requires this after every alloc so the
/// collector can install GC headers / register the object with the
/// right space. Phase 0 is a no-op; phase 0.5 calls mmtk_post_alloc.
#[no_mangle]
pub extern "C" fn pluto_post_alloc(
    _heap: *mut PlutoHeap,
    _obj: *mut c_void,
    _size: usize,
) {
    // Intentionally empty in phase 0. Keep the symbol so the Zig side
    // codes against the eventual mmtk shape and the swap is invisible.
}

/// Free a single object. Only meaningful in the stub allocator —
/// once mmtk is wired in, the collector reclaims memory and this
/// becomes a no-op.
#[no_mangle]
pub extern "C" fn pluto_dealloc_stub(obj: *mut c_void, _size: usize, _align: usize) {
    if !obj.is_null() {
        unsafe { free(obj) };
    }
}

#[no_mangle]
pub extern "C" fn pluto_stat_bytes_allocated() -> usize {
    BYTES_ALLOCATED.load(Ordering::Relaxed)
}

#[no_mangle]
pub extern "C" fn pluto_stat_objects_allocated() -> usize {
    OBJECTS_ALLOCATED.load(Ordering::Relaxed)
}

// no_std requires a panic handler. abort() is fine — phase 0 has no
// recoverable failure modes and panic = "abort" in Cargo.toml means
// nothing in the crate panics today anyway.
#[panic_handler]
fn panic(_info: &core::panic::PanicInfo) -> ! {
    unsafe { abort() }
}

// Glibc + linker compatibility shims.
//
// Older crt1.o (RHEL 8 ships gcc 8 with crt1 that references these
// symbols) calls into __libc_csu_init / __libc_csu_fini, which glibc
// 2.34+ inlined into _start and stopped exporting. Providing no-op
// stubs satisfies the linker without changing runtime behavior on
// glibcs that still carry them (the system's are picked first via
// symbol resolution order).
#[no_mangle]
pub extern "C" fn __libc_csu_init() {}

#[no_mangle]
pub extern "C" fn __libc_csu_fini() {}

// compiler_builtins keeps a DWARF reference to rust_eh_personality
// even with panic = "abort". The symbol is never actually invoked at
// runtime (no panics, no unwinding), so a no-op stub is enough.
#[no_mangle]
pub extern "C" fn rust_eh_personality() {}
