from app.workflows.health_question import build_health_question_graph


def invoke(question, observations=None, explain=lambda facts, q: None):
    graph = build_health_question_graph(explain=explain)
    return graph.invoke({"question": question, "observations": observations or []})


OBSERVATIONS = [
    {"name": "HbA1c", "value": 5.6, "unit": "%", "observed_at": "2026-01-10T00:00:00Z", "reference_range": ""},
    {"name": "HbA1c", "value": 6.1, "unit": "%", "observed_at": "2026-06-10T00:00:00Z", "reference_range": ""},
]


def test_question_without_observations_asks_for_upload():
    result = invoke("How is my health?")
    assert result["domains"] == ["medical_records"]
    assert "no user-approved lab observations" in result["findings"][0]
    assert result["sources"] == []


def test_lab_question_compares_latest_two_values():
    result = invoke("Why did my HbA1c increase?", OBSERVATIONS)
    assert result["domains"] == ["medical_records", "lab_intelligence"]
    assert "HbA1c increased from 5.6 to 6.1 %" in " ".join(result["findings"])


def test_mixed_units_are_not_compared():
    observations = [
        {"name": "Glucose", "value": 5.0, "unit": "mmol/L", "observed_at": "2026-01-01", "reference_range": ""},
        {"name": "Glucose", "value": 90, "unit": "mg/dL", "observed_at": "2026-06-01", "reference_range": ""},
    ]
    result = invoke("Compare my glucose results", observations)
    assert any("different units" in item for item in result["limitations"])
    assert not any("increased" in item or "decreased" in item for item in result["findings"])


def test_research_question_routes_to_research_agent_with_limitation():
    result = invoke("What does the research say about HbA1c?", OBSERVATIONS)
    assert "health_research" in result["domains"]
    assert "No approved research sources are available yet." in result["limitations"]


def test_explanation_is_appended_after_recorded_facts():
    result = invoke("Compare my HbA1c", OBSERVATIONS, explain=lambda facts, q: "Your value rose.")
    assert "HbA1c increased" in result["answer"]
    assert result["answer"].endswith("Your value rose.")
    assert not any("unavailable" in item for item in result["limitations"])


def test_explanation_failure_falls_back_to_facts():
    def broken(facts, question):
        raise RuntimeError("model crashed")

    result = invoke("Compare my HbA1c", OBSERVATIONS, explain=broken)
    assert "HbA1c increased" in result["answer"]
    assert "AI-generated" not in result["answer"]
    assert any("unavailable" in item for item in result["limitations"])


def test_retrieved_research_chunks_route_to_health_research():
    from app.workflows.health_question import orchestrate

    base = {"question": "how is prediabetes managed", "observations": []}
    assert "health_research" not in orchestrate({**base, "research_chunks": []})["domains"]
    assert "health_research" in orchestrate({**base, "research_chunks": [{"text": "x"}]})["domains"]
