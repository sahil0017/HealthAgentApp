"""Tool-calling chat completions through the Hugging Face Inference Providers router."""

import httpx

from app import config

MAX_MESSAGES = 24
MAX_TOOLS = 12
MAX_TOKENS = 450


class AgentModelUnavailable(Exception):
    """No token is configured, or the provider could not answer."""


def configured() -> bool:
    return bool(config.HF_TOKEN)


def chat(messages: list[dict], tools: list[dict]) -> dict:
    if not configured():
        raise AgentModelUnavailable("model_not_configured")
    payload: dict = {
        "model": config.HF_AGENT_MODEL,
        "messages": messages[-MAX_MESSAGES:],
        "temperature": 0.1,
        "max_tokens": MAX_TOKENS,
    }
    if tools:
        payload["tools"] = tools[:MAX_TOOLS]
        payload["tool_choice"] = "auto"
    try:
        response = httpx.post(
            f"{config.HF_API_URL}/chat/completions",
            json=payload,
            headers={"Authorization": f"Bearer {config.HF_TOKEN}"},
            timeout=config.HF_TIMEOUT_SECONDS,
        )
    except httpx.HTTPError as exc:
        raise AgentModelUnavailable("model_unreachable") from exc
    if response.status_code != 200:
        raise AgentModelUnavailable(f"model_status_{response.status_code}")
    try:
        message = response.json()["choices"][0]["message"]
    except (KeyError, IndexError, ValueError) as exc:
        raise AgentModelUnavailable("model_bad_response") from exc
    calls = []
    for call in message.get("tool_calls") or []:
        fn = call.get("function") or {}
        calls.append({"id": call.get("id", ""), "name": fn.get("name", ""), "arguments": fn.get("arguments", "{}")})
    return {"content": (message.get("content") or "").strip(), "tool_calls": calls, "model": config.HF_AGENT_MODEL}
