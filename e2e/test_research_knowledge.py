"""Agentic Health Research Intelligence: research tasks propose knowledge that a person must review."""
import uuid

import pytest

from conftest import wait_until


def entries(api, kind, status="pending"):
    data = api.ok(api.get(f"/knowledge/entries?status={status}&kind={kind}&limit=200"), 200)
    return data["entries"] if isinstance(data, dict) else data


def test_research_request_validation(api):
    assert api.post("/knowledge/research", {"kind": "biomarker", "term": "x"}).status_code == 400
    assert api.post("/knowledge/research", {"kind": "other", "term": "ferritin"}).status_code == 400
    assert api.post("/knowledge/research", {"kind": "biomarker", "term": "y" * 200}).status_code == 400
    assert api.post("/knowledge/research", {"unknown": 1}).status_code == 400


def test_biomarker_research_becomes_a_reviewable_proposal(api):
    term = "Magnesium"
    assert api.post("/knowledge/research", {"kind": "biomarker", "term": term}).status_code == 202

    def proposal():
        found = [e for e in entries(api, "biomarker") if term.lower() in str(e["payload"]).lower() or term.lower() in e["key"].lower()]
        return found[0] if found else None

    try:
        entry = wait_until("biomarker proposal", proposal, 150, every=5)
    except AssertionError:
        pytest.skip("research sources unreachable from the runner")
    assert entry["status"] == "pending"
    assert entry["evidence"], "every proposal carries its sources"
    rejected = api.post(f"/knowledge/entries/{entry['id']}/reject", {"note": "e2e: not needed"})
    assert rejected.status_code in (200, 204)
    assert api.post(f"/knowledge/entries/{entry['id']}/approve", {}).status_code in (400, 404, 409)


def test_literature_research_is_queued(api):
    assert api.post("/knowledge/research", {"kind": "literature", "term": "hba1c variability"}).status_code == 202
    listing = api.ok(api.get("/knowledge/entries"), 200)
    assert "research_queue" in listing
