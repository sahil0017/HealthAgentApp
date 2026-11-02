import json
import urllib.request

from app import config
from app.inference.generate import SYSTEM_PROMPT


def generate_with_ollama(facts: str, question: str) -> str:
    payload = {
        "model": config.OLLAMA_MODEL,
        "stream": False,
        "options": {"temperature": 0, "num_predict": 180},
        "messages": [
            {"role": "system", "content": SYSTEM_PROMPT},
            {"role": "user", "content": f"Question: {question}\n\nRecorded facts:\n{facts}"},
        ],
    }
    request = urllib.request.Request(
        config.OLLAMA_URL.rstrip("/") + "/api/chat",
        data=json.dumps(payload).encode("utf-8"),
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(request, timeout=config.OLLAMA_TIMEOUT_SECONDS) as response:
        body = json.loads(response.read(1 << 20))
    return str(body.get("message", {}).get("content", "")).strip()
