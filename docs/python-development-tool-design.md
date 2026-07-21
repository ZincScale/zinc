# Python Development Tool Design

**Status:** design contract implemented through Phase 6 in `build-tools/pymgr/`,
including the optional PyCharm plugin. Automated acceptance is complete; a live
PyCharm installation smoke test remains a release check rather than missing
implementation.

This document defines a development tool for ordinary Python projects. The
tool coordinates dependency management, IDE state, module resolution, public
exports, safe refactoring, and runtime tracing. It does not introduce a new
language and does not replace Python's existing package managers, type engines,
linters, or IDEs.

The CLI is named `pymgr`. The earlier `pydev` candidate was rejected because it
is already the name of an established Python IDE project.

## 1. Problem

Python project state is distributed across source files, `pyproject.toml`, an
optional lockfile, installed distribution metadata, editable installs,
`sys.path`, IDE interpreter configuration, type-checker configuration, and IDE
indexes. These pieces can disagree while the program still runs.

The tool must address the following failures:

- A dependency update requires manual uninstall/reinstall work before the IDE
  resolves it correctly.
- The terminal, CI, and IDE resolve the same import differently.
- Editable workspace packages or source roots become stale in the IDE.
- Developers cannot explain which distribution or file supplied an import.
- Public package exports are implicit, inconsistent, or hidden in
  `__init__.py` side effects.
- Moving a module requires manual import and re-export cleanup.
- Static "find usages" misses dynamic calls, while runtime tracing lacks the
  static project context.
- Developers cannot tell whether an explicit loop, comprehension, generator,
  built-in reduction, `itertools` pipeline, or an already-available vectorized
  operation best fits the semantics, memory constraints, and actual workload.
- Import cycles, package shadowing, undeclared dependencies, and private API
  imports are discovered late.

## Core invariant: tooling is optional

The project MUST remain an ordinary Python project. Installing, running, or
deploying the application MUST NOT depend on `pymgr`, its daemon, its IDE
plugin, its indexes, or its trace agent.

All of these workflows must continue to work without the tool:

```text
python -m acme
python src/acme/main.py
pytest
uv run python -m acme
```

The tool may read standard Python project state and may update ordinary Python
source or standard project metadata only through explicit user commands. It
MUST NOT introduce a required runtime import hook, custom module loader,
`sitecustomize`, `PYTHONPATH` mutation, generated runtime shim, wrapper module,
or proprietary application manifest.

Deleting `.pymgr/`, uninstalling the CLI, disabling the IDE plugin, or running
the project on a machine that has never seen the tool may remove diagnostics,
navigation, synchronization, and trace history, but MUST NOT change application
behavior. Tool caches and state are always optional derived data.

Runtime tracing is explicitly opt-in for a single launched command. The trace
agent MUST NOT remain installed as an application dependency or activate during
normal Python execution.

## 2. Goals

The tool MUST:

1. Make dependency changes transactional and reproducible.
2. Keep the project environment and IDE project model synchronized.
3. Explain exactly how a module or symbol resolves.
4. Give packages an explicit, enforceable public API convention.
5. Model imports across files and workspace packages.
6. Detect cycles, shadowing, undeclared dependencies, and boundary violations.
7. Safely update static imports and exports when modules or symbols move.
8. Combine static references with observed runtime calls.
9. Explain loop alternatives without making unmeasured performance claims.
10. Give local development and CI the same interpretation of the project.
11. Continue to work well enough to diagnose a damaged project environment.
12. Preserve direct execution by the standard Python interpreter with no tool
    involvement.

## 3. Non-goals

The tool MUST NOT:

- Replace `uv` dependency resolution or environment synchronization.
- Implement another Python type checker, linter, or formatter.
- Replace PyCharm, Pyright, Ruff, or another editor language server.
- Claim that arbitrary dynamic Python is statically safe.
- Import application modules merely to index them.
- Rewrite ambiguous dynamic imports or reflective references automatically.
- Maintain a package index or artifact repository.
- Support every Python package manager in the first release.
- Require a new runtime or language for application code.
- Become a production runtime dependency or mandatory application launcher.
- Install persistent import hooks, startup hooks, or tracing instrumentation.
- Claim that one Python loop form is universally fastest.
- Automatically rewrite stateful or ambiguous loop control flow.
- Add a third-party dependency solely to satisfy a loop recommendation.

## 4. Initial product boundary

The first release supports one deliberately narrow project model:

- `uv` manages dependencies and the environment.
- The project uses a `src/` layout.
- The project uses regular packages containing `__init__.py`.
- A uv workspace has one root `uv.lock` and one root `.venv`.
- Every workspace member declares its dependencies explicitly.
- Absolute imports are canonical for project code.
- `__all__` is the public export source of truth.
- Star imports and project-local `sys.path` mutation are forbidden.
- Namespace packages require an explicit opt-in and are out of scope for the
  first implementation milestone.
- Python 3.12 or newer is required for runtime tracing through
  `sys.monitoring`; the rest of the tool may support older interpreters later.
- Loop performance rankings apply only to the interpreter build and workload
  on which they were measured. The initial measured-performance target is
  CPython.
- PyCharm is the first IDE integration. The CLI remains fully usable without
  an IDE plugin.

Do not expand these boundaries during the initial implementation merely to
accommodate an edge case. Record the case and evaluate it after the acceptance
criteria for the narrow model pass.

## 5. Sources of truth

| Concern | Authoritative source |
|---|---|
| Project and dependency declarations | `pyproject.toml` |
| Resolved dependency versions | `uv.lock` |
| Materialized environment | project `.venv` |
| Importable project code | configured `src/` roots |
| Public symbols | module or package `__all__` |
| Tool policy | `[tool.pymgr]` in `pyproject.toml` |
| Static analysis | source files, rebuilt in memory for each query |
| Runtime observations | `.pymgr/traces/*.sqlite`, disposable |
| Loop performance evidence | explicit trace or benchmark run, disposable |
| Workspace state | `.pymgr/state.json`, derived |

The tool MUST NOT create a second dependency manifest or public-export
manifest that can drift from Python source. API snapshots are derived review
artifacts, not an additional source of truth.

Recommended initial configuration:

```toml
[tool.pymgr]
source-roots = ["src"]
package-mode = "regular"
import-style = "absolute"
public-api = "all"
forbid-star-imports = true
forbid-sys-path-mutation = true
allow-dynamic-imports = "warn"
```

## 6. Architecture

```text
pyproject.toml + uv.lock + source tree
                    |
                 pymgr core
       +------------+-------------+
       |            |             |
 environment    module/API     code/loop intelligence
       |            |             |
      uv        static index   IDE/type engine
       |            |             |
    .venv      imports/exports   usages/trace
```

The implementation is divided into the following components:

```text
pymgr CLI and optional daemon
|- workspace and transaction manager
|- uv adapter
|- module/import/export index
|- environment and import doctor
|- refactoring coordinator
|- JSON-RPC service for editor integrations
`- JSON workspace state and disposable SQLite reports

isolated Python helper
|- concrete-syntax-tree transformations
`- sys.monitoring runtime trace agent

PyCharm plugin
|- environment generation watcher
|- project model refresh
|- interpreter verification
`- module, import, export, and trace UI
```

The initial core is an isolated Python CLI installed and managed by uv. It uses
Python's standard AST and LibCST without installing either the CLI or LibCST
into the target project's environment. A later daemon MAY split stable,
performance-sensitive coordination into a standalone Go executable, but that
split is not a prerequisite for the environment, analysis, or refactoring
phases. Python-specific transformations and tracing MUST run from the tool's
isolated environment, not from arbitrary packages in the project environment.

The application never calls into the core. The dependency direction is always
from the tool toward standard project files and processes:

```text
pymgr -> Python project
Python project -X-> pymgr
```

## 7. Workspace state machine

The workspace has an explicit state:

```text
dirty -> resolving -> syncing -> validating -> ready
                              `-------------> broken
```

`.pymgr/state.json` contains at least:

```json
{
  "generation": 42,
  "status": "ready",
  "python": "/project/.venv/bin/python",
  "lock_hash": "84ab...",
  "synced_at": "2026-07-17T18:10:00Z"
}
```

The generation number changes only after a successful environment mutation.
IDE integrations compare their loaded generation with this value. Derived
state may always be deleted and rebuilt.

## 8. Dependency and environment transactions

Initial commands:

```text
pymgr new <path> [--name <name>] [--python <version>]
pymgr init
pymgr sync
pymgr add <package>
pymgr remove <package>
pymgr update <package>...
pymgr update --all
pymgr status
pymgr doctor
pymgr run -- <command...>
```

`new` delegates creation of a packaged `src/` project to uv, adds pymgr policy
and an explicit package `__all__`, then completes the normal synchronization
transaction. `run` executes through the locked environment only when the
recorded generation is synchronized; it never performs an implicit mutation.

Every mutation MUST:

1. Acquire an exclusive workspace mutation lock.
2. Save the current `pyproject.toml` and `uv.lock` contents.
3. Apply the requested declaration change.
4. Ask uv to resolve the lockfile.
5. Ask uv to perform an exact environment synchronization.
6. Validate the interpreter, installed distribution metadata, editable
   workspace members, and a minimal set of import probes.
7. Increment and atomically publish the workspace generation.
8. Notify connected editor integrations.
9. On failure, restore the prior metadata and resynchronize the prior locked
   environment.

The tool MUST serialize concurrent mutations. Indexing and read-only queries
may continue while a dependency transaction is running, but their results must
be labeled with the generation they used.

CI MUST use the checked-in lockfile and fail rather than silently update it.

## 9. Module and import model

Initial commands:

```text
pymgr modules
pymgr resolve <module-or-symbol>
pymgr imports <module>
pymgr importers <module>
pymgr graph imports
pymgr cycles
```

For each module, the static index records:

- Fully qualified module name.
- Filesystem origin and configured source root.
- Owning workspace member and installed distribution.
- Regular or namespace package status.
- Static imports and imported symbols.
- Imports guarded by `TYPE_CHECKING`.
- Dynamic import sites.
- Public exports and re-export targets.
- Potential module-initialization side effects.
- Static workspace dependency edges.
- Content hash and workspace generation.

The resolver MUST detect and explain:

- Local modules shadowing the standard library.
- Local modules shadowing installed packages.
- More than one distribution exposing the same import package.
- Installed but undeclared dependencies.
- Declared but unresolved dependencies.
- Workspace-member imports without a declared workspace dependency.
- Resolution differences between the terminal, CI, and IDE.
- Project code that mutates `sys.path`.
- Ambiguous namespace-package portions.
- Import cycles and the edge that closes each cycle.

Static indexing MUST NOT import or execute application modules. Runtime import
probing is allowed only as an explicit diagnostic subprocess with a timeout,
captured output, and a clear warning that module code may execute.

Example verbose result:

```text
Interpreter:     /project/.venv/bin/python
Module:          acme.api
Origin:          /project/src/acme/api/__init__.py
Package type:    regular
Export:          Client -> acme.api.client.Client
Distribution:    acme 1.8.0, editable workspace member
Dependency:      declared
Shadowing:       none
Import cycle:    none
IDE generation:  42, synchronized
```

## 10. Public exports and API contracts

Initial commands:

```text
pymgr exports <package>
pymgr export <package> <name> --from <module>
pymgr unexport <package> <name>
pymgr api snapshot
pymgr api diff
pymgr api check
```

The canonical explicit re-export form is:

```python
from .client import Client as Client
from .config import Config as Config

__all__ = ["Client", "Config"]
```

Rules:

- `__all__` defines the declared public API.
- `X as X` marks an intentional re-export for static tooling.
- Package `__init__.py` files SHOULD otherwise be side-effect-free.
- Star imports in project code are errors.
- Imports from another workspace package's private modules are boundary
  violations even though Python permits them at runtime.
- Duplicate or ambiguous exports are errors.
- Typed distributable packages SHOULD include `py.typed`.
- The tool enforces a development and CI contract; it does not claim to add
  runtime access control to Python.

An API snapshot records derived exported names and signatures so CI can report
removed exports, changed signatures, and additions. It MUST be regenerated from
source rather than edited by hand.

## 11. Dependency and package boundaries

Each workspace member has a declared dependency set. The module graph MUST map
every cross-member import to one of those dependencies.

The first release supports these boundary classifications:

- `public`: imports through a package's declared exported modules.
- `private`: modules or symbols beginning with `_`, or packages explicitly
  marked internal by configuration.
- `test`: imports permitted only from test source roots.
- `type-only`: dependencies reachable only through `TYPE_CHECKING` imports.

The tool MUST report direct imports that bypass a package's declared public
surface. A project may initially configure such findings as warnings, but CI
must be able to promote them to errors.

## 12. Safe refactoring

Initial commands:

```text
pymgr move <old-module> <new-module>
pymgr rename <old-symbol> <new-symbol>
pymgr imports fix
pymgr imports organize
```

Import fixing and organization delegate to the isolated tool installation's
Ruff engine. `pymgr` owns preview, path safety, validation, and rollback; it does
not implement a competing import sorter.

Refactoring MUST use a concrete syntax tree. Regex or unrestricted textual
replacement is prohibited.

A module move updates:

- Static imports.
- Explicit package re-exports.
- `__all__` entries.
- Type annotations and forward references when unambiguous.
- Tests.
- Workspace dependency declarations when ownership changes.
- Derived API snapshots.

Before applying a refactor, the command presents a dry-run report:

```text
18 static imports will change
2 re-exports will change
1 API snapshot entry will change
3 dynamic references require manual review
```

Dynamic imports, strings passed to `importlib`, `getattr` references, plugin
registries, and other ambiguous reflective uses are reported but not rewritten
unless a later feature supplies a provably safe transformation.

## 13. Loop construct analysis

Initial commands:

```text
pymgr loops [path]
pymgr loop explain <file:line>
```

Measured-comparison command:

```text
pymgr loop compare <file:line> -- <command...>
pymgr loop compare --warmups 2 --runs 10 <file:line> -- <command...>
```

The analyzer first classifies intent: stateful control flow, eager collection
construction, lazy streaming, reduction, short-circuit search, parallel or
indexed iteration, condition-driven repetition, asynchronous iteration, or a
numeric/tabular operation supported by an existing project dependency.

It initially understands explicit `for` and `while` loops, list/set/dict
comprehensions, generator expressions, `enumerate`, `zip`, `sum`, `min`,
`max`, `any`, `all`, and relevant `itertools` operations. A library-specific
adapter may suggest an already-declared vectorized API, but the analyzer never
adds a dependency or changes the project's data model by itself.

Every finding separates three evidence levels:

```text
Semantics: generator expression preserves lazy, single-pass consumption
Heuristic: any(...) can short-circuit without constructing a list
Measured:  1.42x faster on CPython 3.14.2 for this recorded workload
```

Static analysis records mutation, side effects, ordering, `break`, `continue`,
loop `else`, early returns, async behavior, result escape, and eager-versus-lazy
allocation. "Keep the explicit loop" is a successful result whenever the loop
expresses state or control flow more safely than an alternative.

The tool may label an alternative clearer, lazier, or less allocating from
static evidence. It MUST NOT label it faster or best without an explicit
comparison tied to an interpreter build, input fixture, warm-up policy, and
workload command. Reports include absolute timings, variability, and conversion
costs where relevant; microbenchmarks are not extrapolated to production.

`loop compare` runs user-approved candidates in child processes and requires
the supplied tests or equivalence command to pass for every candidate. It
currently compares the original source with a complete analyzer-generated
collection, reduction, or predicate rewrite. Every warm-up and measured run
starts from a fresh isolated workspace copy; `.git`, `.pymgr`, `.venv`, bytecode,
and caches are not copied. The command must therefore name the interpreter and
all test or fixture setup it needs.

Candidate order alternates between runs. Reports record source hashes, command,
controller and workload interpreter identity, warm-up policy, absolute samples,
mean, median, standard deviation, range, and a stable workload identifier. A
ranking is inconclusive when the observed difference is within run variability.
The timer covers the complete child-process command, including startup, so the
result describes that supplied workload rather than an isolated loop body or
production performance. Generated variants are temporary, measurements remain
disposable `.pymgr/loop-comparisons/` data, and project source is never replaced.
Interpreter or source changes produce a different report identity.

## 14. Static usages and runtime tracing

Initial commands:

```text
pymgr uses <symbol>
pymgr callers <symbol>
pymgr trace -- <command...>
pymgr trace report
```

The tool MUST delegate semantic static analysis to an established type engine
or IDE. It may cache definitions and reference results, but it must not grow a
parallel type system.

The runtime agent uses `sys.monitoring` and records:

- Caller and callee code identity.
- Source files and lines.
- Call count and elapsed time.
- Exceptions, raises, and handlers.
- Import events.
- Optionally observed argument and return types.

The runtime agent MUST NOT record argument values, return values, secrets, or
arbitrary object representations by default. Trace files are local derived
artifacts and must be gitignored. Instrumentation exists only in the child
process launched by `pymgr trace -- ...`; it must not alter the project's
normal entry points or installed dependencies.

`uses` combines but clearly separates evidence classes:

```text
Static references:
  routes/users.py:31
  jobs/sync.py:82

Observed calls:
  routes.get_user -> services.find_user       12,841 calls
  jobs.sync_user  -> services.find_user           92 calls

Unresolved dynamic references:
  plugins/loader.py:44
```

Observed calls are evidence only for executed paths. Absence from a trace never
proves that a symbol is unused.

Loop tracing may record targeted loop entries, iterations, exits, and elapsed
time. These events are enabled only for selected project code so tracing
overhead does not silently distort the entire workload. Hotness identifies a
candidate for comparison; it does not prove an alternative is faster.

## 15. IDE integration

The CLI must provide all core behavior without an IDE plugin. The PyCharm
plugin adds synchronization and presentation.

The plugin is responsible for:

- Watching `.pymgr/state.json` generation changes.
- Verifying or selecting the project interpreter.
- Requesting a project model and dependency refresh.
- Restarting or refreshing the selected external type engine when needed.
- Displaying ready, dirty, syncing, and broken workspace states.
- Running dependency mutations through the core rather than modifying the
  environment itself.
- Presenting import resolution, cycles, exports, and trace callers.
- Previewing refactors and applying only user-approved changes.

The plugin communicates with the core over a versioned local JSON-RPC
protocol. Other editor integrations may use the same protocol. The core must
not replace an editor's existing Python language server.

The initial protocol implementation is newline-delimited JSON-RPC 2.0 over the
core process's stdin and stdout:

```text
pymgr --root /project serve --stdio
```

Protocol `1.0` exposes initialization and capabilities, workspace generation
and interpreter state, structured doctor findings, module diagnostics, import
cycles, loop inventory and explanations, dependency mutations, and preview-first
module moves and symbol renames. Mutating refactors require explicit
`apply: true`; notifications follow JSON-RPC rules and do not receive responses.
The stdio transport opens no network listener, stores no credentials, and may
be restarted whenever the editor project closes or the core is upgraded.

An early technical spike MUST verify which PyCharm refresh operations are
available through stable plugin APIs. If a full automatic refresh is not
available, the plugin must detect the stale generation and present a single
action that invokes the closest supported synchronization flow.

## 16. Doctor diagnostics

`pymgr doctor` is a primary product feature, not a collection of debug prints.
It MUST compare:

- Configured and active interpreter paths.
- Python versions.
- Lockfile hash and materialized environment generation.
- Declared and installed distributions.
- Editable workspace member origins.
- Source roots.
- `sys.path` under the terminal, tool, and IDE when available.
- Import package to distribution mappings.
- Shadowed modules.
- Missing `__init__.py` files under the regular-package policy.
- IDE generation and analysis-engine state.

Every failure should explain the observed state, expected state, and safe
repair command. The doctor MUST prefer `pymgr sync` or an explicit IDE refresh
over recommending manual package uninstall/reinstall.

## 17. Caching and persistence

The current implementation rebuilds its static project index in memory for
each query. It persists only derived workspace state in `.pymgr/state.json`,
loop-comparison reports as JSON, runtime traces as SQLite, and API snapshots
when the user explicitly requests them. Persisted reports record or derive
identity from:

- Workspace identity.
- Workspace generation.
- Source content hash.
- Python feature version.
- Tool schema version.
- Interpreter build and workload identity for measured loop advice.

Workspace-state and source-file writes use atomic replacement; SQLite trace
writes use transactions. The tool must recover from deleted derived state by
rebuilding it. No derived state is required for source correctness.

## 18. Security and safety

- Static indexing never executes application modules.
- Explicit runtime probes run in subprocesses with timeouts.
- Dependency credentials remain owned by uv or existing credential providers.
- Workspace state files and trace databases do not store credentials.
- Tracing captures identities and types, not values, by default.
- Refactors are dry-run first and use atomic file replacement.
- Dependency changes retain enough prior state to roll back.
- IDE integrations may request mutations only through the core transaction
  manager.
- Loop comparisons run only explicit user commands, require equivalence checks,
  and never install suggested dependencies automatically.

## 19. Delivery phases

### Phase 0: technical spikes

Prove these risks before building the full CLI:

1. PyCharm can detect a uv environment generation change and refresh its
   project model without an IDE restart.
2. The selected static type engine can provide definitions and references over
   a stable integration boundary.
3. A concrete-syntax-tree transformation can move a representative module
   while preserving formatting and comments.
4. `sys.monitoring` can record useful call edges at acceptable overhead.
5. Representative loop patterns can be classified and compared without the
   measurement harness overwhelming the performance difference.

Each spike gates the phase that depends on it. Environment, module, and
refactoring work need not wait for the later PyCharm, tracing, or loop-measurement
spikes, but those later phases must not begin until their own risks are proven.

### Phase 1: environment foundation

- `new`, `init`, `sync`, `add`, `remove`, `update`, and locked `run`.
- Workspace state, generation, and mutation locking.
- Rollback after failed synchronization.
- `status` and environment-focused `doctor`.
- Locked CI verification.

Exit criterion: dependency changes never require manual uninstall/reinstall,
and the environment can always be explained or repaired by the tool.

### Phase 2: modules and exports

- Module discovery and ownership.
- Static import graph.
- Import resolution and shadow detection.
- Workspace dependency boundaries.
- Cycle detection.
- Explicit export management.
- API snapshots and diffs.

Exit criterion: every static import and declared public export is queryable and
has one unambiguous origin.

### Phase 3: refactoring

- Module moves.
- Symbol renames where semantic resolution is certain.
- Import organization.
- Re-export and `__all__` updates.
- Dynamic-reference review reports.

Exit criterion: moving a representative module updates all static imports and
exports without textual search-and-replace or formatting loss.

Implementation status: file and package moves, cross-package relative-import
migration, qualified symbol renames, re-export updates, and Ruff-backed import
organization are implemented. Dynamic references remain review-only.

### Phase 4: loop analysis

- Loop inventory and semantic classification.
- Explicit-loop, comprehension, generator, built-in, and `itertools` advice.
- Eager-versus-lazy and allocation diagnostics.
- Interpreter-aware explanations without unmeasured speed claims.
- User-approved comparison harnesses with equivalence checks.

Exit criterion: representative loops receive useful semantic and memory advice,
and every performance ranking is reproducible on its named interpreter and
workload. "Keep this loop" is a first-class successful result.

Implementation status: `loops` and `loop explain` inventory and classify
explicit loops, comprehensions, generators, reductions, iterator adapters, and
common accumulator/index patterns. `loop compare` measures safe generated
collection, reduction, and predicate candidates in isolated copies, rejects any
candidate whose equivalence command fails, and persists reproducible reports.
Conservative adapters recognize declared NumPy `nditer`, pandas `iterrows` and
`itertuples`, and Polars `iter_rows` usage. They recommend array or column
expressions only as semantic review candidates and never add a dependency,
rewrite the data model, or claim they are faster without measurement. Static
reports still mark performance as unmeasured until a comparison is explicitly
requested.

### Phase 5: PyCharm integration

- Generation watcher.
- Interpreter verification.
- Automatic or one-action project synchronization.
- Import and module diagnostics.
- Dependency mutation actions.
- Refactor previews.
- Loop explanations and comparison results.

Exit criterion: a successful dependency update becomes navigable in PyCharm
without restarting the IDE or manually reinstalling packages.

Implementation status: the versioned JSON-RPC-over-stdio core boundary is
implemented, including workspace generation/interpreter state, diagnostics,
dependency actions, refactor previews, loop presentation, and runtime evidence.
The optional PyCharm 2026.1 plugin watches generation changes, verifies or
selects an already-configured interpreter, refreshes the VFS and code analyzer,
and presents dependency, refactor, loop, usage, caller, and trace actions. Its
Gradle build is verified against the PyCharm SDK; installation in a live IDE is
the remaining release smoke test.

### Phase 6: runtime intelligence

- `sys.monitoring` trace agent.
- Runtime call and exception graph.
- Targeted loop counts, exits, and elapsed-time evidence.
- Static-plus-observed `uses` and `callers` queries.
- IDE trace presentation.

Exit criterion: developers can navigate representative dynamic call paths that
static analysis alone cannot resolve.

Implementation status: `trace` launches only an explicit Python 3.12+ child
through a standalone `sys.monitoring` agent, without environment hooks or a
runtime dependency. Disposable SQLite reports record project call edges,
function counts and elapsed time, raised and handled exception types, imports,
and selected loop entries/header hits/elapsed time. `trace report`, `uses`, and
`callers` expose the data while keeping static imports and observed calls as
separate evidence classes; the RPC service and PyCharm tool window present the
same reports.

## 20. Acceptance workflow

The complete initial product is accepted when this workflow succeeds:

```text
pymgr new acme --python 3.14
cd acme
pymgr update pydantic
pymgr move acme.old.models acme.models --apply
pymgr api check
pymgr loops src/acme
pymgr loop explain src/acme/models.py:42
pymgr loop compare --warmups 2 --runs 10 src/acme/models.py:42 -- python check_models.py
pymgr trace -- pytest
pymgr run -- pytest
```

Afterward:

- `.venv` exactly matches `uv.lock`.
- PyCharm sees the updated dependency without an IDE restart or manual package
  reinstall.
- Static imports point to the moved module.
- Public re-exports and `__all__` remain correct.
- CI observes the same dependency and module model.
- Static and observed usages are separately queryable.
- Loop reports separate semantic, memory, and measured-performance evidence.
- Unresolved dynamic behavior is explicitly reported.
- `pymgr doctor` reports a ready, synchronized workspace.
- The same source still runs directly through `python`, `python -m`, `pytest`,
  and `uv run` without `pymgr` installed or active.
- Removing `.pymgr/` changes no application output or import behavior.

## 21. Implementation invariants

These rules apply throughout implementation:

1. Keep the tool completely optional for application execution and deployment.
2. Do not create a new Python language or runtime.
3. Do not add required runtime hooks, loaders, shims, wrappers, or agents.
4. Do not implement dependency resolution; delegate it to uv.
5. Do not implement a parallel type system; delegate semantic analysis.
6. Do not execute user modules during static indexing.
7. Do not use regex for source refactoring.
8. Do not silently rewrite unresolved dynamic behavior.
9. Do not let the IDE mutate the environment outside the core transaction
   manager.
10. Do not recommend manual uninstall/reinstall as routine recovery.
11. Keep caches disposable and sources of truth explicit.
12. Complete and validate each delivery phase before expanding scope.
13. Never infer loop performance from syntax alone; make speed claims only for
    a named interpreter and measured workload.
14. Prefer no loop rewrite over one with uncertain semantics, ordering, side
    effects, or exception behavior.
