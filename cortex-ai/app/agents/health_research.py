from typing import Any

from app.rag.retriever import Retriever

NOT_CONNECTED = "No approved research sources are available yet."
NO_MATCH = "No verified research sources matched this question."
NOT_CLINICAL = "Research excerpts are general published findings, not advice about this person's care."


def _from_approved(chunks: list[dict[str, Any]]) -> dict[str, Any]:
    findings, sources = [], []
    for number, c in enumerate(chunks, start=1):
        findings.append(f"[R{number}] Research ({c.get('identifier') or 'published source'}): {c['title']}. {c['content']}")
        sources.append(
            {
                "type": "research_source",
                "source_id": c["source_id"],
                "description": c["title"],
                "source_type": "published_literature",
                "verification_status": "approved",
                "identifier": c.get("identifier", ""),
                "url": c.get("url", ""),
            }
        )
    return {"findings": findings, "sources": sources, "limitations": [NOT_CLINICAL]}


def run(state: dict[str, Any], retriever: Retriever | None = None) -> dict[str, Any]:
    # Approved, embedded literature supplied by the API is the source of truth; a file corpus is only a dev override.
    if state.get("research_chunks"):
        return _from_approved(state["research_chunks"])
    if retriever is None:
        return {"limitations": [NOT_CONNECTED]}
    citations = retriever.retrieve(state["question"], k=3)
    if not citations:
        return {"limitations": [NO_MATCH]}
    findings = [
        f"[R{number}] Research ({c.source_type}, {c.verification_status}): {c.title}. {c.excerpt}"
        for number, c in enumerate(citations, start=1)
    ]
    sources = [
        {
            "type": "research_source",
            "source_id": c.source_id,
            "description": c.title,
            "source_type": c.source_type,
            "verification_status": c.verification_status,
            "identifier": c.identifier or "",
        }
        for c in citations
    ]
    return {"findings": findings, "sources": sources, "limitations": [NOT_CLINICAL]}
