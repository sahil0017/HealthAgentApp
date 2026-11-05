from fastapi.testclient import TestClient

from app import config
from app.inference import copilot_agent
from app.main import app

client = TestClient(app)
TURN = {"message": "what is high?", "system": "be careful", "snapshot": {}}


def test_agent_unavailable_without_token(monkeypatch):
    monkeypatch.setattr(config, "COPILOT_TOKEN", "")
    r = client.post("/v1/agent/copilot", json=TURN)
    assert r.status_code == 503 and r.json()["reason"] == "model_not_configured"


def test_agent_returns_answer_and_calls(monkeypatch):
    async def fake(message, system, history, snapshot):
        return {"answer": "ok", "calls": [{"tool": "list_flagged_labs", "args": {}}], "model": "m"}

    monkeypatch.setattr(copilot_agent, "run_agent", fake)
    r = client.post("/v1/agent/copilot", json=TURN)
    assert r.status_code == 200 and r.json()["calls"][0]["tool"] == "list_flagged_labs"


def test_prescription_rejects_non_images():
    r = client.post("/v1/prescription/read", content=b"x", headers={"content-type": "application/pdf"})
    assert r.status_code == 415


def test_prescription_rejects_empty_and_oversized(monkeypatch):
    assert client.post("/v1/prescription/read", content=b"", headers={"content-type": "image/png"}).status_code == 413
    monkeypatch.setattr("app.main.MAX_PRESCRIPTION_BYTES", 4)
    assert client.post("/v1/prescription/read", content=b"12345", headers={"content-type": "image/png"}).status_code == 413


def test_prescription_unavailable_without_token(monkeypatch):
    monkeypatch.setattr(config, "COPILOT_TOKEN", "")
    r = client.post("/v1/prescription/read", content=b"img", headers={"content-type": "image/png"})
    assert r.status_code == 503 and r.json()["error"] == "reader_unavailable"


def test_record_tool_lookup_matches_name_and_reports_missing():
    snap = {"trends": [{"code": "LDL", "name": "LDL Cholesterol", "out": "LDL fact"}]}
    assert copilot_agent._find_trend(snap, "ldl cholesterol") == "LDL fact"
    assert copilot_agent._find_trend(snap, "LDL") == "LDL fact"
    assert "no verified result" in copilot_agent._find_trend(snap, "Ferritin")


def test_standardise_unavailable_without_token(monkeypatch):
    monkeypatch.setattr(config, "COPILOT_TOKEN", "")
    r = client.post("/v1/labs/standardise", json={"labels": ["Cholesterol - Total"]})
    assert r.status_code == 503 and r.json()["reason"] == "model_not_configured"


def test_standardise_returns_names(monkeypatch):
    async def fake(labels):
        return {"tests": [{"label": labels[0], "name": "Total Cholesterol", "panel": "Lipid Profile"}], "model": "m"}

    monkeypatch.setattr(copilot_agent, "standardise_labs", fake)
    r = client.post("/v1/labs/standardise", json={"labels": ["Cholesterol - Total"]})
    assert r.status_code == 200 and r.json()["tests"][0]["name"] == "Total Cholesterol"


def test_standardise_validates_input():
    assert client.post("/v1/labs/standardise", json={"labels": []}).status_code == 422


def test_care_plan_tool_is_exposed():
    calls: list[dict] = []
    names = [getattr(t, "name", None) for t in copilot_agent._record_tools({"get_care_plan": "x"}, calls)]
    assert "get_care_plan" in names


def test_document_extract_unavailable_without_token(monkeypatch):
    monkeypatch.setattr(config, "COPILOT_TOKEN", "")
    r = client.post("/v1/documents/extract", json={"text": "Haemoglobin 13.2 g/dL 12 - 15" * 2})
    assert r.status_code == 503 and r.json()["reason"] == "model_not_configured"


def test_document_extract_returns_tests_and_medicines(monkeypatch):
    async def fake(text):
        return {"document_type": "LAB_REPORT", "tests": [{"name": "Haemoglobin", "value": "13.2"}], "medicines": [], "model": "m"}

    monkeypatch.setattr(copilot_agent, "extract_document", fake)
    r = client.post("/v1/documents/extract", json={"text": "Haemoglobin 13.2 g/dL 12 - 15 range"})
    assert r.status_code == 200 and r.json()["tests"][0]["name"] == "Haemoglobin"


def test_document_extract_validates_input():
    assert client.post("/v1/documents/extract", json={"text": "short"}).status_code == 422
