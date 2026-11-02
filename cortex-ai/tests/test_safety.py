from app.safety import check_explanation


FACTS = "HbA1c increased from 5.6 to 6.1 % on 2026-06-10.\n[R1] Research (guideline, verified): HbA1c targets."


def test_grounded_answer_passes():
    assert check_explanation("Your HbA1c rose from 5.6 to 6.1 % [R1].", FACTS).passed


def test_unsupported_number_is_rejected():
    result = check_explanation("Your HbA1c is 7.2 %.", FACTS)
    assert any("7.2" in v for v in result.violations)


def test_unknown_citation_is_rejected():
    result = check_explanation("See [R9] for details.", FACTS)
    assert any("R9" in v for v in result.violations)


def test_diagnosis_language_is_rejected():
    assert not check_explanation("You have diabetes.", FACTS).passed
    assert not check_explanation("You should take 500 mg daily.", FACTS).passed
