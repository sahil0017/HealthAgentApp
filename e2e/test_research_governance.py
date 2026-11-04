"""Module 08 saved investigations and module 12 retention and family sharing, through the public API."""
import uuid

from conftest import sign_in


def test_investigation_lifecycle(api, profile):
    base = f"/profiles/{profile}/investigations"
    assert api.post(base, {"question": "x"}).status_code == 400
    created = api.ok(api.post(base, {"question": "Does low ferritin cause fatigue?"}), 201)
    one = f"{base}/{created['id']}"
    assert created["title"] and created["status"] == "open"

    assert api.ok(api.post(one + "/refresh"), 200)["added"] >= 0
    api.ok(api.post(one + "/items", {"note": "Ask my GP about iron studies"}), 201)
    assert api.post(one + "/items", {}).status_code == 400
    assert api.post(one + "/items", {"source_id": str(uuid.uuid4())}).status_code == 404

    detail = api.ok(api.get(one), 200)
    assert detail["disclaimer"] and any(i["kind"] == "note" for i in detail["items"])
    assert api.ok(api.patch(one, {"status": "archived"}), 200)["status"] == "archived"
    assert api.patch(one, {}).status_code == 400
    assert len(api.ok(api.get(base + "?status=archived"), 200)["investigations"]) == 1
    assert api.get(base + "?status=zzz").status_code == 400

    note = next(i for i in detail["items"] if i["kind"] == "note")
    assert api.delete(f"{one}/items/{note['id']}").status_code == 204
    assert api.delete(f"{one}/items/{note['id']}").status_code == 404
    assert api.delete(one).status_code == 204
    assert api.get(one).status_code == 404


def test_investigations_are_private(api, profile):
    created = api.ok(api.post(f"/profiles/{profile}/investigations", {"question": "vitamin d and bone health"}), 201)
    stranger = sign_in("e2e-stranger-" + uuid.uuid4().hex[:6])
    assert api.get(f"/profiles/{profile}/investigations", token=stranger).status_code == 404
    assert api.get(f"/profiles/{profile}/investigations/{created['id']}", token=stranger).status_code == 404
    export = api.ok(api.get(f"/profiles/{profile}/export"), 200)
    assert len(export["research_investigations"]) == 1


def test_retention_policy_validation_and_apply(api, profile):
    base = f"/profiles/{profile}/retention"
    assert api.ok(api.get(base), 200)["ai_history_days"] is None
    assert api.put(base, {"ai_history_days": 0}).status_code == 400
    assert api.put(base, {"device_readings_days": 5000}).status_code == 400
    saved = api.ok(api.put(base, {"ai_history_days": 30, "device_readings_days": 90}), 200)
    assert saved["ai_history_days"] == 30 and saved["device_readings_days"] == 90
    result = api.ok(api.post(base + "/apply"), 200)
    assert result == {"ai_history_removed": 0, "device_readings_removed": 0}
    assert api.ok(api.get(base), 200)["last_applied_at"]
    assert api.ok(api.put(base, {}), 200)["ai_history_days"] is None
    history = api.ok(api.get(f"/profiles/{profile}/access-history"), 200)
    assert "retention.applied" in str(history)


def test_family_sharing_is_scoped_read_only_and_revocable(api, profile):
    base = f"/profiles/{profile}/shares"
    assert api.post(base, {"label": "Mum", "scopes": ["bogus"]}).status_code == 400
    created = api.ok(api.post(base, {"label": "Mum", "scopes": ["summary", "labs"], "expires_in_days": 7}), 201)
    share, code = created["share"], created["code"]
    assert share["status"] == "pending"

    guardian = sign_in("e2e-guardian-" + uuid.uuid4().hex[:6])
    assert api.post("/shares/accept", {"code": "wrong"}, token=guardian).status_code == 404
    assert api.post("/shares/accept", {"code": code}).status_code == 400
    api.ok(api.post("/shares/accept", {"code": code}, token=guardian), 200)
    assert api.post("/shares/accept", {"code": code}, token=sign_in("e2e-late-" + uuid.uuid4().hex[:6])).status_code == 404

    listed = api.ok(api.get("/shared-with-me", token=guardian), 200)["shares"]
    assert [s["id"] for s in listed] == [share["id"]]
    view = api.ok(api.get(f"/shared-with-me/{share['id']}", token=guardian), 200)
    assert view["read_only"] is True and "labs" in view and "history" not in view
    assert api.get(f"/shared-with-me/{share['id']}").status_code == 404

    assert api.ok(api.get(base), 200)["shares"][0]["status"] == "active"
    assert "share.viewed" in str(api.ok(api.get(f"/profiles/{profile}/access-history"), 200))
    exported = api.ok(api.get(f"/profiles/{profile}/export"), 200)
    assert exported["shares"] and "code_hash" not in exported["shares"][0]

    assert api.delete(f"{base}/{share['id']}", token=guardian).status_code == 404
    assert api.delete(f"{base}/{share['id']}").status_code == 204
    assert api.get(f"/shared-with-me/{share['id']}", token=guardian).status_code == 404


def test_grantee_can_leave_a_share(api, profile):
    created = api.ok(api.post(f"/profiles/{profile}/shares", {"label": "Dr", "scopes": ["follow_ups"]}), 201)
    guardian = sign_in("e2e-leaver-" + uuid.uuid4().hex[:6])
    api.ok(api.post("/shares/accept", {"code": created["code"]}, token=guardian), 200)
    assert api.delete(f"/shared-with-me/{created['share']['id']}", token=guardian).status_code == 204
    assert api.delete(f"/shared-with-me/{created['share']['id']}", token=guardian).status_code == 404
    assert api.ok(api.get(f"/profiles/{profile}/shares"), 200)["shares"][0]["status"] == "revoked"
