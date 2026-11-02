from app.extraction.biomarkers import extract_candidates, find_document_date


def test_extracts_values_units_ranges_and_date():
    text = "Collected: 2026-10-15\nHbA1c 6.1 % (4.0 - 5.6)\nLDL cholesterol 131 mg/dL <100"
    candidates, warnings = extract_candidates([(1, text)])
    by_code = {c.code: c for c in candidates}
    assert by_code["HBA1C"].value == 6.1
    assert by_code["HBA1C"].unit == "%"
    assert by_code["HBA1C"].reference_range == "4.0 - 5.6"
    assert by_code["HBA1C"].observed_at == "2026-10-15"
    assert by_code["LDL"].reference_range == "<100"
    assert warnings == []


def test_hemoglobin_a1c_is_not_classified_as_hemoglobin():
    candidates, _ = extract_candidates([(1, "Hemoglobin A1c 5.6 %")])
    assert [c.code for c in candidates] == ["HBA1C"]


def test_unit_must_match_the_analyte():
    candidates, _ = extract_candidates([(1, "HbA1c 6.1 mg/dL")])
    assert candidates == []


def test_missing_unit_is_low_confidence_and_warned():
    candidates, warnings = extract_candidates([(1, "TSH 2.4")])
    assert candidates[0].unit == "(unit not detected)"
    assert candidates[0].confidence == "low"
    assert warnings


def test_missing_date_is_warned_not_guessed():
    candidates, warnings = extract_candidates([(1, "Glucose 104 mg/dL 70-99")])
    assert candidates[0].observed_at is None
    assert any("No report date" in w for w in warnings)


def test_month_name_dates_are_normalized():
    assert find_document_date("Collected on March 4, 2026") == "2026-03-04"
    assert find_document_date("Collected on 31/02/2026") is None


def test_duplicate_lines_are_collapsed_and_keep_page_reference():
    text = "HbA1c 6.1 %\nHbA1c 6.1 %"
    candidates, _ = extract_candidates([(1, text), (2, text)])
    assert [(c.page, c.code) for c in candidates] == [(1, "HBA1C"), (2, "HBA1C")]
