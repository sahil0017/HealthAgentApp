"""Connected Diagnostics: device registration, consent, sync, de-duplication, summaries, export and delete."""
from datetime import datetime, timedelta, timezone

from conftest import ago


def stamp(days, hours=0):
    return (datetime.now(timezone.utc) - timedelta(days=days, hours=hours)).strftime("%Y-%m-%dT%H:%M:%SZ")


def readings(count=12):
    return [{"metric": "resting_heart_rate", "value": 60 + i, "unit": "bpm", "measured_at": stamp(i), "external_id": f"rhr-{i}"} for i in range(count)]


def test_device_requires_consent(api, bare_profile):
    body = {"kind": "phone", "source": "apple_health", "name": "Apple Health", "external_id": "ah"}
    response = api.post(f"/profiles/{bare_profile}/devices", body)
    assert response.status_code == 403
    assert response.json()["error"]["code"] == "CONSENT_REQUIRED"


def test_device_lifecycle_sync_and_summary(api, profile):
    api.ok(api.put(f"/profiles/{profile}/consents/health_data_integration", {"granted": True}), 200)
    metrics = api.ok(api.get("/device-metrics"), 200)
    assert any(m["key"] == "heart_rate" for m in metrics)

    body = {"kind": "phone", "source": "apple_health", "name": "Apple Health", "external_id": "ah"}
    device = api.ok(api.post(f"/profiles/{profile}/devices", body), 201)
    assert api.post(f"/profiles/{profile}/devices", body).status_code == 200  # idempotent

    path = f"/profiles/{profile}/devices/{device['id']}/readings"
    first = api.ok(api.post(path, {"readings": readings() + [{"metric": "heart_rate", "value": 9999, "unit": "bpm", "measured_at": stamp(1)}]}), 200)
    assert (first["accepted"], first["duplicates"], first["rejected"]) == (12, 0, 1)
    second = api.ok(api.post(path, {"readings": readings()}), 200)
    assert (second["accepted"], second["duplicates"]) == (0, 12)

    summaries = api.ok(api.get(f"/profiles/{profile}/device-readings/summary"), 200)["summaries"]
    assert [s["metric"] for s in summaries] == ["resting_heart_rate"]
    daily = api.ok(api.get(f"/profiles/{profile}/device-readings/resting_heart_rate/daily"), 200)
    assert len(daily["days"]) >= 10
    assert api.get(f"/profiles/{profile}/device-readings/heart_rate/lab-comparison").status_code == 400

    export = api.ok(api.get(f"/profiles/{profile}/export"), 200)
    assert len(export["device_readings"]) == 12

    api.ok(api.patch(f"/profiles/{profile}/devices/{device['id']}", {"status": "disconnected"}), 200)
    assert api.post(path, {"readings": readings(1)}).status_code == 409

    api.ok(api.delete(f"/profiles/{profile}/devices/{device['id']}"), 200)
    assert api.ok(api.get(f"/profiles/{profile}/device-readings/summary"), 200)["summaries"] == []
