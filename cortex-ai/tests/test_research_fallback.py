import asyncio

import pytest

from app.inference import copilot_agent
from app.research import discover


def test_general_answer_uses_copilot_and_rejects_empty(monkeypatch):
    async def submit(system, prompt, model_cls, tool_name):
        assert "no sources" in system
        assert "What is vitamin D?" in prompt
        assert tool_name == "submit_answer"
        return {"answer": "General information."}

    monkeypatch.setattr(copilot_agent, "_submit", submit)
    assert asyncio.run(copilot_agent.research_answer("What is vitamin D?")) == "General information."

    async def empty(*args):
        return {"answer": ""}

    monkeypatch.setattr(copilot_agent, "_submit", empty)
    with pytest.raises(copilot_agent.CopilotUnavailable):
        asyncio.run(copilot_agent.research_answer("What is vitamin D?"))


def test_source_failure_is_not_reported_as_empty(monkeypatch):
    def fail(*args, **kwargs):
        raise OSError("unavailable")

    monkeypatch.setattr(discover.research_agent, "research_literature", fail)
    monkeypatch.setattr(discover.sources, "search_medlineplus_topics", fail)
    with pytest.raises(RuntimeError, match="could not be searched"):
        discover._gather(None, ["vitamin D"])


def test_literature_outage_is_not_reported_as_no_results(monkeypatch):
    def fail(*args, **kwargs):
        raise OSError("unavailable")

    monkeypatch.setattr(discover.research_agent, "search_pubmed", fail)
    monkeypatch.setattr(discover.research_agent, "search_europepmc", lambda *args: [])
    monkeypatch.setattr(discover.research_agent, "search_trials", lambda *args: [])
    with pytest.raises(RuntimeError, match="could not be searched"):
        discover.research_agent.research_literature(None, "vitamin D")
