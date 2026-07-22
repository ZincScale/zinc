# Zinc library dependencies

## Contract

Zinc reuses Go modules as its package transport and cache. There is no
second Zinc-specific download directory or registry:

1. A Zinc import selects a package, for example `import stdlib/config`.
2. `zinc-go` writes the selected module and version to the generated
   `go.mod`.
3. `go mod download` fetches it into `GOMODCACHE`; repeated builds reuse
   that cached module.
4. `zinc-go` asks `go list -m` for the resolved module directory and reads
   the Zinc metadata shipped in that module.
5. Generated Go imports refer to packages from that same resolved module.

The standard-library alias is reserved. New projects pin its stable public
module identity and version independently of the compiler repository layout:

```toml
[stdlib]
module = "github.com/ZincScale/zinc-stdlib"
version = "v0.1.0"
```

Importing any `stdlib/...` package then selects:

```text
github.com/ZincScale/zinc-stdlib@v0.1.0
```

Projects do not need a `[deps]` entry for the standard library. The compiler
uses its built-in coordinate only for older manifests without `[stdlib]`.
Changing the compiler checkout layout or upgrading the compiler therefore does
not silently retarget new applications. A project may update `[stdlib]`
deliberately or use a local `[replace]` while developing the standard library.

## Published module layout

A published Zinc module is also a valid Go module:

```text
go.mod
zinc-library.json
zinc-src/
  config/
    config.zn
config/
  config.go
```

`zinc-library.json` is the stable discovery point:

```json
{
  "schema": 1,
  "module": "github.com/example/library",
  "source": "zinc-src"
}
```

The generated Go packages are the executable library. The `.zn` files under
`zinc-src/` are compiler metadata: they retain Zinc-only declarations and
semantics that cannot always be reconstructed from Go source. `zinc-go build`
copies project source into this layout, so the output directory is ready to
publish as a module.

Library projects declare `kind = "library"` under `[project]`. Their build
validates all generated packages with `go build ./...` and does not place an
application executable in the module output.

The source path must be relative to the module root and cannot escape it.
Unknown manifest schemas, invalid source paths, malformed Zinc metadata, and
missing metadata directories are hard errors. Ordinary Go dependencies have
no `zinc-library.json` and continue through the existing Go interop path.

## Local development

The standard library can be developed without publishing a version:

```toml
[stdlib]
module = "github.com/ZincScale/zinc-stdlib"
version = "v0.1.0"

[replace]
stdlib = "../../stdlib/zinc-out"
```

The reserved `stdlib` alias makes a matching `[deps]` entry unnecessary.
The replacement directory must have the same publishable layout, including
`go.mod`, `zinc-library.json`, `zinc-src/`, and generated Go packages.

## Release boundary

Module publication is deliberately separate from compilation. A release
publishes the contents of `zinc-out/` and tags the module version. Consumers
never clone or pull a library repository themselves; Go's module tooling owns
network retrieval, checksums, version selection, and caching.
