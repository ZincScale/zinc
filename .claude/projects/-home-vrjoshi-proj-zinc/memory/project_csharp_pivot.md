---
name: C# AOT pivot
description: Zinc pivoting to C# AOT as default backend, Go secondary, Python removed. Major syntax changes (var, ->).
type: project
---

Zinc is pivoting to C# AOT as the default backend (2026-03-16).

**Identity:** "Spring Boot for compiled native apps" — convention over configuration, less typing, less ceremony. Write one clean language, get optimized native AOT binaries.

**Syntax changes:**
- `:=` → `var` (ergonomic — avoids pinky-shift colon)
- `=>` → `->` for lambdas (matches Java/Kotlin)

**Backend changes:**
- C# AOT is the **default** backend (was Go)
- Go is secondary (`--target go`)
- Python backend **removed**
- AOT compilation with full optimizations: `PublishAot=true`, `OptimizationPreference=Speed`, strip symbols, invariant globalization

**Collection methods:** Being added back for C# backend (LINQ-style operations).

**Why:** Enterprise feedback — ecosystem friction with Go's non-OO idioms was the primary adoption blocker. C# AOT gives 1:1 OO mapping, competitive/better performance (2-3x faster on Where+Select), and 1.6 MB native binaries.

**How to apply:** All new features should target C# backend first. Go backend maintained but not primary. Use `->` for lambdas, `var` for declarations in all examples and docs.
