import json

from fastapi.testclient import TestClient

from app import config
from app.inference import generate
from app.main import app
from app.rag.hashing import DIMENSIONS, HashingEmbedder

client = TestClient(app)


def test_hashing_embedder_is_deterministic_normalised_and_lexically_similar():
    e = HashingEmbedder()
    a, b, c = e.embed_passages(["HbA1c 6.1 % on 2026-03-01", "HbA1c 6.1 %", "LDL cholesterol 130 mg/dL"])
    assert len(a) == DIMENSIONS and a == e.embed_passages(["HbA1c 6.1 % on 2026-03-01"])[0]
    dot = lambda x, y: sum(p * q for p, q in zip(x, y))
    assert abs(dot(a, a) - 1) < 1e-6
    assert dot(a, b) > dot(a, c)


def test_embed_endpoint():
    r = client.post("/v1/embed", json={"texts": ["a b"], "kind": "query"})
    assert r.status_code == 200
    body = r.json()
    assert body["dimensions"] == DIMENSIONS and len(body["vectors"]) == 1
    assert client.post("/v1/embed", json={"texts": [], "kind": "query"}).status_code == 422
    assert client.post("/v1/embed", json={"texts": ["x"], "kind": "bad"}).status_code == 422


def test_answer_uses_record_chunks():
    r = client.post(
        "/v1/answer",
        json={
            "question": "what is my a1c",
            "observations": [{"name": "HbA1c", "value": 6.1, "unit": "%", "observed_at": "2026-03-01"}],
            "record_chunks": [{"observation_id": "o1", "content": "HbA1c: 6.1 % on 2026-03-01", "score": 0.9}],
        },
    )
    assert r.status_code == 200
    assert any(s.get("observation_id") == "o1" for s in r.json()["sources"])


def test_ollama_adapter(monkeypatch):
    captured = {}

    class Resp:
        def __enter__(self):
            return self

        def __exit__(self, *a):
            return False

        def read(self, n):
            return json.dumps({"message": {"content": " Your HbA1c is 6.1 %. "}}).encode()

    def fake_open(request, timeout):
        captured["url"] = request.full_url
        captured["body"] = json.loads(request.data)
        return Resp()

    monkeypatch.setattr(config, "OLLAMA_MODEL", "qwen2.5:1.5b")
    monkeypatch.setattr("urllib.request.urlopen", fake_open)
    assert generate.generate_explanation("HbA1c: 6.1 %", "q") == "Your HbA1c is 6.1 %."
    assert captured["url"].endswith("/api/chat") and captured["body"]["model"] == "qwen2.5:1.5b"
