from __future__ import annotations

import json
from dataclasses import asdict
from typing import IO, Any, Callable

from pymgr.analysis import ProjectIndex
from pymgr.loops import LoopAnalyzer
from pymgr.refactor import (
    apply_plan,
    plan_module_move,
    plan_symbol_rename,
)
from pymgr.workspace import Workspace, WorkspaceError


PROTOCOL_VERSION = "1.0"


class RpcFailure(RuntimeError):
    def __init__(self, code: int, message: str, data: object | None = None) -> None:
        super().__init__(message)
        self.code = code
        self.data = data


class RpcServer:
    def __init__(self, workspace: Workspace) -> None:
        self.workspace = workspace
        self.shutdown_requested = False
        self.methods: dict[str, Callable[[dict[str, object]], object]] = {
            "pymgr/initialize": self._initialize,
            "workspace/status": self._workspace_status,
            "workspace/doctor": self._workspace_doctor,
            "modules/list": self._modules_list,
            "imports/cycles": self._imports_cycles,
            "loops/list": self._loops_list,
            "loops/explain": self._loops_explain,
            "dependencies/mutate": self._dependencies_mutate,
            "refactors/move": self._refactors_move,
            "refactors/rename": self._refactors_rename,
            "pymgr/shutdown": self._shutdown,
        }

    def handle(self, message: object) -> dict[str, object] | None:
        request_id: object = None
        notification = False
        try:
            if not isinstance(message, dict):
                raise RpcFailure(-32600, "request must be a JSON object")
            request_id = message.get("id")
            if message.get("jsonrpc") != "2.0" or not isinstance(
                message.get("method"), str
            ):
                raise RpcFailure(-32600, "invalid JSON-RPC 2.0 request")
            notification = "id" not in message
            raw_params = message.get("params", {})
            if not isinstance(raw_params, dict):
                raise RpcFailure(-32602, "params must be an object")
            method = self.methods.get(message["method"])
            if method is None:
                raise RpcFailure(-32601, f"unknown method: {message['method']}")
            result = method(raw_params)
            if notification:
                return None
            return {"jsonrpc": "2.0", "id": request_id, "result": result}
        except WorkspaceError as error:
            failure = RpcFailure(-32010, str(error))
        except RpcFailure as error:
            failure = error
        except (TypeError, ValueError) as error:
            failure = RpcFailure(-32602, str(error))
        except Exception:
            failure = RpcFailure(-32603, "internal pymgr error")
        if notification:
            return None
        payload: dict[str, object] = {
            "code": failure.code,
            "message": str(failure),
        }
        if failure.data is not None:
            payload["data"] = failure.data
        return {"jsonrpc": "2.0", "id": request_id, "error": payload}

    def _initialize(self, params: dict[str, object]) -> dict[str, object]:
        requested = params.get("protocolVersion")
        if requested is not None and requested != PROTOCOL_VERSION:
            raise RpcFailure(
                -32001,
                "unsupported protocol version",
                {"requested": requested, "supported": PROTOCOL_VERSION},
            )
        state, synchronized = self.workspace.status()
        return {
            "protocolVersion": PROTOCOL_VERSION,
            "root": str(self.workspace.root),
            "generation": state.generation,
            "workspaceStatus": state.status,
            "synchronized": synchronized,
            "capabilities": {
                "generationWatcher": True,
                "interpreterVerification": True,
                "dependencyMutations": ["sync", "check", "add", "remove", "update"],
                "moduleDiagnostics": True,
                "refactorPreview": ["move", "rename"],
                "loopAnalysis": ["list", "explain"],
            },
        }

    def _workspace_status(self, params: dict[str, object]) -> dict[str, object]:
        _no_params(params)
        state, synchronized = self.workspace.status()
        return {
            "root": str(self.workspace.root),
            **asdict(state),
            "synchronized": synchronized,
        }

    def _workspace_doctor(self, params: dict[str, object]) -> dict[str, object]:
        _no_params(params)
        findings = [asdict(item) for item in self.workspace.doctor()]
        return {
            "ready": not any(item["level"] == "error" for item in findings),
            "findings": findings,
        }

    def _modules_list(self, params: dict[str, object]) -> dict[str, object]:
        _no_params(params)
        index = ProjectIndex(self.workspace).build()
        return {
            "modules": [
                {
                    "name": module.name,
                    "path": str(module.path),
                    "package": module.is_package,
                    "owner": module.owner,
                    "exports": module.exports,
                }
                for module in sorted(index.modules.values(), key=lambda item: item.name)
            ],
            "issues": [asdict(issue) for issue in index.issues],
        }

    def _imports_cycles(self, params: dict[str, object]) -> dict[str, object]:
        _no_params(params)
        index = ProjectIndex(self.workspace).build()
        return {"cycles": index.cycles()}

    def _loops_list(self, params: dict[str, object]) -> dict[str, object]:
        paths = _strings(params, "paths", default=())
        findings = LoopAnalyzer(self.workspace).scan(paths)
        return {
            "findings": [
                {"location": finding.location, **asdict(finding)}
                for finding in findings
            ]
        }

    def _loops_explain(self, params: dict[str, object]) -> dict[str, object]:
        location = _string(params, "location")
        finding = LoopAnalyzer(self.workspace).explain(location)
        return {"location": finding.location, **asdict(finding)}

    def _dependencies_mutate(self, params: dict[str, object]) -> dict[str, object]:
        operation = _string(params, "operation")
        if operation not in {"sync", "check", "add", "remove", "update"}:
            raise RpcFailure(-32602, f"unsupported dependency operation: {operation}")
        packages = _strings(params, "packages", default=())
        all_packages = _boolean(params, "all", default=False)
        if operation == "update" and not packages and not all_packages:
            raise RpcFailure(-32602, "update requires packages or all=true")
        if operation == "update" and packages and all_packages:
            raise RpcFailure(-32602, "update accepts packages or all=true, not both")
        if operation in {"add", "remove"} and not packages:
            raise RpcFailure(-32602, f"{operation} requires packages")
        return asdict(self.workspace.mutate(operation, packages))

    def _refactors_move(self, params: dict[str, object]) -> dict[str, object]:
        old = _string(params, "oldModule")
        new = _string(params, "newModule")
        apply = _boolean(params, "apply", default=False)
        plan = plan_module_move(ProjectIndex(self.workspace).build(), old, new)
        if apply:
            apply_plan(plan)
        return _plan_payload(plan, apply)

    def _refactors_rename(self, params: dict[str, object]) -> dict[str, object]:
        symbol = _string(params, "qualifiedSymbol")
        new_name = _string(params, "newName")
        apply = _boolean(params, "apply", default=False)
        plan = plan_symbol_rename(
            ProjectIndex(self.workspace).build(), symbol, new_name
        )
        if apply:
            apply_plan(plan)
        return _plan_payload(plan, apply)

    def _shutdown(self, params: dict[str, object]) -> dict[str, object]:
        _no_params(params)
        self.shutdown_requested = True
        return {"shutdown": True}


def serve_stdio(
    workspace: Workspace,
    input_stream: IO[str],
    output_stream: IO[str],
) -> None:
    server = RpcServer(workspace)
    for line in input_stream:
        if not line.strip():
            continue
        try:
            message = json.loads(line)
        except json.JSONDecodeError as error:
            response: dict[str, object] | None = {
                "jsonrpc": "2.0",
                "id": None,
                "error": {"code": -32700, "message": f"parse error: {error.msg}"},
            }
        else:
            response = server.handle(message)
        if response is not None:
            output_stream.write(
                json.dumps(response, sort_keys=True, default=str) + "\n"
            )
            output_stream.flush()
        if server.shutdown_requested:
            break


def _plan_payload(plan: Any, applied: bool) -> dict[str, object]:
    return {
        "summary": plan.summary,
        "applied": applied,
        "files": [str(change.path) for change in plan.changes],
        "move": {"from": str(plan.source), "to": str(plan.destination)}
        if plan.source
        else None,
        "warnings": list(plan.warnings),
    }


def _no_params(params: dict[str, object]) -> None:
    if params:
        raise RpcFailure(-32602, "method does not accept params")


def _string(params: dict[str, object], name: str) -> str:
    value = params.get(name)
    if not isinstance(value, str) or not value:
        raise RpcFailure(-32602, f"{name} must be a non-empty string")
    return value


def _strings(
    params: dict[str, object], name: str, *, default: tuple[str, ...]
) -> tuple[str, ...]:
    value = params.get(name, default)
    if not isinstance(value, (list, tuple)) or not all(
        isinstance(item, str) and item for item in value
    ):
        raise RpcFailure(-32602, f"{name} must be a list of non-empty strings")
    return tuple(value)


def _boolean(params: dict[str, object], name: str, *, default: bool) -> bool:
    value = params.get(name, default)
    if not isinstance(value, bool):
        raise RpcFailure(-32602, f"{name} must be a boolean")
    return value
