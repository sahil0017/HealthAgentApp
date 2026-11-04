import httpx
from fastapi.testclient import TestClient

from app import config
from app.inference import hf_api
from app.main import app

client = TestClient(app)
BODY = {"messages": [{"role": "user", "content": "hi"}], "tools": []}


def test_unavailable_without_token(monkeypatch):
    monkeypatch.setattr(config, "HF_TOKEN", "")
    r = client.post("/v1/agent/step", json=BODY)
    assert r.status_code == 503 and r.json()["reason"] == "model_not_configured"


def test_tool_call_and_answer(monkeypatch):
    monkeypatch.setattr(config, "HF_TOKEN", "t")
    seen = {}

    def fake_post(url, json, headers, timeout):
        seen.update(url=url, auth=headers["Authorization"], tools=json.get("tools"))
        msg = {"role": "assistant", "content": None, "tool_calls": [{"id": "c1", "function": {"name": "get_trend", "arguments": "{\"name\":\"HbA1c\"}"}}]}
        return httpx.Response(200, json={"choices": [{"message": msg}]}, request=httpx.Request("POST", url))

    monkeypatch.setattr(httpx, "post", fake_post)
    tools = [{"type": "function", "function": {"name": "get_trend", "parameters": {"type": "object"}}}]
    r = client.post("/v1/agent/step", json={**BODY, "tools": tools})
    assert r.status_code == 200
    assert r.json()["tool_calls"] == [{"id": "c1", "name": "get_trend", "arguments": "{\"name\":\"HbA1c\"}"}]
    assert seen["auth"] == "Bearer t" and seen["url"].endswith("/chat/completions") and seen["tools"] == tools


def test_provider_error_is_unavailable(monkeypatch):
    monkeypatch.setattr(config, "HF_TOKEN", "t")
    monkeypatch.setattr(httpx, "post", lambda *a, **k: httpx.Response(429, request=httpx.Request("POST", "http://x")))
    r = client.post("/v1/agent/step", json=BODY)
    assert r.status_code == 503 and r.json()["reason"] == "model_status_429"
