"""Live discovery of sources for a followed health question: literature and plain-language pages from
allowlisted public sources, chosen and explained by the model. URLs only ever come from those sources."""
from __future__ import annotations

import asyncio
import logging

from app.inference import copilot_agent
from app.research import agent as research_agent, sources

logger = logging.getLogger(__name__)

def _gather(fetcher: sources.Fetcher, queries: list[str]) -> list[dict]:
    found: dict[str, dict] = {}
    failures = 0
    for q in queries:
        try:
            for it in research_agent.research_literature(fetcher, q, limit=3):
                p = it["payload"]
                if p["url"] and p["url"] not in found:
                    kind = "trial" if str(p["identifier"]).startswith("NCT") else "paper"
                    found[p["url"]] = {"title": p["title"], "url": p["url"], "publisher": p["journal"] or p["source"], "year": p["year"], "kind": kind, "text": p["abstract"] or p["title"]}
        except Exception as exc:
            failures += 1
            logger.warning("Literature search failed: %s", type(exc).__name__)
            continue
    try:
        for t in sources.search_medlineplus_topics(fetcher, queries[0], 3):
            found.setdefault(t.url, {"title": t.title, "url": t.url, "publisher": "MedlinePlus", "year": None, "kind": "guide", "text": t.summary or t.title})
    except Exception as exc:
        failures += 1
        logger.warning("Health guide search failed: %s", type(exc).__name__)
    if not found and failures:
        raise RuntimeError("Research sources could not be searched")
    return list(found.values())


async def discover(fetcher: sources.Fetcher, question: str, limit: int = 8) -> list[dict]:
    queries = [question]
    try:
        queries = (await copilot_agent.plan_queries(question)) or queries
    except copilot_agent.CopilotUnavailable:
        pass
    candidates = await asyncio.to_thread(_gather, fetcher, queries)
    if not candidates:
        return []
    verdicts = {}
    try:
        for v in await copilot_agent.judge_sources(question, candidates):
            if isinstance(v.get("index"), int):
                verdicts[v["index"]] = v
    except copilot_agent.CopilotUnavailable:
        pass
    out = []
    for i, c in enumerate(candidates):
        v = verdicts.get(i)
        if verdicts and (v is None or not v.get("relevant")):
            continue
        summary = (v or {}).get("summary") or c["text"][:300]
        out.append({k: c[k] for k in ("title", "url", "publisher", "year", "kind")} | {"summary": summary.strip()})
    return out[:limit]
