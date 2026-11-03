"""The Python AI service called directly: health, embeddings, RAG answer workflow, safety and research boundaries."""
import requests

from conftest import AI_URL


def get(path, **kw):
    return requests.get(AI_URL + path, timeout=60, **kw)


def post(path, body, **kw):
    return requests.post(AI_URL + path, json=body, timeout=120, **kw)


def test_health_capabilities_and_models():
    assert get("/healthz").json()["status"] == "ok"
    assert get("/readyz").status_code == 200
    caps = get("/v1/capabilities").json()
    assert caps["orchestration"] == "langgraph"
    assert {"medical_records", "lab_intelligence", "health_research"} <= set(caps["agents"])
    assert isinstance(get("/v1/models").json(), list)
    assert "model" in get("/v1/embedding").json() or get("/v1/embedding").status_code == 200


def test_embeddings():
    out = post("/v1/embed", {"texts": ["hemoglobin a1c", "ldl cholesterol"], "kind": "passage"}).json()
    assert out["dimensions"] > 0 and len(out["vectors"]) == 2
    assert post("/v1/embed", {"texts": ["glucose"], "kind": "query"}).status_code == 200
    assert post("/v1/embed", {"texts": []}).status_code == 422
    assert post("/v1/embed", {"texts": ["x"], "kind": "bogus"}).status_code == 422
    assert post("/v1/embed", {"texts": ["x" * 2500]}).status_code == 422


def test_rerank_is_configuration_gated():
    assert post("/v1/rerank", {"query": "a1c", "passages": ["hemoglobin a1c", "weather"]}).status_code in (200, 501)
    assert post("/v1/rerank", {"query": "", "passages": []}).status_code == 422


def test_answer_workflow_cites_and_limits():
    body = {
        "question": "Is my HbA1c in range?",
        "observations": [{"name": "Hemoglobin A1c", "value": 5.4, "unit": "%", "observed_at": "2026-01-10", "reference_range": "4.0-5.6"},
                         {"name": "Glucose", "value": 120, "unit": "mg/dL", "reference_range": "70-99"}],
        "record_chunks": [{"observation_id": "o1", "content": "Hemoglobin A1c 5.4 % on 2026-01-10", "score": 0.9}],
        "research_chunks": [{"source_id": "s1", "identifier": "PMID:1", "title": "HbA1c targets", "url": "https://pubmed.ncbi.nlm.nih.gov/1/", "content": "Targets vary.", "score": 0.5}],
    }
    out = post("/v1/answer", body).json()
    assert out["answer"] and out["limitations"] and out["workflow"]
    assert isinstance(out["sources"], list)
    empty = post("/v1/answer", {"question": "What should I take for chest pain?"}).json()
    assert empty["answer"]
    assert post("/v1/answer", {"question": ""}).status_code == 422
    assert post("/v1/answer", {"question": "x", "observations": [{"name": "a"}]}).status_code == 422
    for question in ("Explain the lab term hemoglobin", "Compare my reports and flag anything missing", "Summarize recent research on statins", "Prepare a summary for my doctor visit"):
        assert post("/v1/answer", {"question": question, "observations": body["observations"]}).status_code == 200


def test_research_endpoints_respect_the_egress_allowlist():
    assert post("/v1/research/biomarker", {"term": "x"}).status_code == 422
    assert post("/v1/research/biomarker", {"term": "ferritin", "medlineplus_url": "https://evil.example/x"}).status_code == 422
    assert post("/v1/research/discover", {"limit": 0}).status_code == 422
    assert get("/v1/research/convert").status_code == 422
    assert get("/v1/research/unit").status_code == 422


def test_research_sources_when_reachable():
    unit = get("/v1/research/unit", params={"unit": "mg/dL"})
    if unit.status_code != 200:
        import pytest
        pytest.skip("UCUM unreachable from the runner")
    assert unit.json()["valid_ucum"] is True
    assert get("/v1/research/unit", params={"unit": "not a unit!!"}).status_code in (200, 502)
    conv = get("/v1/research/convert", params={"from_unit": "mg/dL", "to_unit": "g/L"})
    assert conv.status_code == 200 and abs(conv.json()["conversion"]["factor"] - 0.01) < 1e-9
    assert get("/v1/research/convert", params={"from_unit": "mg/dL", "to_unit": "mmol/L"}).json()["conversion"] is None
    assert post("/v1/research/literature", {"term": "hemoglobin a1c"}).status_code in (200, 502)
    assert post("/v1/research/biomarker", {"term": "ferritin"}).status_code in (200, 502)
    assert post("/v1/research/discover", {"limit": 3, "offset": 0}).status_code in (200, 502)
