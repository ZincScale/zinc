# Getting started with pymgr

`pymgr` helps develop normal Python projects without becoming part of their
runtime. It coordinates uv, explains imports and public APIs, performs safe
refactors, compares loop alternatives, records opt-in runtime evidence, and
feeds the same data to PyCharm.

Use `pymgr` as the project-facing command. It delegates environment mechanics
to uv internally, but project creation, dependency changes, synchronization,
and commands in the managed environment all go through `pymgr`.

## 1. Requirements

- Python 3.12 or newer.
- [uv](https://docs.astral.sh/uv/) on `PATH`.
- For an existing project, `pyproject.toml`, a `src/` layout, and regular
  packages with `__init__.py`.
- For an existing uv workspace, one root `uv.lock`; workspaces may have multiple
  members.

A minimal target looks like this:

```text
acme/
|- pyproject.toml
|- uv.lock
|- src/
|  `- acme/
|     |- __init__.py
|     `- service.py
`- tests/
```

Recommended configuration:

```toml
[tool.pymgr]
source-roots = ["src"]
```

## 2. Install the development tool

From the Zinc checkout:

```text
cd build-tools/pymgr
./install.sh
pymgr --help
```

The installer is the one bootstrap step before `pymgr` exists; it uses the uv
tool installer internally. After that, use `pymgr` for project operations and
do not invoke uv directly.

Global options must precede the command:

```text
pymgr --root /path/to/acme status
pymgr --root /path/to/acme --json modules
```

`--root` may name the workspace root or any path inside it. Without the option,
`pymgr` searches upward from the current directory. `--json` makes command
output machine-readable.

## 3. First project workflow

Create and enter a new packaged project:

```text
pymgr new /path/to/acme --name acme --python 3.14 \
  --description "Example service"
cd /path/to/acme
pymgr doctor
pymgr modules
pymgr api snapshot
pymgr run -- acme
```

`new` requires an absent or empty destination. It creates `pyproject.toml`, a
regular package under `src/`, `__init__.py`, an explicit empty `__all__`, the
`[tool.pymgr]` configuration, the lockfile, and a synchronized environment.
The empty `__all__` means the package initially exposes no public names; use
`pymgr export` as the API grows.

To adopt an existing packaged project instead:

```text
cd /path/to/acme
pymgr init
pymgr sync
pymgr doctor
pymgr modules
pymgr api snapshot
```

Commit standard project files such as `pyproject.toml`, `uv.lock`, source, and
an intentional API snapshot. `.pymgr/` is derived local state and can be
deleted. Run project commands through the synchronized environment:

```text
pymgr run -- acme
pymgr run -- python -c "import acme; print(acme.__all__)"
```

## 4. Environment commands

### `new`

Bootstrap and synchronize a new packaged `src/` project. Python 3.12 is the
default; select a newer interpreter explicitly when desired.

```text
pymgr new acme
pymgr new acme --python 3.14
pymgr new services/acme --name acme-service \
  --description "Acme HTTP service"
```

`--root` does not apply to `new`; the positional path is the new root. pymgr
will not add files to a nonempty directory.

### `init`

Initialize an existing project by creating `.pymgr/` and adding a default
`[tool.pymgr]` table when absent. It does not create `pyproject.toml`, source
packages, tests, or `__init__.py` files.

```text
pymgr init
```

Unlike `new`, `init` deliberately does not create source packages or
`__init__.py` files in an existing project.

### `sync`

Resolve, synchronize, validate, and record the uv environment generation.

```text
pymgr sync
```

In CI, verify the checked-in lockfile without updating it:

```text
pymgr sync --check
```

### `status`

Show the recorded generation, lock hash, interpreter, and synchronization
state. It exits nonzero when the workspace is not synchronized.

```text
pymgr status
pymgr --json status
```

### `doctor`

Compare the lockfile, workspace state, active interpreter, Python version,
source roots, editable members, and installed distributions. Every finding
includes an observed value, expected value, and repair.

```text
pymgr doctor
```

### `run`

Run a command in the locked, synchronized project environment without invoking
uv directly. It refuses to run if `pymgr status` is stale; synchronize first so
execution cannot silently rewrite project metadata or the environment.

```text
pymgr run -- acme
pymgr run -- python -c "import acme; print(acme.__all__)"
pymgr run -- pytest tests
```

The last example assumes the project declares pytest and has a `tests/`
directory. `pymgr new` intentionally creates only the packaged application
skeleton; add tests and development dependencies according to project policy.

### `add`

Add one or more requirements through uv, synchronize, and validate.

```text
pymgr add httpx
pymgr add "pydantic>=2" rich
```

### `remove`

Remove one or more requirements transactionally.

```text
pymgr remove rich
pymgr remove httpx pydantic
```

### `update`

Upgrade named packages or every locked package.

```text
pymgr update pydantic
pymgr update pydantic httpx
pymgr update --all
```

An empty `update` is rejected; choose package names or `--all` explicitly.

## 5. Modules and imports

### `modules`

List local modules, owning workspace packages, paths, package status, exports,
and analysis issues.

```text
pymgr modules
pymgr --json modules
```

### `resolve`

Explain the origin of a local module, definition, public export, installed
module, or installed symbol.

```text
pymgr resolve acme.service
pymgr resolve acme.User
pymgr resolve pydantic.BaseModel
```

### `imports`

Show imports made by one local module.

```text
pymgr imports acme.service
```

Preview and apply safe Ruff import fixes:

```text
pymgr imports fix
pymgr imports fix src/acme/service.py
pymgr imports fix src/acme --apply
```

Preview and apply import organization:

```text
pymgr imports organize
pymgr imports organize src/acme/service.py
pymgr imports organize src/acme --apply
```

### `importers`

List local modules that statically import a module.

```text
pymgr importers acme.models
```

### `graph imports`

Emit the complete local import adjacency graph.

```text
pymgr graph imports
pymgr --json graph imports
```

### `cycles`

Find strongly connected import cycles. It exits nonzero when cycles exist.

```text
pymgr cycles
```

## 6. Public APIs

`pymgr` treats a static `__all__` in `__init__.py` as the declared public API.
It diagnoses missing `__init__.py` files under the regular-package policy,
warns about imports of underscore-prefixed private names, and reports dynamic
`__all__` or star imports rather than guessing. Export commands and safe
move/rename refactors update package re-exports and `__all__` together.

### `exports`

List a package's public names and their origins.

```text
pymgr exports acme
```

### `export`

Add a public re-export and update `__all__`.

```text
pymgr export acme User --from acme.models
```

### `unexport`

Remove a public name from the package surface.

```text
pymgr unexport acme User
```

### `api snapshot`

Write the current derived API snapshot.

```text
pymgr api snapshot
```

### `api diff`

Compare current exports and signatures with the saved snapshot.

```text
pymgr api diff
```

### `api check`

Perform the same comparison and exit nonzero for removed or changed API.

```text
pymgr api check
```

This is the command to place in CI after intentionally committing a snapshot.

## 7. Refactoring

Refactors show a plan by default. Add `--apply` only after reviewing its files,
move destination, and warnings.

### `move`

Move a module or package while updating static imports, relative imports,
re-exports, and `__all__` entries.

```text
pymgr move acme.old_models acme.models
pymgr move acme.old_models acme.models --apply
```

Dynamic imports, strings, registries, and reflective references remain warnings
for manual review.

### `rename`

Rename a qualified definition where semantic resolution is certain.

```text
pymgr rename acme.models.LegacyUser User
pymgr rename acme.models.LegacyUser User --apply
```

## 8. Loop analysis

Static findings distinguish semantic facts from heuristics. Their performance
field remains `unmeasured` until an explicit workload comparison succeeds.

### `loops`

Inventory loops and iterator constructs in all source roots or selected paths.

```text
pymgr loops
pymgr loops src/acme
pymgr loops src/acme/service.py
```

The analyzer recognizes `for`, `while`, async iteration, comprehensions,
generators, reductions, `any`/`all`, `enumerate`, `zip`, iterator adapters,
common accumulator/index patterns, and declared NumPy, pandas, or Polars APIs.

### `loop explain`

Explain the smallest construct containing a source line, including intent,
blockers, alternatives, and a suggested code shape when one is safe.

```text
pymgr loop explain src/acme/service.py:42
pymgr --json loop explain src/acme/service.py:42
```

### `loop compare`

Compare original source with a complete analyzer-generated candidate in fresh,
disposable workspace copies. The command after `--` must verify equivalence and
exit nonzero on failure.

```text
pymgr loop compare src/acme/service.py:42 -- python check_workload.py
pymgr loop compare --warmups 2 --runs 10 --timeout 120 \
  src/acme/service.py:42 -- python -m pytest tests/test_service.py
```

The report records source hashes, interpreter identity, command, absolute
samples, median, mean, standard deviation, range, and workload identity under
`.pymgr/loop-comparisons/`. A result may be `inconclusive`; it applies only to
that interpreter and workload.

## 9. Runtime intelligence

Tracing is opt-in and supports Python 3.12+ interpreters and Python console
scripts. It does not install an agent, change `PYTHONPATH`, use `sitecustomize`,
or alter the target environment.

### `trace`

Trace a Python script, module, inline command, or Python console script:

```text
pymgr trace -- python src/acme/main.py
pymgr trace -- python -m pytest tests/test_service.py
pymgr trace -- pytest tests/test_service.py
pymgr trace -- python -c "from acme import main; main()"
```

The trace command uses the exact interpreter or console script named after
`--`; it does not silently substitute another environment. When `python` on
`PATH` is ambiguous, use the synchronized interpreter shown by `pymgr status`:

```text
pymgr trace -- .venv/bin/python -m pytest tests/test_service.py
```

Target selected loops for entry, header-hit, and elapsed-time observations:

```text
pymgr trace --loop src/acme/service.py:42 -- python workload.py
pymgr trace --loop src/acme/a.py:10 --loop src/acme/b.py:20 -- pytest
```

The child process's exit status becomes the `pymgr` exit status. SQLite reports
under `.pymgr/traces/` contain code identities, files, lines, call counts,
elapsed time, exception type names, handled/raised events, import names, and
targeted loop counts. Argument values, return values, exception messages, and
object representations are not recorded.

### `trace report`

Summarize the newest trace or a specific report.

```text
pymgr trace report
pymgr trace report .pymgr/traces/20260721T120000.000000Z-abcd.sqlite
pymgr --json trace report
```

### `uses`

Combine static import evidence with observed calls from the newest or selected
trace. Evidence classes remain separate.

```text
pymgr uses acme.services.find_user
pymgr uses acme.services.find_user --trace .pymgr/traces/example.sqlite
```

An absent observed call means only that the traced workload did not execute it.

### `callers`

Show static importing modules and observed runtime callers.

```text
pymgr callers acme.services.find_user
pymgr callers acme.services.find_user --trace .pymgr/traces/example.sqlite
```

## 10. PyCharm

Build the optional plugin:

```text
cd /path/to/zinc/build-tools/pymgr/pycharm-plugin
./gradlew buildPlugin
```

Install the ZIP from `build/distributions/` using **Settings > Plugins > Install
Plugin from Disk**. Launch PyCharm with `pymgr` on `PATH`; alternatively set
`PYMGR_EXECUTABLE=/absolute/path/to/pymgr` before launching it.

Open the target directory itself as the PyCharm project, then select **View >
Tool Windows > pymgr**. The status line shows the recorded generation,
synchronization state, and whether PyCharm's selected SDK matches pymgr's
interpreter. Use **Sync** first when refresh is required, then **Use
Interpreter** if the matching interpreter is already registered in PyCharm.
If it is not registered, add the interpreter in PyCharm's Python Interpreter
settings and press **Use Interpreter** again.

The **pymgr** tool window:

- Watches workspace generation and synchronization state.
- Compares the PyCharm SDK with the synchronized interpreter.
- Can select an already-configured matching interpreter.
- Requests VFS and code-analysis refresh after synchronization.
- Runs add, remove, update, doctor, module, and cycle actions.
- Presents loop explanations/comparisons, uses, callers, and trace reports.
- Previews move/rename plans and requires confirmation before applying them.

The plugin is an RPC client. Closing it or uninstalling it does not affect the
project. The plugin's own
[README](../build-tools/pymgr/pycharm-plugin/README.md) contains the full
build, install, configuration, usage, and troubleshooting procedure.

## 11. Editor RPC

### `serve --stdio`

Start newline-delimited JSON-RPC 2.0 protocol version `1.0` over stdin/stdout.
This is intended for editor integrations, not normal interactive use.

```text
pymgr --root /path/to/acme serve --stdio
```

Initialize with one JSON object per line:

```json
{"jsonrpc":"2.0","id":1,"method":"pymgr/initialize","params":{"protocolVersion":"1.0"}}
```

The service exposes workspace/doctor state, modules, cycles, loop results,
runtime evidence, transactional dependencies, and preview-first refactors. It
opens no network listener.

## 12. CI example

```text
pymgr sync --check
pymgr doctor
pymgr cycles
pymgr api check
pymgr run -- pytest
```

Use `--json` when CI needs structured artifacts. Do not commit `.pymgr/` trace,
comparison, or environment state.

## 13. Troubleshooting

- Run `pymgr doctor` before manually changing site-packages.
- Run `pymgr sync` when `status` reports stale lock or interpreter state.
- Put global `--root` and `--json` before the command name.
- Put workload commands after `--` for `trace` and `loop compare`.
- Use the same Python 3.12+ interpreter that owns the workload for tracing;
  pass the interpreter path from `pymgr status` when `PATH` is ambiguous.
- Treat dynamic-reference warnings as manual-review work, not safe rewrites.
- Delete `.pymgr/` to discard derived state; then run `pymgr sync` to recreate
  synchronized state.
