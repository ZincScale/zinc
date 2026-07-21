from __future__ import annotations

import json
import subprocess
import sys
from pathlib import Path

import pytest

from pymgr.workspace import Workspace, WorkspaceError


class FakeUV:
    def __init__(self, fail_sync: bool = False) -> None:
        self.fail_sync = fail_sync
        self.commands: list[tuple[str, ...]] = []

    def __call__(self, root: Path, args) -> subprocess.CompletedProcess[str]:
        command = tuple(args)
        self.commands.append(command)
        if command[:2] == ("uv", "lock"):
            (root / "uv.lock").write_text("version = 1\n", encoding="utf-8")
        if command[:2] == ("uv", "add"):
            (root / "pyproject.toml").write_text("mutated\n", encoding="utf-8")
            (root / "uv.lock").write_text("mutated-lock\n", encoding="utf-8")
        if command[:2] == ("uv", "sync") and self.fail_sync:
            return subprocess.CompletedProcess(args, 1, "", "synthetic sync failure")
        if command[:3] == ("uv", "run", "--locked"):
            output = json.dumps(
                {
                    "python": sys.executable,
                    "version": sys.version.split()[0],
                    "distributions": 1,
                }
            )
            return subprocess.CompletedProcess(args, 0, output + "\n", "")
        return subprocess.CompletedProcess(args, 0, "", "")


class InitializingUV(FakeUV):
    def __call__(self, root: Path, args) -> subprocess.CompletedProcess[str]:
        if args[:3] == ["uv", "init", "--package"]:
            target = Path(args[-1])
            project_name = (
                args[args.index("--name") + 1] if "--name" in args else target.name
            )
            package_name = project_name.replace("-", "_")
            target.mkdir()
            (target / "src" / package_name).mkdir(parents=True)
            (target / "src" / package_name / "__init__.py").write_text(
                'def main() -> None:\n    print("hello")\n', encoding="utf-8"
            )
            (target / "pyproject.toml").write_text(
                f'''[project]
name = "{project_name}"
version = "0.1.0"

[build-system]
requires = ["uv_build"]
build-backend = "uv_build"
''',
                encoding="utf-8",
            )
        return super().__call__(root, args)


class MissingEditableUV(FakeUV):
    def __call__(self, root: Path, args) -> subprocess.CompletedProcess[str]:
        if args[:3] == ["uv", "run", "--locked"] and any(
            "direct_url.json" in argument for argument in args
        ):
            output = json.dumps(
                {
                    "python": sys.executable,
                    "version": sys.version.split()[0],
                    "prefix": str(root / ".venv"),
                    "sys_path": [],
                    "distributions": {},
                }
            )
            return subprocess.CompletedProcess(args, 0, output + "\n", "")
        return super().__call__(root, args)


def test_sync_is_transactional_and_publishes_generation(project: Path) -> None:
    runner = FakeUV()
    workspace = Workspace(project, runner)
    workspace.init()

    state = workspace.mutate("sync")

    assert state.status == "ready"
    assert state.generation == 1
    assert state.lock_hash
    assert workspace.status()[1]
    assert not workspace.lock_path.exists()
    assert ("uv", "sync", "--locked") in runner.commands


def test_create_bootstraps_package_public_api_and_environment(tmp_path: Path) -> None:
    runner = InitializingUV()
    target = tmp_path / "sample-project"

    workspace, state = Workspace.create(
        target,
        name="sample-project",
        python="3.14",
        description="Example package",
        runner=runner,
    )

    assert workspace.root == target
    assert state.status == "ready"
    assert state.generation == 1
    assert (target / "src" / "sample_project" / "__init__.py").read_text(
        encoding="utf-8"
    ) == 'def main() -> None:\n    print("hello")\n\n__all__ = []\n'
    pyproject = (target / "pyproject.toml").read_text(encoding="utf-8")
    assert '[tool.pymgr]\nsource-roots = ["src"]' in pyproject
    assert (
        "uv",
        "init",
        "--package",
        "--python",
        "3.14",
        "--name",
        "sample-project",
        "--description",
        "Example package",
        str(target),
    ) in runner.commands


def test_create_rejects_nonempty_target(tmp_path: Path) -> None:
    target = tmp_path / "existing"
    target.mkdir()
    (target / "notes.txt").write_text("keep me", encoding="utf-8")

    with pytest.raises(WorkspaceError, match="must be absent or empty"):
        Workspace.create(target, runner=InitializingUV())

    assert (target / "notes.txt").read_text(encoding="utf-8") == "keep me"


def test_run_command_uses_locked_environment(project: Path) -> None:
    runner = FakeUV()
    workspace = Workspace(project, runner)
    workspace.init()
    workspace.mutate("sync")

    result = workspace.run_command(["--", "pytest", "-q"])

    assert result.returncode == 0
    assert runner.commands[-1] == (
        "uv",
        "run",
        "--locked",
        "--no-sync",
        "pytest",
        "-q",
    )


def test_locked_check_does_not_resolve_new_versions(project: Path) -> None:
    runner = FakeUV()
    workspace = Workspace(project, runner)
    workspace.init()
    (project / "uv.lock").write_text("version = 1\n", encoding="utf-8")

    state = workspace.mutate("check")

    assert state.status == "ready"
    assert ("uv", "lock", "--check") in runner.commands
    assert ("uv", "lock") not in runner.commands


def test_failed_mutation_restores_metadata(project: Path) -> None:
    original = (project / "pyproject.toml").read_bytes()
    runner = FakeUV(fail_sync=True)
    workspace = Workspace(project, runner)
    workspace.init()
    initialized = (project / "pyproject.toml").read_bytes()

    with pytest.raises(WorkspaceError, match="synthetic sync failure"):
        workspace.mutate("add", ["requests"])

    assert (project / "pyproject.toml").read_bytes() == initialized
    assert not (project / "uv.lock").exists()
    assert workspace.load_state().generation == 0
    assert original in initialized


def test_existing_mutation_lock_is_reported(project: Path) -> None:
    workspace = Workspace(project, FakeUV())
    workspace.state_dir.mkdir()
    workspace.lock_path.write_text("pid=42\n", encoding="utf-8")

    with pytest.raises(WorkspaceError, match="pid=42"):
        with workspace.mutation_lock():
            pass


def test_doctor_explains_unsynchronized_workspace(project: Path) -> None:
    workspace = Workspace(project, FakeUV())
    workspace.init()

    findings = workspace.doctor()

    subjects = {item.subject for item in findings}
    assert "uv.lock" in subjects
    assert "workspace state" in subjects


def test_doctor_detects_missing_editable_workspace_member(
    project: Path, monkeypatch
) -> None:
    pyproject = project / "pyproject.toml"
    pyproject.write_text(
        pyproject.read_text(encoding="utf-8")
        + "\n[build-system]\nrequires = ['uv_build']\nbuild-backend = 'uv_build'\n",
        encoding="utf-8",
    )
    monkeypatch.setattr("pymgr.workspace.shutil.which", lambda _name: "/bin/uv")
    workspace = Workspace(project, MissingEditableUV())
    workspace.init()
    workspace.mutate("sync")

    findings = workspace.doctor()

    assert any(
        finding.subject == "workspace member acme"
        and finding.observed == "not installed"
        for finding in findings
    )
