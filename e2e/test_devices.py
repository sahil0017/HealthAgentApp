"""Connected Diagnostics: device registration, consent, sync, de-duplication, summaries, export and delete."""
from datetime import datetime, timedelta, timezone

from conftest import ago


def stamp(days, hours=0):
    return (datetime.now(timezone.utc) - timedelta(days=days, hours=hours)).strftime("%Y-%m-%dT%H:%M:%SZ")


def readings(count=12):
    return [{"metric": "resting_heart_rate", "value": 60 + i, "unit": "bpm", "measured_at": stamp(i), "external_id": f"rhr-{i}"} for i in range(count)]


def test_device_requires_consent(api, bare_profile):
    body = {"kind": "phone_health_app", "source": "healthkit", "name": "Apple Health", "external_id": "ah"}
    response = api.post(f"/profiles/{bare_profile}/devices", body)
    assert response.status_code == 403
    assert response.json()["error"]["code"] == "CONSENT_REQUIRED"


def test_device_lifecycle_sync_and_summary(api, profile):
    api.ok(api.put(f"/profiles/{profile}/consents/health_data_integration", {"granted": True}), 200)
    metrics = api.ok(api.get("/device-metrics"), 200)
    assert any(m["key"] == "heart_rate" for m in metrics)

    body = {"kind": "phone_health_app", "source": "healthkit", "name": "Apple Health", "external_id": "ah"}
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

    events = api.ok(api.get(f"/profiles/{profile}/timeline", params={"kind": "device"}), 200)
    assert len(events) == 1 and events[0]["title"] == "Apple Health" and events[0]["detail"] == "12 readings synced"

    export = api.ok(api.get(f"/profiles/{profile}/export"), 200)
    assert len(export["device_readings"]) == 12

    api.ok(api.patch(f"/profiles/{profile}/devices/{device['id']}", {"status": "disconnected"}), 200)
    assert api.post(path, {"readings": readings(1)}).status_code == 409

    api.ok(api.delete(f"/profiles/{profile}/devices/{device['id']}"), 200)
    assert api.ok(api.get(f"/profiles/{profile}/device-readings/summary"), 200)["summaries"] == []


def connect(api, profile, external="ah"):
    api.ok(api.put(f"/profiles/{profile}/consents/health_data_integration", {"granted": True}), 200)
    return api.ok(api.post(f"/profiles/{profile}/devices", {"kind": "smartwatch", "source": "healthkit", "name": "Watch", "external_id": external, "manufacturer": "Acme", "model": "W1"}), 201)


def test_device_validation(api, profile):
    api.ok(api.put(f"/profiles/{profile}/consents/health_data_integration", {"granted": True}), 200)
    base = f"/profiles/{profile}/devices"
    good = {"kind": "scale", "source": "manual", "name": "Scale"}
    for bad in (
        {**good, "kind": "toaster"},
        {**good, "source": "carrier-pigeon"},
        {**good, "name": "  "},
        {**good, "name": "x" * 121},
        {**good, "manufacturer": "m" * 121},
        {**good, "unknown_field": 1},
    ):
        assert api.post(base, bad).status_code == 400, bad
    assert api.post(base, good).status_code == 201  # no external id: always a new device
    assert api.post(base, good).status_code == 201
    assert len(api.ok(api.get(base), 200)) == 2


def test_device_update_and_ownership_errors(api, profile):
    device = connect(api, profile)
    path = f"/profiles/{profile}/devices/{device['id']}"
    assert api.patch(path, {}).status_code == 400
    assert api.patch(path, {"name": " "}).status_code == 400
    assert api.patch(path, {"status": "asleep"}).status_code == 400
    renamed = api.ok(api.patch(path, {"name": "Renamed"}), 200)
    assert renamed["name"] == "Renamed"
    api.ok(api.patch(path, {"status": "disconnected"}), 200)
    again = api.ok(api.post(f"/profiles/{profile}/devices", {"kind": "smartwatch", "source": "healthkit", "name": "Watch 2", "external_id": "ah"}), 200)
    assert again["status"] == "connected" and again["id"] == device["id"]

    missing = f"/profiles/{profile}/devices/00000000-0000-0000-0000-000000000000"
    assert api.patch(missing, {"name": "x"}).status_code == 404
    assert api.delete(missing).status_code == 404
    assert api.get(f"/profiles/{profile}/devices/not-a-uuid/syncs").status_code == 404
    assert api.post(f"/profiles/{profile}/devices/not-a-uuid/readings", {"readings": readings(1)}).status_code == 404


def test_sync_validation_history_and_readings(api, profile):
    device = connect(api, profile)
    path = f"/profiles/{profile}/devices/{device['id']}/readings"
    assert api.post(path, {"readings": []}).status_code == 400
    assert api.post(path, {"readings": readings(1) * 501}).status_code == 400
    mixed = [
        {"metric": "nonsense", "value": 1, "unit": "bpm", "measured_at": stamp(1)},
        {"metric": "heart_rate", "unit": "bpm", "measured_at": stamp(1)},
        {"metric": "heart_rate", "value": 70, "unit": "furlongs", "measured_at": stamp(1)},
        {"metric": "heart_rate", "value": 70, "unit": "bpm", "measured_at": "yesterday"},
        {"metric": "heart_rate", "value": 70, "unit": "bpm", "measured_at": "2999-01-01T00:00:00Z"},
        {"metric": "heart_rate", "value": 70, "unit": "bpm", "measured_at": "1980-01-01T00:00:00Z"},
        {"metric": "heart_rate", "value": 70, "unit": "bpm", "measured_at": stamp(1), "utc_offset_minutes": 5000},
        {"metric": "body_temperature", "value": 98.6, "unit": "°F", "measured_at": stamp(2), "utc_offset_minutes": 330},
        {"metric": "body_weight", "value": 150, "unit": "lb", "measured_at": stamp(2)},
        {"metric": "blood_glucose", "value": 5.5, "unit": "mmol/L", "measured_at": stamp(2)},
        {"metric": "steps", "value": 4000, "unit": "count", "measured_at": stamp(3), "external_id": "step-a"},
        {"metric": "steps", "value": 3000, "unit": "count", "measured_at": stamp(3), "external_id": "step-b"},
        {"metric": "sleep_duration", "value": 450, "unit": "min", "measured_at": stamp(3)},
    ]
    result = api.ok(api.post(path, {"readings": mixed}), 200)
    assert (result["accepted"], result["rejected"]) == (6, 7)
    assert {e["index"] for e in result["errors"]} == {0, 1, 2, 3, 4, 5, 6}

    syncs = api.ok(api.get(f"/profiles/{profile}/devices/{device['id']}/syncs"), 200)
    assert syncs[0]["accepted"] == 6
    assert api.get(f"/profiles/{profile}/devices/{device['id']}/syncs?limit=0").status_code == 400

    base = f"/profiles/{profile}/device-readings"
    everything = api.ok(api.get(base), 200)
    assert len(everything) == 6 and all(r["measurement_kind"] == "device" for r in everything)
    temp = api.ok(api.get(base + "?metric=body_temperature"), 200)
    assert temp[0]["unit"] == "°C" and abs(temp[0]["value"] - 37.0) < 0.1
    assert len(api.ok(api.get(base + f"?from={ago(10)}&to={ago(0)}&limit=2&offset=1"), 200)) == 2
    assert api.get(base + "?metric=nonsense").status_code == 400
    assert api.get(base + "?from=nope").status_code == 400
    assert api.get(base + "?limit=-1").status_code == 400

    steps = api.ok(api.get(base + "/steps/daily?days=30"), 200)
    assert steps["cumulative"] is True and steps["days"][0]["sum"] == 7000
    assert api.get(base + "/steps/daily?days=0").status_code == 400
    assert api.get(base + "/nonsense/daily").status_code == 400
    assert api.get(base + "/nonsense/lab-comparison").status_code == 400

    glucose = api.ok(api.get(base + "/blood_glucose/lab-comparison?window_days=7"), 200)
    assert glucose["pairs"] == [] and "not interchangeable" in glucose["note"]
    assert api.get(base + "/blood_glucose/lab-comparison?window_days=99").status_code == 400

    summary = {s["metric"]: s for s in api.ok(api.get(base + "/summary"), 200)["summaries"]}
    assert {"steps", "body_weight", "blood_glucose", "body_temperature", "sleep_duration"} <= set(summary)
    assert abs(summary["body_weight"]["latest_value"] - 68.04) < 0.1
    assert abs(summary["sleep_duration"]["latest_value"] - 7.5) < 0.01


def test_summary_trend_and_typical_range(api, profile):
    device = connect(api, profile, "trend")
    path = f"/profiles/{profile}/devices/{device['id']}/readings"
    batch = []
    for day in range(0, 60):
        batch.append({"metric": "resting_heart_rate", "value": 75 if day < 30 else 55, "unit": "bpm", "measured_at": stamp(day), "external_id": f"t{day}"})
    batch += [{"metric": "oxygen_saturation", "value": 90, "unit": "%", "measured_at": stamp(d), "external_id": f"o{d}"} for d in range(5)]
    api.ok(api.post(path, {"readings": batch}), 200)
    summary = {s["metric"]: s for s in api.ok(api.get(f"/profiles/{profile}/device-readings/summary"), 200)["summaries"]}
    assert summary["resting_heart_rate"]["trend"] == "rising"
    assert summary["oxygen_saturation"]["outside_typical_range"] is True


def test_device_data_is_private_between_profiles(api, profile, bare_profile):
    device = connect(api, profile, "private")
    assert api.get(f"/profiles/{bare_profile}/devices/{device['id']}/syncs").status_code == 404
    assert api.post(f"/profiles/{bare_profile}/devices/{device['id']}/readings", {"readings": readings(1)}).status_code == 404
    assert api.get(f"/profiles/{bare_profile}/device-readings").status_code == 200
    assert api.ok(api.get(f"/profiles/{bare_profile}/device-readings"), 200) == []
