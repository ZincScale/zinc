# pymgr

`pymgr` is an optional development coordinator for ordinary Python projects.
It provides transactional uv environment changes, module/API diagnostics,
module and package scaffolding, explicit public-API management, preview-first
refactors, loop guidance and measurement, opt-in runtime tracing, and a PyCharm
integration. Application code never imports or depends on it.

## Quick start

Install the development tool from this checkout:

```text
cd build-tools/pymgr
./install.sh
```

The installer uses uv internally to make the `pymgr` command available. Once
installed, project workflows go through pymgr.

Create a new project entirely through the pymgr interface:

```text
pymgr new /path/to/python-project --python 3.14
cd /path/to/python-project
pymgr doctor
pymgr run -- python-project
```

Or initialize and synchronize an existing packaged project:

```text
cd /path/to/python-project
pymgr init
pymgr sync
pymgr doctor
```

Create package structure and manage public APIs without hand-editing
`__init__.py`:

```text
pymgr module create acme.billing.models
pymgr module create acme.billing.providers --package
# Write the Model logic in src/acme/billing/models.py.
pymgr export acme.billing.models Model
pymgr export acme.billing Model --from acme.billing.models
```

The target must use `pyproject.toml`, a `src/` layout, regular packages, and
Python 3.12 or newer. uv remains pymgr's internal environment engine, while
project-facing workflows use `pymgr`. `pymgr init` adds `[tool.pymgr]` only
when it is missing and creates disposable `.pymgr/` state.

Read the complete [getting-started and command guide](../../docs/python-development-tool-getting-started.md)
for project setup, workflows, output interpretation, PyCharm installation, and
an example for every command. The architectural and safety rules live in the
[design contract](../../docs/python-development-tool-design.md).

## Development

```text
pymgr sync
pymgr run -- pytest
pymgr run -- ruff check src tests
pymgr --help
```

Build the optional PyCharm plugin:

```text
cd pycharm-plugin
./gradlew buildPlugin
```

Install the generated ZIP from `pycharm-plugin/build/distributions/` using
PyCharm's **Settings > Plugins > Install Plugin from Disk**. See the plugin's
[standalone usage guide](pycharm-plugin/README.md) for configuration, every
tool-window action, and troubleshooting.

## Safety model

- Dependency mutations use uv, a workspace lock, validation, and rollback.
- Refactors and import cleanup are previews unless `--apply` is explicit.
- Loop speed claims exist only after a named workload is measured.
- Tracing launches only the requested Python child process and stores no values
  or object representations.
- `.pymgr/` contains disposable state, reports, and traces.
- Removing the CLI, plugin, or `.pymgr/` does not change application behavior.
