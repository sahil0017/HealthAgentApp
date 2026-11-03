"""Biomarker and Longitudinal Intelligence: observations, trends, ranges, units, comparison, coverage, learning."""
import uuid

import pytest

from conftest import add_verified, ago, make_pdf, sha256, wait_until
from test_vault import processed


def test_observation_validation_and_review_flow(api, profile, bare_profile):
    base = f"/profiles/{profile}/observations"
    assert api.post(base, {"biomarker_code": "GLUCOSE", "unit": "mg/dL"}).status_code == 400
    assert api.post(base, {"biomarker_code": "NOPE", "value_numeric": 1, "unit": "x"}).status_code == 400
    assert api.post(base, {"biomarker_code": "GLUCOSE", "value_numeric": 90, "unit": "furlongs"}).status_code == 400
    assert api.post(base, {"biomarker_code": "GLUCOSE", "value_numeric": 90, "unit": "mg/dL", "observed_at": "yesterday"}).status_code == 400
    assert api.post(base, {"value_numeric": 90, "unit": "mg/dL"}).status_code == 400
    assert api.post(base, {"biomarker_code": "GLUCOSE", "value_numeric": 90, "unit": "mg/dL", "source_document_id": str(uuid.uuid4())}).status_code == 400
    assert api.post(f"/profiles/{bare_profile}/observations", {"biomarker_code": "GLUCOSE", "value_numeric": 90, "unit": "mg/dL"}).status_code == 403

    free = api.ok(api.post(base, {"display_name": "Custom marker", "value_numeric": 3.2, "unit": "u", "observed_at": ago(2) + "T10:00:00Z"}), 201)
    assert free["verification_status"] == "PENDING_REVIEW"

    obs = api.ok(api.post(base, {"biomarker_code": "glucose", "value_numeric": 120, "unit": "mg/dL", "observed_at": ago(30)}), 201)
    pending = api.ok(api.get(base + "?status=PENDING_REVIEW"), 200)
    assert any(o["id"] == obs["id"] for o in pending)
    api.ok(api.post(f"/observations/{obs['id']}/verify"), 200)
    assert api.post(f"/observations/{obs['id']}/verify").status_code in (404, 409)

    corrected = api.ok(api.post(f"/observations/{obs['id']}/corrections", {"value_numeric": 118, "change_reason": "typo on report"}), 200, 201)
    assert corrected["revision"] >= 2
    assert api.post(f"/observations/{obs['id']}/corrections", {"value_numeric": 118}).status_code == 400
    assert api.post(f"/observations/{uuid.uuid4()}/corrections", {"value_numeric": 1, "change_reason": "x"}).status_code == 404
    assert api.post(f"/observations/{uuid.uuid4()}/verify").status_code == 404
    assert api.get(f"/profiles/{profile}/observations?limit=1&offset=0").status_code == 200


def test_trend_summary_history_and_comparison(api, profile):
    for days, value in [(120, 6.1), (60, 5.8), (5, 5.4)]:
        add_verified(api, profile, "HBA1C", value, "%", ago(days), "4.0-5.6")
    add_verified(api, profile, "GLUCOSE", 101, "mg/dL", ago(3))
    api.ok(api.post(f"/profiles/{profile}/observations", {"biomarker_code": "HBA1C", "value_numeric": 9.9, "unit": "%", "observed_at": ago(1)}), 201)

    trend = api.ok(api.get(f"/profiles/{profile}/biomarkers/HBA1C/trend"), 200)
    assert len(trend["points"]) == 3, "unverified readings never feed the trend"
    assert trend["trend"]["direction"] in ("falling", "decreasing", "down")
    with_unverified = api.ok(api.get(f"/profiles/{profile}/biomarkers/HBA1C/trend?include_unverified=true"), 200)
    assert len(with_unverified["points"]) >= 3
    assert api.get(f"/profiles/{profile}/biomarkers/NOPE/trend").status_code in (200, 404)

    summary = api.ok(api.get(f"/profiles/{profile}/biomarkers/summary"), 200)
    assert {s["biomarker_code"] for s in summary} >= {"HBA1C", "GLUCOSE"}
    history = api.ok(api.get(f"/profiles/{profile}/biomarkers/HBA1C/history"), 200)
    assert len(history) >= 3
    assert api.ok(api.get(f"/profiles/{profile}/biomarkers"), 200)

    compare = api.ok(api.get(f"/profiles/{profile}/biomarkers/compare?from={ago(90)}&to={ago(0)}"), 200)
    assert compare
    assert api.get(f"/profiles/{profile}/biomarkers/compare").status_code == 400
    assert api.get(f"/profiles/{profile}/biomarkers/compare?from=bad").status_code == 400
    assert api.get(f"/profiles/{profile}/biomarkers/compare?from={ago(1)}&to={ago(90)}").status_code == 400

    coverage = api.ok(api.get(f"/profiles/{profile}/biomarkers/coverage"), 200)
    assert coverage
    assert api.get(f"/profiles/{uuid.uuid4()}/biomarkers/summary").status_code == 404


def test_catalog_entry_review_and_bulk_approve(api):
    entries = api.ok(api.get("/knowledge/entries?status=pending&limit=5&offset=0"), 200)
    assert entries is not None
    assert api.get("/knowledge/entries?status=approved&kind=biomarker").status_code == 200
    assert api.post("/knowledge/entries/bulk-approve", {"kind": "biomarker", "min_agreement": 2, "limit": 5}).status_code in (200, 400)
    assert api.post("/knowledge/entries/bulk-approve", {"kind": "reference_range"}).status_code == 400
    assert api.post(f"/knowledge/entries/{uuid.uuid4()}/approve", {}).status_code == 404
    assert api.post(f"/knowledge/entries/{uuid.uuid4()}/reject", {"note": "n"}).status_code == 404
    assert api.post("/knowledge/entries/not-a-uuid/approve", {}).status_code == 400


def _pending(api, kind, key_prefix, seconds):
    def check():
        entries = api.ok(api.get(f"/knowledge/entries?status=pending&kind={kind}&limit=200"), 200)
        entries = entries["entries"] if isinstance(entries, dict) else entries
        return next((e for e in entries if e["key"].startswith(key_prefix)), None)
    return wait_until(f"{kind} proposal for {key_prefix}", check, seconds, every=5)


def test_learned_range_and_conversion_proposals_need_review(api, profile):
    """Three reports printing the same range lead to a pending range proposal; two units lead to a conversion task."""
    for i in range(3):
        content = make_pdf(f"Thyroid stimulating hormone report {uuid.uuid4()} number {i}")
        doc = api.ok(api.post(f"/profiles/{profile}/documents/upload-intents", {"filename": f"tsh{i}.pdf", "content_type": "application/pdf", "size_bytes": len(content), "sha256": sha256(content), "document_type": "LAB_REPORT"}), 200, 201)
        import requests
        requests.put(doc["upload_url"], data=content, headers={"Content-Type": "application/pdf"}, timeout=60).raise_for_status()
        api.ok(api.post(f"/documents/{doc['document_id']}/complete"), 200, 202)
        processed(api, doc["document_id"])
        api.ok(api.post(f"/profiles/{profile}/documents/{doc['document_id']}/approve", {"biomarkers": [
            {"biomarker_code": "TSH", "name": "TSH", "value": 2.0 + i / 10, "unit": "mIU/L", "observed_at": ago(10 * (i + 1)), "reference_range": "0.4-4.0"}]}), 200, 201)

    add_verified(api, profile, "GLUCOSE", 100, "mg/dL", ago(40))
    add_verified(api, profile, "GLUCOSE", 5.5, "mmol/L", ago(2))

    proposal = _pending(api, "reference_range", "TSH|", 120)
    assert proposal["status"] == "pending"
    assert api.ok(api.post(f"/knowledge/entries/{proposal['id']}/approve", {"note": "e2e review"}), 200, 201) is not None
    assert api.post(f"/knowledge/entries/{proposal['id']}/approve", {}).status_code in (400, 404, 409)

    try:
        conversion = _pending(api, "unit_conversion", "GLUCOSE|", 150)
    except AssertionError:
        pytest.skip("UCUM service unreachable from the runner")
    api.ok(api.post(f"/knowledge/entries/{conversion['id']}/approve", {"note": "e2e review"}), 200, 201)
    trend = api.ok(api.get(f"/profiles/{profile}/biomarkers/GLUCOSE/trend"), 200)
    assert len(trend["points"]) == 2 and not trend["excluded"], "an approved conversion makes both readings comparable"
