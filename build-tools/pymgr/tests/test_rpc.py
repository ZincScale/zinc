from __future__ import annotations

import io
import json
from pathlib import Path

from pymgr.rpc import PROTOCOL_VERSION, RpcServer, serve_stdio
from pymgr.workspace import State, Workspace


def _request(
    method: str,
    params: dict[str, object] | None = None,
    *,
    request_id: int = 1,
) -> dict[str, object]:
    request: dict[str, object] = {
        "jsonrpc": "2.0",
        "id": request_id,
        "method": method,
    }
    if params is not None:
        request["params"] = params
    return request


def _rpc_project(project: Path) -> Path:
    package = project / "src" / "acme"
    (package / "__init__.py").write_text(
        'from acme.work import collect\n\n__all__ = ["collect"]\n',
        encoding="utf-8",
    )
    module = package / "work.py"
    module.write_text(
        """def collect(items):
    result = []
    for item in items:
        result.append(item.name)
    return result
""",
        encoding="utf-8",
    )
    return module


def test_rpc_initialize_exposes_generation_and_editor_capabilities(
    project: Path,
) -> None:
    workspace = Workspace(project)
    workspace.save_state(
        State(generation=4, status="ready", python="/tmp/python", lock_hash="abc")
    )

    response = RpcServer(workspace).handle(
        _request("pymgr/initialize", {"protocolVersion": PROTOCOL_VERSION})
    )

    assert response is not None
    result = response["result"]
    assert isinstance(result, dict)
    assert result["protocolVersion"] == PROTOCOL_VERSION
    assert result["generation"] == 4
    assert result["capabilities"]["generationWatcher"] is True
    assert result["capabilities"]["refactorPreview"] == ["move", "rename"]


def test_rpc_lists_modules_and_explains_loops(project: Path) -> None:
    module = _rpc_project(project)
    server = RpcServer(Workspace(project))

    modules_response = server.handle(_request("modules/list"))
    loop_response = server.handle(
        _request("loops/explain", {"location": f"{module}:3"}, request_id=2)
    )

    assert modules_response is not None
    modules = modules_response["result"]["modules"]
    assert [item["name"] for item in modules] == ["acme", "acme.work"]
    assert loop_response is not None
    finding = loop_response["result"]
    assert finding["construct"] == "for"
    assert "list comprehension" in finding["recommendation"]
    assert finding["performance"] == "unmeasured"


def test_rpc_refactor_is_preview_only_by_default(project: Path) -> None:
    module = _rpc_project(project)
    destination = module.with_name("tasks.py")

    response = RpcServer(Workspace(project)).handle(
        _request(
            "refactors/move",
            {"oldModule": "acme.work", "newModule": "acme.tasks"},
        )
    )

    assert response is not None
    result = response["result"]
    assert result["applied"] is False
    assert result["move"] == {"from": str(module), "to": str(destination)}
    assert module.exists()
    assert not destination.exists()


def test_rpc_rejects_unknown_versions_methods_and_invalid_params(
    project: Path,
) -> None:
    server = RpcServer(Workspace(project))

    version = server.handle(_request("pymgr/initialize", {"protocolVersion": "99"}))
    missing = server.handle(_request("missing/method", request_id=2))
    params = server.handle(_request("loops/explain", {"location": 42}, request_id=3))

    assert version is not None and version["error"]["code"] == -32001
    assert missing is not None and missing["error"]["code"] == -32601
    assert params is not None and params["error"]["code"] == -32602


def test_stdio_server_frames_responses_and_ignores_notifications(
    project: Path,
) -> None:
    requests = [
        _request("pymgr/initialize", request_id=1),
        {"jsonrpc": "2.0", "method": "workspace/status"},
        _request("missing/method", request_id=2),
        _request("pymgr/shutdown", request_id=3),
    ]
    input_stream = io.StringIO(
        "\n".join(json.dumps(request) for request in requests) + "\n"
    )
    output_stream = io.StringIO()

    serve_stdio(Workspace(project), input_stream, output_stream)

    responses = [json.loads(line) for line in output_stream.getvalue().splitlines()]
    assert [response["id"] for response in responses] == [1, 2, 3]
    assert responses[0]["result"]["protocolVersion"] == PROTOCOL_VERSION
    assert responses[1]["error"]["code"] == -32601
    assert responses[2]["result"] == {"shutdown": True}


def test_stdio_server_reports_invalid_json(project: Path) -> None:
    output_stream = io.StringIO()

    serve_stdio(Workspace(project), io.StringIO("not-json\n"), output_stream)

    response = json.loads(output_stream.getvalue())
    assert response["id"] is None
    assert response["error"]["code"] == -32700
