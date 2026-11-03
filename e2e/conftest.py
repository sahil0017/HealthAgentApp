"""Shared client and fixtures for the full-stack end-to-end suite.

The suite talks only to the running docker compose stack: the Go API on API_URL and the Python AI
service on AI_URL. Every test file works in its own profile and cleans it up.
"""
import hashlib
import json
import os
import time
import uuid
from datetime import date, timedelta

import pytest
import requests

API_URL = os.environ.get("API_URL", "http://localhost:8080").rstrip("/")
AI_URL = os.environ.get("AI_URL", "http://localhost:8000").rstrip("/")
DEV_TOKEN = os.environ.get("CORTEX_DEV_TOKEN", "")
PURPOSES = ["medical_record_storage", "document_processing", "ai_processing", "health_data", "research"]


class Api:
    def __init__(self):
        self.http = requests.Session()
        self.token = ""

    def call(self, method, path, body=None, token=None, **kw):
        headers = kw.pop("headers", {})
        bearer = self.token if token is None else token
        if bearer:
            headers["Authorization"] = "Bearer " + bearer
        if body is not None:
            kw["json"] = body
        return self.http.request(method, API_URL + path, headers=headers, timeout=60, **kw)

    def get(self, path, **kw):
        return self.call("GET", "/api/v1" + path, **kw)

    def post(self, path, body=None, **kw):
        return self.call("POST", "/api/v1" + path, body if body is not None else {}, **kw)

    def put(self, path, body=None, **kw):
        return self.call("PUT", "/api/v1" + path, body, **kw)

    def patch(self, path, body=None, **kw):
        return self.call("PATCH", "/api/v1" + path, body, **kw)

    def delete(self, path, body=None, **kw):
        return self.call("DELETE", "/api/v1" + path, body, **kw)

    def ok(self, response, *statuses):
        want = statuses or (200, 201, 202, 204)
        assert response.status_code in want, f"{response.request.method} {response.request.url}: {response.status_code} {response.text[:400]}"
        if response.status_code == 204 or not response.content:
            return None
        body = response.json()
        return body.get("data", body) if isinstance(body, dict) else body

    def upload_pdf(self, profile, text="Hemoglobin A1c 5.4 % Reference 4.0-5.6 %", name="report.pdf"):
        content = make_pdf(text)
        response = self.call("POST", f"/api/v1/profiles/{profile}/documents", files={"file": (name, content, "application/pdf")})
        return response, content


def make_pdf(text: str) -> bytes:
    stream = f"BT /F1 12 Tf 72 720 Td ({text}) Tj ET"
    objects = [
        "<< /Type /Catalog /Pages 2 0 R >>",
        "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
        "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
        f"<< /Length {len(stream)} >>\nstream\n{stream}\nendstream",
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


def sha256(content: bytes) -> str:
    return hashlib.sha256(content).hexdigest()


def ago(days: int) -> str:
    return (date.today() - timedelta(days=days)).isoformat()


def ahead(days: int) -> str:
    return (date.today() + timedelta(days=days)).isoformat()


def wait_until(label, check, seconds=120, every=2):
    deadline = time.time() + seconds
    last = None
    while time.time() < deadline:
        try:
            last = check()
            if last:
                return last
        except Exception as err:  # the stack may still be settling
            last = err
        time.sleep(every)
    raise AssertionError(f"{label}: timed out after {seconds}s (last: {last})")


@pytest.fixture(scope="session")
def api():
    client = Api()
    wait_until("api ready", lambda: requests.get(API_URL + "/readyz", timeout=5).status_code == 200, 180)
    assert DEV_TOKEN, "CORTEX_DEV_TOKEN must be set"
    response = client.call("POST", "/api/v1/auth/session", token=DEV_TOKEN)
    assert response.status_code == 201, response.text
    client.token = response.json()["data"]["access_token"]
    return client


# Logical biomarker -> (term sent to the research agent, panel). The catalog starts empty; every entry below is
# researched from LOINC and approved by the reviewer, exactly as in production, and its generated code is recorded here.
WANTED = {
    "a1c": ("Hemoglobin A1c", "glucose"),
    "glucose": ("Glucose", "glucose"),
    "ldl": ("Cholesterol in LDL", "lipid"),
    "creatinine": ("Creatinine", "kidney"),
    "tsh": ("Thyrotropin", "hormone"),
    "ferritin": ("Ferritin", "vitamin"),
}
CODES: dict = {}


def _entries(api, status):
    data = api.ok(api.get(f"/knowledge/entries?status={status}&kind=biomarker&limit=200"), 200)
    return data["entries"] if isinstance(data, dict) else data


@pytest.fixture(scope="session", autouse=True)
def catalog(api):
    probe = api.ok(api.post("/profiles", {"display_name": "catalog"}), 201)["id"]
    try:
        for name, (term, category) in WANTED.items():
            def known():
                catalog_rows = api.ok(api.get(f"/profiles/{probe}/biomarkers"), 200)
                for row in catalog_rows:
                    if row["display_name"].lower() == term.lower():
                        return row["code"]
                return None

            if not known():
                api.ok(api.post("/knowledge/research", {"kind": "biomarker", "term": term}), 202)

                def proposal():
                    return next((e for e in _entries(api, "pending") if str(e["payload"].get("display_name", "")).lower() == term.lower()), None)

                entry = wait_until(f"research proposal for {term}", proposal, 180, every=5)
                api.ok(api.post(f"/knowledge/entries/{entry['id']}/approve", {"category": category, "note": "e2e reviewer"}), 200)
            CODES[name] = wait_until(f"catalog entry for {term}", known, 30, every=1)
    finally:
        api.delete(f"/profiles/{probe}", {"confirm": "DELETE"})
    return CODES


@pytest.fixture()
def profile(api):
    """A fresh profile with every consent granted, deleted afterwards."""
    created = api.ok(api.post("/profiles", {"display_name": "E2E " + uuid.uuid4().hex[:6]}), 201)
    pid = created["id"]
    for purpose in PURPOSES:
        api.ok(api.put(f"/profiles/{pid}/consents/{purpose}", {"granted": True}), 200)
    yield pid
    api.delete(f"/profiles/{pid}", {"confirm": "DELETE"})


@pytest.fixture()
def bare_profile(api):
    """A fresh profile with no consents at all."""
    pid = api.ok(api.post("/profiles", {"display_name": "Bare " + uuid.uuid4().hex[:6]}), 201)["id"]
    yield pid
    api.delete(f"/profiles/{pid}", {"confirm": "DELETE"})


def add_verified(api, profile, code, value, unit, on, reference_range=""):
    """Create a manual observation and verify it, returning its id."""
    body = {"biomarker_code": code, "value_numeric": value, "unit": unit, "observed_at": on}
    if reference_range:
        body["reference_range"] = reference_range
    obs = api.ok(api.post(f"/profiles/{profile}/observations", body), 201)
    api.ok(api.post(f"/observations/{obs['id']}/verify"), 200)
    return obs["id"]
