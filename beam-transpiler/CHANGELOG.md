# Changelog

## zc 0.1.0

First release candidate for the canonical Zinc-to-BEAM toolchain.

- Canonical `.zn` frontend only.
- BEAM/OTP project support through `zinc.toml`, `zc build`, `zc run`, and `zc release`.
- `class Main : Application` roots and `class X : Actor` supervised processes.
- `class X : Test` support through `zc test` and EUnit generation.
- Rebar plugin support for `src/**/*.zn` and `test/**/*.zn`.
- Packaged CLI, transpiler jar, rebar plugin, and install script smoke tests.
- Retired alternate frontends, gates, and examples from the active tree.
