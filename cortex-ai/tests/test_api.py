from fastapi.testclient import TestClient

from app.main import app

client = TestClient(app)


def test_health_reports_inference_disabled_by_default():
    response = client.get("/healthz")
    assert response.status_code == 200
    assert response.json()["inference"] == "disabled"


def test_models_empty_until_reviewed():
    assert client.get("/v1/models").json() == []


def test_capabilities_report_langgraph():
    body = client.get("/v1/capabilities").json()
    assert body["orchestration"] == "langgraph"
    assert body["model_inference"] == "disabled"


def test_answer_returns_workflow_and_limitations():
    response = client.post(
        "/v1/answer",
        json={
            "question": "Why did my HbA1c change?",
            "observations": [
                {"name": "HbA1c", "value": 5.6, "unit": "%", "observed_at": "2026-01-10T00:00:00Z"},
                {"name": "HbA1c", "value": 6.1, "unit": "%", "observed_at": "2026-06-10T00:00:00Z"},
            ],
        },
    )
    assert response.status_code == 200
    body = response.json()
    assert body["workflow"] == ["medical_records", "lab_intelligence"]
    assert body["sources"] == [{"type": "user_record", "description": "User-approved observations"}]
    assert "HbA1c increased" in body["answer"]
    assert body["limitations"][-1] == "This educational prototype does not provide diagnosis or treatment advice."


def test_answer_endpoint_reports_triage_confidence_and_suggestions():
    from fastapi.testclient import TestClient

    from app.main import app

    client = TestClient(app)
    urgent = client.post("/v1/answer", json={"question": "crushing chest pain right now"}).json()
    assert urgent["urgent"] is True and urgent["triage"] == "emergency" and urgent["workflow"] == []
    calm = client.post("/v1/answer", json={"question": "How is my health?"}).json()
    assert calm["urgent"] is False and calm["confidence"] == "insufficient"
    assert calm["suggested_questions"] and calm["uncertainty"]
    assert client.post("/v1/answer", json={"question": "x" * 4001}).status_code == 422
