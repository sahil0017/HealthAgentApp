import re
from dataclasses import dataclass
from datetime import date

UNIT = r"(%|mmol/mol|mg/dL|mmol/L|g/dL|g/L|mg/L|ng/mL|nmol/L|mIU/L|µIU/mL|μIU/mL|pg/mL|µmol/L|μmol/L|umol/L|U/L)"
NUMBER = r"[-+]?\d+(?:\.\d+)?"
RANGE = re.compile(rf"(\d+(?:\.\d+)?)\s*(?:-|–|—|to)\s*(\d+(?:\.\d+)?)|([<>]=?)\s*(\d+(?:\.\d+)?)")
ISO_DATE = re.compile(r"\b(\d{4})-(\d{2})-(\d{2})\b")
MONTH_DATE = re.compile(r"\b(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)[a-z]*\.?\s+(\d{1,2}),?\s+(\d{4})\b")
MONTHS = {m: i for i, m in enumerate(["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"], start=1)}


@dataclass(frozen=True)
class Analyte:
    code: str
    display: str
    pattern: re.Pattern
    units: tuple[str, ...]


def _analyte(code, display, aliases, units):
    return Analyte(code, display, re.compile(rf"(?<![A-Za-z])(?:{aliases})(?![A-Za-z])", re.IGNORECASE), units)


ANALYTES = (
    _analyte("HBA1C", "HbA1c", r"hb\s?a1c|hemoglobin a1c|glycated hemoglobin|a1c", ("%", "mmol/mol")),
    _analyte("GLUCOSE", "Glucose", r"fasting glucose|glucose|blood sugar", ("mg/dL", "mmol/L")),
    _analyte("LDL", "LDL cholesterol", r"ldl(?: cholesterol)?", ("mg/dL", "mmol/L")),
    _analyte("HDL", "HDL cholesterol", r"hdl(?: cholesterol)?", ("mg/dL", "mmol/L")),
    _analyte("TOTAL_CHOLESTEROL", "Total cholesterol", r"total cholesterol|cholesterol, total|cholesterol", ("mg/dL", "mmol/L")),
    _analyte("TRIGLYCERIDES", "Triglycerides", r"triglycerides?", ("mg/dL", "mmol/L")),
    _analyte("CREATININE", "Creatinine", r"creatinine", ("mg/dL", "µmol/L", "μmol/L", "umol/L")),
    _analyte("HEMOGLOBIN", "Hemoglobin", r"hemoglobin|haemoglobin|hgb", ("g/dL", "g/L")),
    _analyte("TSH", "TSH", r"tsh|thyroid stimulating hormone", ("mIU/L", "µIU/mL", "μIU/mL")),
    _analyte("VITAMIN_D", "Vitamin D", r"vitamin d|25-?hydroxy\s*vitamin d|25\(oh\)d", ("ng/mL", "nmol/L")),
)
UNIT_PATTERN = re.compile(UNIT)
VALUE_PATTERN = re.compile(NUMBER)


@dataclass(frozen=True)
class Candidate:
    code: str
    name: str
    value: float
    unit: str
    observed_at: str | None
    reference_range: str
    page: int
    source_line: str
    confidence: str

    def as_dict(self) -> dict:
        return {
            "name": self.name,
            "biomarker_code": self.code,
            "value": self.value,
            "unit": self.unit,
            "observed_at": self.observed_at,
            "reference_range": self.reference_range,
            "page": self.page,
            "source_line": self.source_line[:200],
            "confidence": self.confidence,
        }


def find_document_date(text: str) -> str | None:
    iso = ISO_DATE.search(text)
    if iso:
        return _valid_iso(*iso.groups())
    month = MONTH_DATE.search(text)
    if month:
        return _valid_iso(month.group(3), str(MONTHS[month.group(1)[:3].title()]), month.group(2))
    return None


def _valid_iso(year: str, month: str, day: str) -> str | None:
    try:
        return date(int(year), int(month), int(day)).isoformat()
    except ValueError:
        return None


def parse_line(line: str, page: int, observed_at: str | None) -> Candidate | None:
    best = None
    for analyte in ANALYTES:
        for match in analyte.pattern.finditer(line):
            key = (match.start(), -(match.end() - match.start()))
            if best is None or key < best[0]:
                best = (key, analyte, match)
    if best is None:
        return None
    _, analyte, match = best
    rest = line[match.end():]
    value_match = VALUE_PATTERN.search(rest)
    if not value_match or value_match.start() > 40:
        return None
    value = float(value_match.group(0))
    after_value = rest[value_match.end():]
    unit_match = UNIT_PATTERN.search(after_value[:20])
    unit = unit_match.group(1) if unit_match else ""
    if unit and unit not in analyte.units:
        return None
    tail = after_value[unit_match.end():] if unit_match else after_value
    range_match = RANGE.search(tail)
    reference_range = range_match.group(0).strip() if range_match else ""
    confidence = "high" if unit and reference_range else "medium" if unit else "low"
    return Candidate(
        code=analyte.code,
        name=match.group(0).strip(),
        value=value,
        unit=unit or "(unit not detected)",
        observed_at=observed_at,
        reference_range=reference_range,
        page=page,
        source_line=line.strip(),
        confidence=confidence,
    )


def extract_candidates(pages: list[tuple[int, str]]) -> tuple[list[Candidate], list[str]]:
    observed_at = find_document_date("\n".join(text for _, text in pages))
    seen: set[tuple] = set()
    candidates: list[Candidate] = []
    for page_number, text in pages:
        for line in text.splitlines():
            candidate = parse_line(line, page_number, observed_at)
            if candidate is None:
                continue
            key = (candidate.code, candidate.value, candidate.unit, candidate.page)
            if key in seen:
                continue
            seen.add(key)
            candidates.append(candidate)
    warnings: list[str] = []
    if candidates and observed_at is None:
        warnings.append("No report date was detected; set the observation date during review.")
    if any(c.confidence == "low" for c in candidates):
        warnings.append("Some values have no detected unit; check units against the original report.")
    return candidates[:100], warnings
