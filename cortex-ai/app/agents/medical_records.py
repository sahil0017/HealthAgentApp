from typing import Any


def run(state: dict[str, Any]) -> dict[str, Any]:
    observations = state["observations"]
    if not observations:
        return {
            "findings": [
                "There are no user-approved lab observations in this profile yet. "
                "Upload a report and review its extracted values to add context."
            ]
        }
    lines = [
        f"{o['name']}: {o['value']:g} {o['unit']}" + (f" on {o['observed_at'][:10]}" if o.get("observed_at") else "")
        for o in observations
    ]
    findings = ["Your reviewed records include: " + "; ".join(lines) + "."]
    sources = [{"type": "user_record", "description": "User-approved observations"}]
    chunks = state.get("record_chunks") or []
    if chunks:
        findings.append("Records most relevant to your question: " + "; ".join(c["content"] for c in chunks) + ".")
        sources += [
            {"type": "user_record", "description": c["content"], "observation_id": c["observation_id"]} for c in chunks
        ]
    return {"findings": findings, "sources": sources}
