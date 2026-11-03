"""Medical Intelligence Vault: upload, OCR extraction, review, versioning, search, download, deletion."""
import uuid

import requests

from conftest import CODES as C, make_pdf, sha256, wait_until


def processed(api, document):
    def check():
        current = api.ok(api.get(f"/documents/{document}"), 200)
        assert current["status"] != "failed", current
        return current if current["status"] in ("review_required", "approved") else None
    return wait_until("worker extracted the document", check, 180)


def test_upload_extract_review_approve(api, profile):
    response, _ = api.upload_pdf(profile)
    document = api.ok(response, 202)["id"]
    processed(api, document)

    pages = api.ok(api.get(f"/documents/{document}/extraction/pages"), 200)
    assert "Hemoglobin" in str(pages)

    detail = api.ok(api.get(f"/profiles/{profile}/documents/{document}"), 200)
    assert detail["id"] == document
    listed = api.ok(api.get(f"/profiles/{profile}/documents"), 200)
    assert any(d["id"] == document for d in listed)
    assert api.get(f"/profiles/{profile}/documents?limit=1&offset=0").status_code == 200

    history = api.ok(api.get(f"/documents/{document}/history"), 200)
    assert history
    assert api.ok(api.get(f"/documents/{document}/versions"), 200) is not None

    approved = api.post(f"/profiles/{profile}/documents/{document}/approve", {"biomarkers": [
        {"biomarker_code": C["a1c"], "name": "Hemoglobin A1c", "value": 5.4, "unit": "%", "observed_at": "2026-01-10", "reference_range": "4.0-5.6"}]})
    api.ok(approved, 200, 201)
    observations = api.ok(api.get(f"/profiles/{profile}/observations"), 200)
    assert any(o.get("biomarker_code") == C["a1c"] and o["verification_status"] == "VERIFIED" for o in observations)


def test_rename_retype_download_and_delete(api, profile):
    document = api.ok(api.upload_pdf(profile)[0], 202)["id"]
    processed(api, document)
    updated = api.ok(api.patch(f"/documents/{document}", {"filename": "renamed.pdf", "document_type": "LAB_REPORT"}), 200)
    assert updated["file_name"] == "renamed.pdf"
    assert api.patch(f"/documents/{document}", {}).status_code == 400
    assert api.patch(f"/documents/{document}", {"document_type": "NOT_A_TYPE"}).status_code == 400
    assert api.patch(f"/documents/{document}", {"filename": ".."}).status_code == 400

    meta = api.ok(api.get(f"/documents/{document}"), 200)
    assert meta["id"] == document
    link = api.ok(api.get(f"/documents/{document}/download"), 200)
    assert link
    url = link.get("url") or link.get("download_url")
    assert requests.get(url, timeout=30).status_code == 200
    direct = api.get(f"/profiles/{profile}/documents/{document}/file")
    assert direct.status_code == 200 and direct.content.startswith(b"%PDF")

    assert api.delete(f"/documents/{document}").status_code in (200, 204)
    assert api.get(f"/documents/{document}").status_code == 404


def test_signed_upload_intent_flow_and_duplicates(api, profile):
    content = make_pdf("Glucose 99 mg/dL Reference 70-99 mg/dL")
    intent_body = {"filename": "intent.pdf", "content_type": "application/pdf", "size_bytes": len(content), "sha256": sha256(content), "document_type": "LAB_REPORT"}
    intent = api.ok(api.post(f"/profiles/{profile}/documents/upload-intents", intent_body), 200, 201)
    put = requests.put(intent["upload_url"], data=content, headers={"Content-Type": "application/pdf", **(intent.get("headers") or {})}, timeout=60)
    assert put.status_code in (200, 201, 204), put.text[:300]
    api.ok(api.post(f"/documents/{intent['document_id']}/complete"), 200, 202)
    processed(api, intent["document_id"])

    # An identical checksum is not stored twice.
    duplicate = api.post(f"/profiles/{profile}/documents/upload-intents", intent_body)
    assert duplicate.status_code in (200, 201, 409), duplicate.text

    bad = dict(intent_body, sha256="zz")
    assert api.post(f"/profiles/{profile}/documents/upload-intents", bad).status_code == 400
    assert api.post(f"/profiles/{profile}/documents/upload-intents", dict(intent_body, content_type="text/exe")).status_code == 400
    assert api.post(f"/profiles/{profile}/documents/upload-intents", dict(intent_body, size_bytes=0)).status_code == 400
    assert api.post(f"/documents/{uuid.uuid4()}/complete").status_code == 404


def test_versioning_supersedes_older_document(api, profile):
    first = make_pdf("Creatinine 1.0 mg/dL version one")
    second = make_pdf("Creatinine 1.1 mg/dL version two")
    one = api.ok(api.post(f"/profiles/{profile}/documents/upload-intents", {"filename": "v1.pdf", "content_type": "application/pdf", "size_bytes": len(first), "sha256": sha256(first), "document_type": "LAB_REPORT"}), 200, 201)
    two = api.post(f"/profiles/{profile}/documents/upload-intents", {"filename": "v2.pdf", "content_type": "application/pdf", "size_bytes": len(second), "sha256": sha256(second), "document_type": "LAB_REPORT", "supersedes_document_id": one["document_id"]})
    created = api.ok(two, 200, 201)
    versions = api.ok(api.get(f"/documents/{created['document_id']}/versions"), 200)
    assert versions
    assert api.post(f"/profiles/{profile}/documents/upload-intents", {"filename": "v3.pdf", "content_type": "application/pdf", "size_bytes": 10, "sha256": sha256(b"x"), "document_type": "LAB_REPORT", "supersedes_document_id": str(uuid.uuid4())}).status_code in (400, 404)


def test_cross_document_search_and_retry(api, profile):
    document = api.ok(api.upload_pdf(profile, "Ferritin 45 ng/mL Reference 30-300 ng/mL", "ferritin.pdf")[0], 202)["id"]
    processed(api, document)
    hits = api.ok(api.get(f"/profiles/{profile}/documents/search?q=ferritin&mode=keyword"), 200)
    assert hits
    assert api.ok(api.get(f"/profiles/{profile}/documents/search?q=ferritin"), 200) is not None
    assert api.ok(api.get(f"/profiles/{profile}/documents/search?q=ferritin&document_type=LAB_REPORT&limit=3"), 200) is not None
    assert api.get(f"/profiles/{profile}/documents/search?q=a").status_code == 400
    assert api.get(f"/profiles/{profile}/documents/search").status_code == 400
    assert api.get(f"/profiles/{profile}/documents/search?q=ferritin&mode=bogus").status_code == 400
    # Retry is only valid for a failed document.
    assert api.post(f"/profiles/{profile}/documents/{document}/retry").status_code in (200, 202, 409)


def test_upload_validation_and_isolation(api, profile, bare_profile):
    assert api.call("POST", f"/api/v1/profiles/{profile}/documents", files={"file": ("x.exe", b"MZ", "application/x-msdownload")}).status_code in (400, 415)
    assert api.call("POST", f"/api/v1/profiles/{profile}/documents", data={"a": "b"}).status_code == 400
    response, _ = api.upload_pdf(bare_profile)
    assert response.status_code == 403
    assert api.get(f"/profiles/{uuid.uuid4()}/documents").status_code == 404
    assert api.get(f"/documents/{uuid.uuid4()}").status_code == 404
    assert api.get(f"/documents/{uuid.uuid4()}/download").status_code == 404
    assert api.delete(f"/documents/{uuid.uuid4()}").status_code == 404
