"""CORTEX Personal Health AI: grounded questions, record search through MCP, and consent enforcement."""
import uuid

from conftest import add_verified, ago, wait_until
from test_vault import processed


def seed(api, profile):
    document = api.ok(api.upload_pdf(profile, "Hemoglobin A1c 5.4 % Reference 4.0-5.6 %")[0], 202)["id"]
    processed(api, document)
    api.ok(api.post(f"/profiles/{profile}/documents/{document}/approve", {"biomarkers": [
        {"biomarker_code": "HBA1C", "name": "Hemoglobin A1c", "value": 5.4, "unit": "%", "observed_at": ago(7), "reference_range": "4.0-5.6"}]}), 200, 201)
    add_verified(api, profile, "GLUCOSE", 92, "mg/dL", ago(3), "70-99")


def test_question_is_grounded_and_cited(api, profile):
    seed(api, profile)
    answer = api.ok(api.post(f"/profiles/{profile}/questions", {"question": "What was my latest HbA1c and is it in range?"}), 200)
    assert answer["answer"]
    assert "sources" in answer
    assert "limitations" in answer or "workflow" in answer


def test_question_validation_and_consent(api, profile, bare_profile):
    assert api.post(f"/profiles/{profile}/questions", {"question": "  "}).status_code == 400
    assert api.post(f"/profiles/{profile}/questions", {}).status_code == 400
    assert api.post(f"/profiles/{bare_profile}/questions", {"question": "hello"}).status_code == 403
    assert api.post(f"/profiles/{uuid.uuid4()}/questions", {"question": "hello"}).status_code == 404
    api.ok(api.put(f"/profiles/{bare_profile}/consents/medical_record_storage", {"granted": True}), 200)
    assert api.post(f"/profiles/{bare_profile}/questions", {"question": "hello"}).status_code == 403, "AI processing consent is separate"


def test_question_without_records_is_safe(api, profile):
    answer = api.ok(api.post(f"/profiles/{profile}/questions", {"question": "Do I have a thyroid problem?"}), 200)
    assert answer["answer"]
    assert "diagnos" not in answer["answer"].lower() or "not" in answer["answer"].lower()


def rpc(api, token, method, params=None, id_=1):
    body = {"jsonrpc": "2.0", "method": method, "id": id_}
    if params is not None:
        body["params"] = params
    return api.post("/mcp", body, token=token)


def test_mcp_records_server(api, profile):
    seed(api, profile)
    grant = api.ok(api.post(f"/profiles/{profile}/mcp-tokens"), 201)
    token = grant["token"]
    assert api.post(f"/profiles/{uuid.uuid4()}/mcp-tokens").status_code == 404

    assert rpc(api, token, "initialize").json()["result"]["serverInfo"]["name"] == "cortex-records"
    assert rpc(api, token, "ping").json()["result"] == {}
    assert rpc(api, token, "notifications/initialized").status_code == 202
    tools = {t["name"] for t in rpc(api, token, "tools/list").json()["result"]["tools"]}
    assert {"search_records", "list_observations", "biomarker_history"} <= tools
    assert rpc(api, token, "nope").json()["error"]["code"] == -32601

    def call(name, **arguments):
        return rpc(api, token, "tools/call", {"name": name, "arguments": arguments}).json()

    assert "result" in call("list_observations", limit=5)
    assert "result" in call("biomarker_history", code="hba1c")
    assert "result" in call("biomarker_history", code="")
    assert "result" in call("search_records", query="hemoglobin", limit=3)
    assert "result" in call("search_records", query="")
    assert call("no_such_tool")["error"]["code"] == -32602
    assert rpc(api, token, "tools/call", "bad-params").json().get("error")

    assert api.post("/mcp", {"jsonrpc": "2.0", "method": "ping", "id": 1}, token="forged").status_code == 401
    assert api.post("/mcp", {"jsonrpc": "2.0", "method": "ping", "id": 1}, token="").status_code == 401

    # Withdrawing AI consent immediately disables the record tools.
    api.ok(api.put(f"/profiles/{profile}/consents/ai_processing", {"granted": False}), 200)
    blocked = call("list_observations")
    assert "consent" in str(blocked).lower()
