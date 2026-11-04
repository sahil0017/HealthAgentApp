"""Research agent: turns a term into cited, pending proposals. It never writes to the catalog itself."""
from __future__ import annotations

import re

from app.research.sources import (
    Evidence,
    Fetcher,
    list_medlineplus_lab_tests,
    medlineplus_summary,
    search_loinc,
    search_europepmc,
    search_pubmed,
    search_trials,
    ucum_transform,
)

_NON_WORD = re.compile(r"[^A-Za-z0-9]+")
_GENERIC = {"blood", "chemistry", "serum", "plasma", "urine", "hematology", "bld", "ser", "pl", "random", "point in time", "level", "qnt", "quantitative", "quan", "lipid", "cardio", "cardiology"}
_STOP = {"test", "tests", "panel", "level", "levels", "blood", "serum"}


def code_for(name: str) -> str:
    return _NON_WORD.sub("_", name.strip().upper()).strip("_")[:48]


def _tokens(s: str) -> set[str]:
    return {t for t in _NON_WORD.sub(" ", s.lower()).split() if t not in _STOP}


def _is_name_like(candidate: str, component: str) -> bool:
    """LOINC related-name lists mix aliases with category and specimen tags; keep only names that share a
    word with the component. Abbreviations are learned later from real labels via their own research task."""
    return bool(_tokens(candidate) & _tokens(component))


def research_biomarker(fetcher: Fetcher, term: str, medlineplus_url: str | None = None) -> dict | None:
    """Returns a proposal only if LOINC has a component whose words match the term (agreement guard)."""
    matches = search_loinc(fetcher, term, limit=200)
    want = _tokens(term)
    if not want:
        return None
    # LOINC PROPERTY "Find" marks narrative/guidance items and "-" marks panels; neither is a measurable analyte.
    matches = [m for m in matches if m.property not in ("Find", "-", "")]
    good = [m for m in matches if want & _tokens(m.component) or want & _tokens(m.long_name)]
    if not good:
        return None
    # Prefer the component whose words equal the term; otherwise the most concise overlapping one.
    good.sort(key=lambda m: (_tokens(m.component) != want, len(_tokens(m.component) ^ want)))
    best = good[0]
    same = [m for m in good if _tokens(m.component) == _tokens(best.component)]
    counts: dict[str, int] = {}
    for m in same:
        for s_ in {x.strip() for x in [m.component, *m.synonyms] if x}:
            counts[s_] = counts.get(s_, 0) + 1
    # A synonym is kept only when independent LOINC records agree on it (or it is the term/component itself).
    synonyms = {term.strip(): None, best.component: None}
    for s_, n in sorted(counts.items(), key=lambda kv: -kv[1]):
        if n >= 2 and len(s_) <= 60 and s_.lower() not in _GENERIC and not s_.lower().endswith(("panel", "pnl")) and re.search(r"[A-Za-z]", s_) and _is_name_like(s_, best.component):
            synonyms.setdefault(s_, None)
        if len(synonyms) >= 8:
            break
    evidence = [Evidence("LOINC", f"https://loinc.org/{best.loinc_num}", best.long_name).as_dict()]
    if medlineplus_url:
        summary = medlineplus_summary(fetcher, medlineplus_url)
        if summary:
            evidence.append(Evidence("MedlinePlus", medlineplus_url, summary).as_dict())
    loincs = sorted({m.loinc_num for m in same})
    name = term.strip() if _tokens(term) == _tokens(best.component) else (best.component or term)
    code = code_for(name)
    return {
        "key": code,
        "payload": {
            "code": code,
            "display_name": name,
            "synonyms": list(synonyms),
            "supported_units": [],
            "loinc_nums": loincs,
            "reference_note": next((e["detail"] for e in evidence if e["source"] == "MedlinePlus"), None),
        },
        "evidence": evidence,
        "agreement": len(evidence),
    }


def research_literature(fetcher: Fetcher, term: str, limit: int = 3) -> list[dict]:
    """PubMed and Europe PMC reviews plus registered trials. One failing source never hides the others."""
    papers: list[tuple[str, str, str, int | None, str, str, str]] = []
    seen: set[str] = set()

    def add(ident, title, journal, year, text, url, source):
        if ident not in seen:
            seen.add(ident)
            papers.append((ident, title, journal, year, text, url, source))

    try:
        for a in search_pubmed(fetcher, term, limit):
            add(f"PMID:{a.pmid}", a.title, a.journal, a.year, a.text, a.url, "PubMed")
    except Exception:
        pass
    for fn, n in ((search_europepmc, limit), (search_trials, 2)):
        try:
            for p in fn(fetcher, term, n):
                add(p.identifier, p.title, p.journal, p.year, p.text, p.url, p.source)
        except Exception:
            continue
    return [
        {
            "key": ident,
            "payload": {"identifier": ident, "title": title, "journal": journal, "year": year, "url": url, "abstract": text, "topic": term, "source": source},
            "evidence": [Evidence(source, url, title).as_dict()],
            "agreement": 1,
        }
        for ident, title, journal, year, text, url, source in papers
    ]


def discover_biomarkers(fetcher: Fetcher, limit: int = 40, offset: int = 0) -> dict:
    """Enumerates the MedlinePlus lab-test index and researches each title. No built-in analyte list."""
    out = []
    index = list_medlineplus_lab_tests(fetcher)
    for item in index[offset : offset + limit]:
        try:
            proposal = research_biomarker(fetcher, re.sub(r"\s*\(.*?\)|\s+test$", "", item["title"], flags=re.I), item["url"])
        except Exception:
            continue
        if proposal:
            out.append(proposal)
    return {"proposals": out, "total": len(index)}


def research_conversion(fetcher: Fetcher, from_unit: str, to_unit: str) -> dict | None:
    """Derives factor and offset for a unit pair from UCUM. The relation must be linear, which is
    checked with a third point, so nothing non-linear is ever proposed as a simple factor."""
    if from_unit == to_unit:
        return None
    at_zero = ucum_transform(fetcher, 0, from_unit, to_unit)
    at_one = ucum_transform(fetcher, 1, from_unit, to_unit)
    at_ten = ucum_transform(fetcher, 10, from_unit, to_unit)
    if at_zero is None or at_one is None or at_ten is None:
        return None
    factor = at_one - at_zero
    if factor <= 0 or abs((at_zero + factor * 10) - at_ten) > 1e-9 * max(1.0, abs(at_ten)):
        return None
    evidence = Evidence("UCUM", "https://ucum.nlm.nih.gov/", f"NLM UCUM service converts {from_unit} to {to_unit}")
    return {"from_unit": from_unit, "to_unit": to_unit, "factor": factor, "offset": at_zero, "evidence": [evidence.as_dict()]}
