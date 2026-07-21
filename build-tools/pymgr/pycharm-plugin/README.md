# pymgr PyCharm plugin

This optional plugin presents the `pymgr` CLI's JSON-RPC API in PyCharm. The
CLI remains the source of truth, and neither the plugin nor pymgr becomes an
application dependency.

## Prerequisites

- PyCharm 2026.1 or 2026.2. Both lines pass the plugin verifier; later lines
  should be verified before use.
- `pymgr` installed and able to run from a terminal.
- A target project with `pyproject.toml`, initialized and synchronized with
  `pymgr init` plus `pymgr sync` (or created with `pymgr new`).
- Java 21 or newer to build the plugin. The checked-in Gradle wrapper supplies
  Gradle itself.

Verify the CLI before opening the IDE:

```text
cd /path/to/project
pymgr status
pymgr doctor
```

## Build and install

From this directory:

```text
./gradlew buildPlugin
```

The installable ZIP is written under `build/distributions/`. In PyCharm:

1. Open **Settings > Plugins**.
2. Open the gear menu and choose **Install Plugin from Disk**.
3. Select the generated ZIP without extracting it.
4. Restart PyCharm when prompted.

## Configure the CLI and project

The plugin starts `pymgr --root <project> serve --stdio`. `pymgr` must therefore
be available to the environment that launched PyCharm. Either:

- Put `pymgr` on that environment's `PATH`; or
- Set `PYMGR_EXECUTABLE=/absolute/path/to/pymgr` before launching PyCharm.

Open the directory containing the target `pyproject.toml` as the PyCharm
project. The plugin does not initialize projects from the IDE; create one with
`pymgr new` or adopt it with `pymgr init` and `pymgr sync` first.

## Use the tool window

Open **View > Tool Windows > pymgr**. The status line reports the workspace
generation, synchronization state, and interpreter agreement.

- **Refresh** rereads workspace state.
- **Sync** resolves, synchronizes, validates, and refreshes PyCharm files and
  code analysis.
- **Use Interpreter** selects pymgr's synchronized interpreter when that exact
  interpreter is already registered as a PyCharm SDK. If it is not registered,
  add it in PyCharm's Python Interpreter settings and retry.
- **Doctor**, **Modules**, and **Cycles** show environment and source analysis.
- **New Module** and **New Package** create the requested dotted path, missing
  regular parent packages, `__init__.py` files, and empty `__all__` declarations.
- **Exports** lists a module or package API. **Export** exposes a local
  definition or re-exports from another module; **Unexport** removes it.
- **Add**, **Remove**, and **Update All** perform transactional dependency
  operations through pymgr.
- **Loop** explains a `file.py:line`; **Loop Result** shows the latest measured
  comparison.
- **Uses**, **Callers**, and **Trace Report** display static and opt-in runtime
  evidence produced by the CLI.
- **Move** and **Rename** display pymgr's complete preview and ask for explicit
  confirmation before applying it.

The plugin polls generation state every two seconds. After a successful sync,
dependency mutation, module/package creation, API update, or refactor, it
requests a virtual-file and Python code-analysis refresh.

## Troubleshooting

- **No pyproject.toml was found:** open the actual project root, not a parent or
  source subdirectory.
- **Cannot start pymgr / RPC error:** launch PyCharm from an environment where
  `pymgr --help` works, or set `PYMGR_EXECUTABLE` before launch.
- **Refresh required:** run **Sync** in the tool window or `pymgr sync` in a
  terminal.
- **PyCharm interpreter differs:** register the interpreter path shown by
  `pymgr status`, then press **Use Interpreter**.
- **No loop result or trace report:** create one with `pymgr loop compare ...`
  or `pymgr trace -- ...`, then refresh the view.
- **Plugin compatibility:** run `./gradlew verifyPlugin` for the configured
  PyCharm targets.

Closing, disabling, or uninstalling the plugin leaves the project and its
runtime behavior unchanged.
