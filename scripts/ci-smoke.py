#!/usr/bin/env python3
"""End-to-end smoke test against a running docker compose stack (stdlib only).

Exercises the real path a user takes: sign in, create a profile, grant consent, upload a PDF, let the
worker extract it, then read the result back. Exits non-zero with a clear message on the first failure.

Usage: CORTEX_DEV_TOKEN=... scripts/ci-smoke.py [base_url]
"""
import json
import os
import sys
import time
import urllib.error
import urllib.request
import uuid

BASE = (sys.argv[1] if len(sys.argv) > 1 else "http://localhost:8080").rstrip("/")
DEV_TOKEN = os.environ.get("CORTEX_DEV_TOKEN", "")
TOKEN = ""


def fail(message: str) -> None:
    print(f"FAIL: {message}", file=sys.stderr)
    sys.exit(1)


def call(method: str, path: str, body=None, token: str | None = None, raw: bytes | None = None, content_type: str | None = None):
    headers = {}
    data = raw
    if body is not None:
        data = json.dumps(body).encode()
        headers["Content-Type"] = "application/json"
    if content_type:
        headers["Content-Type"] = content_type
    bearer = token if token is not None else TOKEN
    if bearer:
        headers["Authorization"] = f"Bearer {bearer}"
    req = urllib.request.Request(BASE + path, data=data, method=method, headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=20) as resp:
            payload = resp.read()
            return resp.status, json.loads(payload) if payload else None
    except urllib.error.HTTPError as err:
        payload = err.read()
        try:
            return err.code, json.loads(payload)
        except ValueError:
            return err.code, payload.decode(errors="replace")


def data(response):
    return response["data"] if isinstance(response, dict) and "data" in response else response


def expect(label: str, got, want):
    if got != want:
        fail(f"{label}: expected {want}, got {got}")
    print(f"ok   {label}")


def wait_for(label: str, check, seconds: int):
    deadline = time.time() + seconds
    last = None
    while time.time() < deadline:
        try:
            last = check()
            if last:
                print(f"ok   {label}")
                return last
        except Exception as err:  # the stack may still be starting
            last = err
        time.sleep(2)
    fail(f"{label}: timed out after {seconds}s (last: {last})")


def sample_pdf() -> bytes:
    """A one-page PDF with selectable text, built by hand so the test needs no libraries."""
    text = "BT /F1 12 Tf 72 720 Td (Hemoglobin A1c 5.4 % Reference 4.0-5.6 %) Tj ET"
    objects = [
        "<< /Type /Catalog /Pages 2 0 R >>",
        "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
        "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
        f"<< /Length {len(text)} >>\nstream\n{text}\nendstream",
        "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
    ]
    out = bytearray(b"%PDF-1.4\n")
    offsets = []
    for i, body in enumerate(objects, 1):
        offsets.append(len(out))
        out += f"{i} 0 obj\n{body}\nendobj\n".encode()
    xref = len(out)
    out += f"xref\n0 {len(objects) + 1}\n0000000000 65535 f \n".encode()
    for off in offsets:
        out += f"{off:010d} 00000 n \n".encode()
    out += f"trailer\n<< /Size {len(objects) + 1} /Root 1 0 R >>\nstartxref\n{xref}\n%%EOF\n".encode()
    return bytes(out)


def multipart(filename: str, content: bytes, content_type: str):
    boundary = uuid.uuid4().hex
    body = (
        f"--{boundary}\r\nContent-Disposition: form-data; name=\"file\"; filename=\"{filename}\"\r\n"
        f"Content-Type: {content_type}\r\n\r\n"
    ).encode() + content + f"\r\n--{boundary}--\r\n".encode()
    return body, f"multipart/form-data; boundary={boundary}"


def main():
    global TOKEN
    if not DEV_TOKEN:
        fail("CORTEX_DEV_TOKEN is not set")

    wait_for("api is ready (database and object storage)", lambda: call("GET", "/readyz")[0] == 200, 120)
    expect("unauthenticated profile list is rejected", call("GET", "/api/v1/profiles")[0], 401)

    status, body = call("POST", "/api/v1/auth/session", token=DEV_TOKEN)
    expect("session is created with the development credential", status, 201)
    TOKEN = data(body)["access_token"]

    status, body = call("POST", "/api/v1/profiles", {"display_name": "CI Smoke"})
    expect("profile is created", status, 201)
    profile = data(body)["id"]

    for purpose in ("medical_record_storage", "document_processing"):
        status, _ = call("PUT", f"/api/v1/profiles/{profile}/consents/{purpose}", {"granted": True})
        expect(f"consent {purpose} is granted", status, 200)

    content, ctype = multipart("smoke.pdf", sample_pdf(), "application/pdf")
    status, body = call("POST", f"/api/v1/profiles/{profile}/documents", raw=content, content_type=ctype)
    expect("pdf upload is accepted", status, 202)
    document = data(body)["id"]

    def processed():
        _, current = call("GET", f"/api/v1/documents/{document}")
        state = data(current)["status"]
        if state == "failed":
            fail(f"worker failed to process the document: {current}")
        return state in ("review_required", "approved")

    wait_for("worker extracted the document", processed, 120)

    status, body = call("GET", f"/api/v1/documents/{document}/extraction/pages")
    expect("extracted pages are readable", status, 200)
    if "Hemoglobin" not in json.dumps(body):
        fail(f"extracted text is missing the PDF content: {body}")
    print("ok   extracted text matches the PDF")

    # Module 07: cited Q&A through the api -> cortex-ai, with history, feedback and emergency triage.
    status, _ = call("PUT", f"/api/v1/profiles/{profile}/consents/ai_processing", {"granted": True})
    expect("consent ai_processing is granted", status, 200)
    status, body = call("POST", f"/api/v1/profiles/{profile}/questions", {"question": "How is my health?"})
    expect("a health question is answered by cortex-ai", status, 200)
    answer = data(body)
    for key in ("id", "answer", "confidence", "uncertainty", "suggested_questions", "limitations"):
        if key not in answer:
            fail(f"answer is missing {key}: {answer}")
    expect("an answer without approved records admits it has little to go on", answer["confidence"], "insufficient")
    question_id = answer["id"]

    status, body = call("POST", f"/api/v1/profiles/{profile}/questions", {"question": "I have crushing chest pain"})
    expect("an emergency question is answered", status, 200)
    expect("an emergency question is flagged urgent", data(body)["urgent"], True)

    status, body = call("GET", f"/api/v1/profiles/{profile}/questions")
    expect("question history lists both questions", (status, len(data(body)["questions"])), (200, 2))
    status, body = call("PUT", f"/api/v1/profiles/{profile}/questions/{question_id}/feedback", {"rating": "helpful"})
    expect("feedback is saved", (status, data(body)["feedback"]), (200, "helpful"))
    expect("a question can be deleted", call("DELETE", f"/api/v1/profiles/{profile}/questions/{question_id}")[0], 204)
    expect("a deleted question is gone", call("GET", f"/api/v1/profiles/{profile}/questions/{question_id}")[0], 404)

    expect("an unknown session cannot read the profile", call("GET", f"/api/v1/profiles/{profile}", token="not-a-session")[0], 401)
    expect("profile export works", call("GET", f"/api/v1/profiles/{profile}/export")[0], 200)
    expect("profile is deleted with its data", call("DELETE", f"/api/v1/profiles/{profile}", {"confirm": "DELETE"})[0] in (200, 204), True)
    print("smoke test passed")


if __name__ == "__main__":
    main()
