//! pluto-zig build.zig — orchestrates the Cargo + Zig builds.
//!
//! The Rust crate at alloc-rs/ produces a static archive
//! (libpluto_alloc.a). We declare a build step that shells out to
//! cargo, then have the Zig executable depend on that step and link
//! the archive. Phase 0.5 will keep this exact wiring; only the
//! Rust crate's body changes when we swap in mmtk.

const std = @import("std");

pub fn build(b: *std.Build) void {
    const target = b.standardTargetOptions(.{});
    const optimize = b.standardOptimizeOption(.{});

    // --- Rust crate (alloc-rs) -------------------------------------------
    // Always build release: LTO + dead-code-elimination strips unused
    // std::fs / std::net paths that otherwise pull in legacy glibc
    // *64 symbols (stat64, lstat64, ...) Zig's linker can't resolve.
    // The Zig-side optimize level is independent.
    const cargo_profile = "release";
    const cargo_cmd = b.addSystemCommand(&.{
        "cargo", "build", "--release",
        "--manifest-path", "alloc-rs/Cargo.toml",
    });
    cargo_cmd.setName("cargo build alloc-rs");

    // --- Executable -------------------------------------------------------
    const exe = b.addExecutable(.{
        .name = "pluto_zig",
        .root_module = b.createModule(.{
            .root_source_file = b.path("src/main.zig"),
            .target = target,
            .optimize = optimize,
        }),
    });

    // Link the Rust static archive. In Zig 0.16, linker inputs hang
    // off the module, not the Compile step. The path is relative to
    // the build root; once cargo has run, the archive is at this path.
    const archive_path = b.fmt("alloc-rs/target/{s}/libpluto_alloc.a", .{cargo_profile});
    exe.root_module.addObjectFile(b.path(archive_path));

    // libc + pthread are required transitively by the Rust stdlib's
    // std::alloc / std::sync paths. libdl + libm round out what the
    // Rust runtime expects on Linux.
    exe.root_module.link_libc = true;
    exe.root_module.linkSystemLibrary("pthread", .{});
    exe.root_module.linkSystemLibrary("dl", .{});
    exe.root_module.linkSystemLibrary("m", .{});
    exe.root_module.linkSystemLibrary("gcc_s", .{});
    // bdwgc — the actual collector behind pluto_alloc in phase 0.5.
    // vendor/lib/libgc.so is a symlink to whatever's available on the
    // system (typically /lib64/libgc.so.1). The symlink avoids needing
    // the gc-devel package installed (which is what would normally
    // provide the unversioned libgc.so name).
    exe.root_module.addLibraryPath(b.path("vendor/lib"));
    exe.root_module.linkSystemLibrary("gc", .{});

    // The executable's compile step depends on cargo finishing.
    exe.step.dependOn(&cargo_cmd.step);

    b.installArtifact(exe);

    // --- run step ---------------------------------------------------------
    const run_cmd = b.addRunArtifact(exe);
    run_cmd.step.dependOn(b.getInstallStep());
    if (b.args) |args| run_cmd.addArgs(args);

    const run_step = b.step("run", "Run the phase-0 spike");
    run_step.dependOn(&run_cmd.step);

    // --- test step (placeholder) -----------------------------------------
    const exe_tests = b.addTest(.{ .root_module = exe.root_module });
    const run_exe_tests = b.addRunArtifact(exe_tests);
    const test_step = b.step("test", "Run tests");
    test_step.dependOn(&run_exe_tests.step);
}
