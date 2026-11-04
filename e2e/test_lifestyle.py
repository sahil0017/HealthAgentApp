"""Lifestyle & Symptoms: symptom journal, daily lifestyle logs, summaries, correlations, export and isolation."""
from datetime import datetime, timedelta, timezone


def day(ago):
    return (datetime.now(timezone.utc) - timedelta(days=ago)).strftime("%Y-%m-%d")


def stamp(ago):
    return (datetime.now(timezone.utc).replace(hour=10, minute=0, second=0, microsecond=0) - timedelta(days=ago)).strftime("%Y-%m-%dT%H:%M:%SZ")


def test_writes_need_storage_consent(api, bare_profile):
    assert api.post(f"/profiles/{bare_profile}/symptoms", {"name": "Headache", "severity": 4}).status_code == 403
    assert api.post(f"/profiles/{bare_profile}/lifestyle-logs", {"category": "sleep", "value": 7}).status_code == 403
    assert api.ok(api.get(f"/profiles/{bare_profile}/symptoms"), 200) == []


def test_symptom_journal_lifecycle(api, profile):
    created = api.ok(api.post(f"/profiles/{profile}/symptoms", {"name": "Headache", "severity": 6, "started_at": stamp(2), "notes": "after work"}), 201)
    assert created["active"] is True and created["severity"] == 6
    for bad in ({"name": "", "severity": 4}, {"name": "x", "severity": 0}, {"name": "x", "severity": 11}, {"severity": 3},
                {"name": "x", "severity": 3, "started_at": "2999-01-01T00:00:00Z"}):
        assert api.post(f"/profiles/{profile}/symptoms", bad).status_code == 400, bad
    assert len(api.ok(api.get(f"/profiles/{profile}/symptoms?name=headache&active=true"), 200)) == 1
    assert api.get(f"/profiles/{profile}/symptoms?active=maybe").status_code == 400
    assert api.patch(f"/profiles/{profile}/symptoms/{created['id']}", {}).status_code == 400
    resolved = api.ok(api.patch(f"/profiles/{profile}/symptoms/{created['id']}", {"severity": 3, "ended_at": stamp(1)}), 200)
    assert resolved["active"] is False and resolved["severity"] == 3
    summary = api.ok(api.get(f"/profiles/{profile}/symptoms/summary"), 200)
    assert summary["symptoms"][0]["name"] == "headache" and summary["note"]
    assert api.get(f"/profiles/{profile}/symptoms/summary?days=0").status_code == 400
    api.ok(api.delete(f"/profiles/{profile}/symptoms/{created['id']}"), 204)
    assert api.delete(f"/profiles/{profile}/symptoms/{created['id']}").status_code == 404


def test_lifestyle_logs_summary_and_correlations(api, profile):
    categories = api.ok(api.get("/lifestyle-categories"), 200)
    assert {c["code"] for c in categories} == {"sleep", "exercise", "nutrition", "hydration", "mood", "stress"}
    for bad in ({"category": "sleep"}, {"category": "yoga", "value": 1}, {"category": "sleep", "value": 25}, {"category": "mood", "value": 0}):
        assert api.post(f"/profiles/{profile}/lifestyle-logs", bad).status_code == 400, bad
    first = api.ok(api.post(f"/profiles/{profile}/lifestyle-logs", {"category": "exercise", "value": 30, "label": "Run", "logged_on": day(1)}), 201)
    api.ok(api.post(f"/profiles/{profile}/lifestyle-logs", {"category": "exercise", "value": 15, "logged_on": day(1)}), 201)
    assert len(api.ok(api.get(f"/profiles/{profile}/lifestyle-logs?category=exercise"), 200)) == 2
    assert api.get(f"/profiles/{profile}/lifestyle-logs?category=zzz").status_code == 400

    empty = api.ok(api.get(f"/profiles/{profile}/lifestyle/correlations"), 200)
    assert all(c["status"] == "insufficient_data" for c in empty["correlations"])
    # Less sleep goes with a worse headache on the same day.
    for ago in range(2, 12):
        hours = 4 + ago % 5
        api.ok(api.post(f"/profiles/{profile}/lifestyle-logs", {"category": "sleep", "value": hours, "logged_on": day(ago)}), 201)
        api.ok(api.post(f"/profiles/{profile}/symptoms", {"name": "Headache", "severity": 10 - hours + 1, "started_at": stamp(ago)}), 201)
    summary = api.ok(api.get(f"/profiles/{profile}/lifestyle/summary?days=30"), 200)
    by = {c["category"]: c for c in summary["categories"]}
    assert by["exercise"]["average_per_day"] == 45 and by["sleep"]["days_logged"] == 10 and by["hydration"]["latest"] is None
    result = api.ok(api.get(f"/profiles/{profile}/lifestyle/correlations?symptom=headache&days=30"), 200)
    sleep = next(c for c in result["correlations"] if c["category"] == "sleep")
    assert sleep["status"] == "ok" and sleep["r"] == -1 and "negative" in sleep["strength"] and result["note"]
    assert api.get(f"/profiles/{profile}/lifestyle/correlations?lag=1&days=30").status_code == 200
    assert api.get(f"/profiles/{profile}/lifestyle/correlations?lag=2").status_code == 400
    api.ok(api.delete(f"/profiles/{profile}/lifestyle-logs/{first['id']}"), 204)
    assert api.delete(f"/profiles/{profile}/lifestyle-logs/{first['id']}").status_code == 404


def test_lifestyle_data_is_private_and_exported(api, profile, bare_profile):
    symptom = api.ok(api.post(f"/profiles/{profile}/symptoms", {"name": "Cough", "severity": 2}), 201)
    api.ok(api.post(f"/profiles/{profile}/lifestyle-logs", {"category": "hydration", "value": 2000}), 201)
    assert api.delete(f"/profiles/{bare_profile}/symptoms/{symptom['id']}").status_code == 404
    assert api.ok(api.get(f"/profiles/{bare_profile}/lifestyle-logs"), 200) == []
    export = api.ok(api.get(f"/profiles/{profile}/export"), 200)
    assert any(s["name"] == "Cough" for s in export["symptom_entries"])
    assert any(l["category"] == "hydration" for l in export["lifestyle_logs"])
