"""Identity (auth, profiles) and Personal Data Governance (consent, access history, export, deletion)."""
import uuid

import testidp
from conftest import CODES as C, API_URL, add_verified, ago, sign_in


def test_health_and_auth_boundaries(api):
    import requests
    assert requests.get(API_URL + "/healthz", timeout=10).status_code == 200
    assert requests.get(API_URL + "/readyz", timeout=10).status_code == 200
    assert api.get("/profiles", token="not-a-session").status_code == 401
    assert api.call("GET", "/api/v1/profiles", token="").status_code == 401
    bad = {"provider": "oidc", "id_token": "not.a.jwt", "nonce": "n"}
    assert api.call("POST", "/api/v1/auth/session", bad, token="").status_code == 401
    forged = {"provider": "oidc", "id_token": testidp.id_token("x", "n", aud="someone-else"), "nonce": "n"}
    assert api.call("POST", "/api/v1/auth/session", forged, token="").status_code == 401
    no_nonce = {"provider": "oidc", "id_token": testidp.id_token("x", "n"), "nonce": "other"}
    assert api.call("POST", "/api/v1/auth/session", no_nonce, token="").status_code == 401
    me = api.ok(api.get("/users/me"), 200)
    assert me


def test_logout_revokes_only_that_session(api):
    second = sign_in("e2e-admin")
    assert api.get("/profiles", token=second).status_code == 200
    assert api.post("/auth/logout", token=second).status_code == 200
    assert api.get("/profiles", token=second).status_code == 401
    assert api.get("/profiles").status_code == 200


def test_profile_crud_and_validation(api):
    assert api.post("/profiles", {"display_name": ""}).status_code == 400
    assert api.post("/profiles", {"unknown": 1}).status_code == 400
    pid = api.ok(api.post("/profiles", {"display_name": "Crud"}), 201)["id"]
    try:
        assert any(p["id"] == pid for p in api.ok(api.get("/profiles"), 200))
        assert api.ok(api.get(f"/profiles/{pid}"), 200)["display_name"] == "Crud"
        updated = api.ok(api.patch(f"/profiles/{pid}", {
            "display_name": "Crud 2", "timezone": "Asia/Kolkata", "measurement_system": "metric",
            "date_of_birth": "1990-05-01", "sex": "female", "blood_group": "O+", "height_cm": 168.5}), 200)
        assert updated["display_name"] == "Crud 2"
        assert updated["sex"] == "female"
        assert api.patch(f"/profiles/{pid}", {"date_of_birth": "not-a-date"}).status_code == 400
        assert api.patch(f"/profiles/{pid}", {"height_cm": -4}).status_code == 400
        assert api.patch(f"/profiles/{pid}", {"timezone": "Mars/Base"}).status_code == 400
        assert api.patch(f"/profiles/{pid}", {}).status_code in (200, 400)
        assert api.get(f"/profiles/{uuid.uuid4()}").status_code == 404
        assert api.get("/profiles/not-a-uuid").status_code in (400, 404)
    finally:
        assert api.delete(f"/profiles/{pid}", {"confirm": "DELETE"}).status_code in (200, 204)
    assert api.get(f"/profiles/{pid}").status_code == 404


def test_delete_requires_explicit_confirmation(api):
    pid = api.ok(api.post("/profiles", {"display_name": "Del"}), 201)["id"]
    assert api.delete(f"/profiles/{pid}", {"confirm": "no"}).status_code == 400
    assert api.delete(f"/profiles/{pid}").status_code == 400
    assert api.get(f"/profiles/{pid}").status_code == 200
    assert api.delete(f"/profiles/{pid}", {"confirm": "DELETE"}).status_code in (200, 204)
    assert api.delete(f"/profiles/{pid}", {"confirm": "DELETE"}).status_code == 404


def test_consent_lifecycle(api, bare_profile):
    base = f"/profiles/{bare_profile}"
    assert api.ok(api.get(base + "/consents"), 200) is not None
    # Writing health data needs consent first.
    assert api.post(base + "/history", {"kind": "condition", "title": "Asthma"}).status_code == 403
    granted = api.ok(api.put(base + "/consents/medical_record_storage", {"granted": True, "policy_version": "2026-01"}), 200)
    assert granted
    assert api.post(base + "/history", {"kind": "condition", "title": "Asthma"}).status_code == 201
    created = api.ok(api.post(base + "/consents", {"purpose": "research", "policy_version": "2026-01"}), 200, 201)
    assert created
    consent_id = created["id"]
    assert api.ok(api.get(f"/consents/{consent_id}"), 200)
    assert api.delete(f"/consents/{consent_id}").status_code in (200, 204)
    assert api.put(base + "/consents/not_a_purpose", {"granted": True}).status_code == 400
    assert api.put(base + "/consents/research", {}).status_code == 400
    assert api.post(base + "/consents", {"purpose": "", "policy_version": ""}).status_code == 400
    assert api.get(f"/consents/{uuid.uuid4()}").status_code == 404
    api.ok(api.put(base + "/consents/medical_record_storage", {"granted": False}), 200)
    assert api.post(base + "/history", {"kind": "condition", "title": "Again"}).status_code == 403


def test_access_history_and_export(api, profile):
    add_verified(api, profile, C["glucose"], 95, "mg/dL", ago(5))
    history = api.ok(api.get(f"/profiles/{profile}/access-history?limit=5&offset=0"), 200)
    assert len(history) >= 1
    assert api.get(f"/profiles/{profile}/access-history?limit=0").status_code == 400
    assert api.get(f"/profiles/{profile}/access-history?offset=-1").status_code == 400
    export = api.ok(api.get(f"/profiles/{profile}/export"), 200)
    assert "observations" in export and "consents" in export
    assert "object_key" not in str(export)
    assert api.get(f"/profiles/{uuid.uuid4()}/export").status_code == 404
