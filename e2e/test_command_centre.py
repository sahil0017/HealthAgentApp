"""Personal Health Command Centre: overview, timeline, history, goals, follow-ups, completeness, reminders."""
import uuid

from conftest import CODES as C, add_verified, ago, ahead


def test_history_entries(api, profile):
    base = f"/profiles/{profile}/history"
    entry = api.ok(api.post(base, {"kind": "condition", "title": "Asthma", "notes": "mild", "started_on": "2015-01-01", "status": "active"}), 201)
    assert api.post(base, {"kind": "bogus", "title": "x"}).status_code == 400
    assert api.post(base, {"kind": "allergy"}).status_code == 400
    assert api.post(base, {"kind": "allergy", "title": "Peanut", "started_on": "garbage"}).status_code == 400
    assert api.post(base, {"kind": "allergy", "title": "Peanut", "started_on": "2020-02-01", "ended_on": "2019-01-01"}).status_code == 400
    assert any(e["id"] == entry["id"] for e in api.ok(api.get(base), 200))
    assert api.ok(api.patch(f"/history/{entry['id']}", {"status": "resolved", "ended_on": "2024-01-01"}), 200)["status"] == "resolved"
    assert api.patch(f"/history/{entry['id']}", {"kind": "bogus"}).status_code == 400
    assert api.patch(f"/history/{uuid.uuid4()}", {"title": "x"}).status_code == 404
    assert api.delete(f"/history/{entry['id']}").status_code in (200, 204)
    assert api.delete(f"/history/{entry['id']}").status_code == 404


def test_goals_and_progress(api, profile):
    base = f"/profiles/{profile}/goals"
    assert api.post(base, {"biomarker_code": C["a1c"], "unit": "%"}).status_code == 400
    assert api.post(base, {"biomarker_code": "NOPE", "unit": "%", "target_max": 5}).status_code == 400
    assert api.post(base, {"biomarker_code": C["a1c"], "unit": "%", "target_min": 9, "target_max": 5}).status_code == 400
    goal = api.ok(api.post(base, {"biomarker_code": C["a1c"], "unit": "%", "target_max": 5.7, "title": "Keep A1c under 5.7"}), 201)

    no_data = api.ok(api.get(f"/profiles/{profile}/overview"), 200)
    assert any(a["kind"] == "goal_without_data" for a in no_data["attention"])

    add_verified(api, profile, C["a1c"], 6.4, "%", ago(30))
    add_verified(api, profile, C["a1c"], 6.0, "%", ago(3))
    goals = api.ok(api.get(base), 200)
    assert goals[0]["progress"]["status"]
    assert api.ok(api.patch(f"/goals/{goal['id']}", {"target_max": 6.5}), 200)
    assert api.patch(f"/goals/{goal['id']}", {"biomarker_code": C["glucose"]}).status_code == 400
    assert api.patch(f"/goals/{goal['id']}", {"target_min": 99}).status_code == 400
    assert api.ok(api.patch(f"/goals/{goal['id']}", {"status": "archived"}), 200)
    assert api.ok(api.get(base + "?status=active"), 200) == []
    assert api.patch(f"/goals/{uuid.uuid4()}", {"title": "x"}).status_code == 404
    assert api.delete(f"/goals/{goal['id']}").status_code in (200, 204)


def test_follow_ups_reminders_and_suggestions(api, profile):
    base = f"/profiles/{profile}/follow-ups"
    assert api.post(base, {"kind": "bogus", "title": "x"}).status_code == 400
    assert api.post(base, {"kind": "other"}).status_code == 400
    assert api.post(base, {"kind": "other", "title": "x", "due_on": "soon"}).status_code == 400
    overdue = api.ok(api.post(base, {"kind": "appointment", "title": "Overdue visit", "due_on": ago(5)}), 201)
    api.ok(api.post(base, {"kind": "investigation", "title": "Soon test", "due_on": ahead(3)}), 201)
    api.ok(api.post(base, {"kind": "other", "title": "No date"}), 201)
    assert len(api.ok(api.get(base), 200)) == 3
    assert api.get(base + "?status=bogus").status_code == 400
    assert api.get(base + "?status=open").status_code == 200

    overview = api.ok(api.get(f"/profiles/{profile}/overview"), 200)
    assert any(a["kind"] == "overdue_follow_up" for a in overview["attention"])
    assert overview["next_follow_up"] is not None

    done = api.ok(api.patch(f"/follow-ups/{overdue['id']}", {"status": "done"}), 200)
    assert done["status"] == "done"
    assert api.patch(f"/follow-ups/{overdue['id']}", {"status": "nonsense"}).status_code == 400
    assert api.patch(f"/follow-ups/{uuid.uuid4()}", {"title": "x"}).status_code == 404
    assert api.delete(f"/follow-ups/{overdue['id']}").status_code in (200, 204)
    assert api.delete(f"/follow-ups/{uuid.uuid4()}").status_code == 404


def test_overview_flags_out_of_range_and_measurement_gaps(api, profile):
    for days in (200, 170, 140):
        add_verified(api, profile, C["glucose"], 90, "mg/dL", ago(days), "70-99")
    add_verified(api, profile, C["ldl"], 160, "mg/dL", ago(2), "0-100")
    overview = api.ok(api.get(f"/profiles/{profile}/overview"), 200)
    kinds = {a["kind"] for a in overview["attention"]}
    assert "out_of_range" in kinds
    assert "measurement_gap" in kinds
    assert overview["summary"]["biomarkers_tracked"] >= 2
    assert overview["completeness"] is not None
    assert "recent_changes" in overview and "goals" in overview and "suggestions" in overview


def test_timeline_filters_and_paging(api, profile):
    api.ok(api.post(f"/profiles/{profile}/history", {"kind": "surgery", "title": "Appendectomy", "started_on": "2018-03-01"}), 201)
    add_verified(api, profile, C["glucose"], 95, "mg/dL", ago(4))
    base = f"/profiles/{profile}/timeline"
    assert len(api.ok(api.get(base), 200)) >= 2
    assert api.ok(api.get(base + "?kind=history"), 200) is not None
    assert api.ok(api.get(base + f"?from={ago(10)}&to={ago(0)}&limit=1&offset=0"), 200) is not None
    assert api.get(base + "?kind=bogus").status_code == 400
    assert api.get(base + "?from=nope").status_code == 400
    assert api.get(base + f"?from={ago(0)}&to={ago(10)}").status_code == 400
    assert api.get(base + "?limit=0").status_code == 400


def test_profile_isolation(api, profile):
    other = api.ok(api.post("/profiles", {"display_name": "Other"}), 201)["id"]
    try:
        # Unknown ids behave like another owner's: not found, never forbidden-with-details.
        for path in ("overview", "timeline", "goals", "history", "follow-ups", "observations"):
            assert api.get(f"/profiles/{uuid.uuid4()}/{path}").status_code == 404
        assert api.ok(api.get(f"/profiles/{other}/goals"), 200) == []
    finally:
        api.delete(f"/profiles/{other}", {"confirm": "DELETE"})
