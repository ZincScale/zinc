# Versioning Policy

Zinc follows [Semantic Versioning 2.0.0](https://semver.org/).

## Current version: 0.5.0

## Pre-1.0 rules (0.x.y)

During 0.x development, the API and language syntax are not yet stable:

- **0.x.0** — new language features, syntax changes, or breaking changes to CLI behavior
- **0.x.y** (patch) — bug fixes, documentation updates, internal improvements

## Post-1.0 rules (x.y.z)

Once Zinc reaches 1.0, the language syntax and CLI interface are considered stable:

- **x.0.0** (major) — breaking changes to language syntax, removed features, or incompatible CLI changes
- **x.y.0** (minor) — new language features, new CLI commands, new built-ins (backwards-compatible)
- **x.y.z** (patch) — bug fixes, performance improvements, documentation updates

## What counts as a breaking change

- Removing or renaming a keyword or built-in function
- Changing the semantics of existing syntax
- Removing a CLI command or changing its flags in incompatible ways
- Changing generated output (C# or Go) in ways that break previously-compiling Zinc code

## What does NOT count as a breaking change

- Adding new keywords, built-in functions, or CLI commands
- Improving error messages
- Changing internal codegen output that doesn't affect behavior
- Adding new examples or documentation

## Release process

1. Update the version in `cmd/zinc/main.go`
2. Tag the commit: `git tag v0.x.y`
3. Push the tag: `git push origin v0.x.y`
4. CI runs all tests and smoke tests against the tagged commit
