"""Research agent connectors for open standards. Every outbound call goes through Fetcher,
which enforces a host allowlist, timeouts, retries and a minimum interval between calls."""
from __future__ import annotations

import html
import re
import threading
import time
import xml.etree.ElementTree as ET
from dataclasses import dataclass, field
from urllib.parse import quote, urlparse

import httpx

ALLOWED_HOSTS = {
    "clinicaltables.nlm.nih.gov",
    "ucum.nlm.nih.gov",
    "eutils.ncbi.nlm.nih.gov",
    "www.ebi.ac.uk",
    "clinicaltrials.gov",
    "medlineplus.gov",
    "wsearch.nlm.nih.gov",
}
USER_AGENT = "cortex-research-agent/1.0 (personal health app; non-commercial lookups)"


class EgressDenied(Exception):
    pass


class Fetcher:
    def __init__(self, timeout: float = 15.0, retries: int = 2, min_interval: float = 0.35, client: httpx.Client | None = None):
        self._client = client or httpx.Client(timeout=timeout, headers={"User-Agent": USER_AGENT}, follow_redirects=False)
        self._retries = retries
        self._min_interval = min_interval
        self._last = 0.0
        self._lock = threading.Lock()

    def get(self, url: str) -> httpx.Response:
        parsed = urlparse(url)
        if parsed.scheme != "https" or parsed.hostname not in ALLOWED_HOSTS:
            raise EgressDenied(f"host not allowed: {parsed.hostname}")
        last_exc: Exception | None = None
        for attempt in range(self._retries + 1):
            with self._lock:
                wait = self._min_interval - (time.monotonic() - self._last)
                if wait > 0:
                    time.sleep(wait)
                self._last = time.monotonic()
            try:
                resp = self._client.get(url)
                if resp.status_code in (429, 500, 502, 503, 504) and attempt < self._retries:
                    time.sleep(0.5 * 2**attempt)
                    continue
                resp.raise_for_status()
                return resp
            except httpx.HTTPError as exc:
                last_exc = exc
                if attempt < self._retries:
                    time.sleep(0.5 * 2**attempt)
        raise RuntimeError(f"fetch failed for {parsed.hostname}: {last_exc}")


@dataclass
class Evidence:
    source: str
    url: str
    detail: str = ""

    def as_dict(self) -> dict:
        return {"source": self.source, "url": self.url, "detail": self.detail}


@dataclass
class LoincMatch:
    loinc_num: str
    long_name: str
    component: str
    short_name: str = ""
    property: str = ""
    synonyms: list[str] = field(default_factory=list)


def search_loinc(fetcher: Fetcher, term: str, limit: int = 5) -> list[LoincMatch]:
    url = (
        "https://clinicaltables.nlm.nih.gov/api/loinc_items/v3/search"
        f"?terms={quote(term)}&maxList={limit}&df=LOINC_NUM,LONG_COMMON_NAME,COMPONENT&ef=SHORTNAME,RELATEDNAMES2,PROPERTY"
    )
    data = fetcher.get(url).json()
    codes, extras, display = data[1], data[2] or {}, data[3]
    shorts = extras.get("SHORTNAME") or []
    related = extras.get("RELATEDNAMES2") or []
    props = extras.get("PROPERTY") or []
    out = []
    for i, code in enumerate(codes):
        row = display[i] if i < len(display) else ["", "", ""]
        syn_raw = related[i] if i < len(related) and related[i] else []
        if isinstance(syn_raw, str):
            syn_raw = syn_raw.split(";")
        out.append(LoincMatch(code, row[1], row[2], shorts[i] if i < len(shorts) and shorts[i] else "", props[i] if i < len(props) and props[i] else "", [s.strip() for s in syn_raw if s.strip()]))
    return out


def is_valid_ucum(fetcher: Fetcher, unit: str) -> bool:
    if not unit or len(unit) > 40:
        return False
    return fetcher.get(f"https://ucum.nlm.nih.gov/ucum-service/v1/isValidUCUM/{quote(unit, safe='')}").text.strip().lower() == "true"


_UCUM_RESULT = re.compile(r"<ResultQuantity>([^<]+)</ResultQuantity>")


def ucum_transform(fetcher: Fetcher, value: float, from_unit: str, to_unit: str) -> float | None:
    """Converts with the NLM UCUM service. Returns None when the units are not commensurable,
    for example mass to moles, which needs a molecular weight the service cannot supply."""
    if not from_unit or not to_unit or len(from_unit) > 40 or len(to_unit) > 40:
        return None
    url = f"https://ucum.nlm.nih.gov/ucum-service/v1/ucumtransform/{value:g}/from/{quote(from_unit, safe='')}/to/{quote(to_unit, safe='')}"
    match = _UCUM_RESULT.search(fetcher.get(url).text)
    if not match:
        return None
    try:
        return float(match.group(1))
    except ValueError:
        return None


_LINK = re.compile(r'href="(https://medlineplus\.gov/lab-tests/([a-z0-9\-]+)/)"[^>]*>([^<]+)<')


def list_medlineplus_lab_tests(fetcher: Fetcher) -> list[dict]:
    page = fetcher.get("https://medlineplus.gov/lab-tests/").text
    seen: dict[str, dict] = {}
    for url, slug, title in _LINK.findall(page):
        seen.setdefault(slug, {"slug": slug, "url": url, "title": html.unescape(title).strip()})
    return list(seen.values())


def medlineplus_summary(fetcher: Fetcher, url: str) -> str:
    page = fetcher.get(url).text
    m = re.search(r'<meta name="description" content="([^"]+)"', page)
    return html.unescape(m.group(1)).strip() if m else ""


@dataclass
class Topic:
    title: str
    url: str
    summary: str


def search_medlineplus_topics(fetcher: Fetcher, query: str, limit: int = 3) -> list[Topic]:
    """Plain-language health topic pages from MedlinePlus (National Library of Medicine)."""
    xml = fetcher.get(f"https://wsearch.nlm.nih.gov/ws/query?db=healthTopics&retmax={limit}&term={quote(query)}").text
    out: list[Topic] = []
    for doc in ET.fromstring(xml).iter("document"):
        fields = {c.get("name"): "".join(c.itertext()) for c in doc.iter("content")}
        url = doc.get("url") or ""
        if url.startswith("https://medlineplus.gov/") and fields.get("title"):
            summary = re.sub(r"\s+", " ", html.unescape(re.sub(r"<[^>]+>", "", fields.get("FullSummary", "")))).strip()
            out.append(Topic(re.sub(r"<[^>]+>", "", html.unescape(fields["title"])).strip(), url, summary[:1500]))
    return out[:limit]


@dataclass
class Abstract:
    pmid: str
    title: str
    journal: str
    year: int | None
    text: str

    @property
    def url(self) -> str:
        return f"https://pubmed.ncbi.nlm.nih.gov/{self.pmid}/"


def search_pubmed(fetcher: Fetcher, query: str, limit: int = 5) -> list[Abstract]:
    ids = fetcher.get(
        "https://eutils.ncbi.nlm.nih.gov/entrez/eutils/esearch.fcgi"
        f"?db=pubmed&retmode=json&retmax={limit}&sort=relevance&term={quote(query + ' AND hasabstract[text] AND (review[pt] OR meta-analysis[pt] OR guideline[pt])')}"
    ).json()["esearchresult"]["idlist"]
    if not ids:
        return []
    xml = fetcher.get(f"https://eutils.ncbi.nlm.nih.gov/entrez/eutils/efetch.fcgi?db=pubmed&retmode=xml&id={','.join(ids)}").text
    return parse_pubmed_xml(xml)


def parse_pubmed_xml(xml: str) -> list[Abstract]:
    out = []
    root = ET.fromstring(xml)
    for art in root.findall("PubmedArticle"):
        pmid = (art.findtext("MedlineCitation/PMID") or "").strip()
        title = "".join(art.find("MedlineCitation/Article/ArticleTitle").itertext()).strip() if art.find("MedlineCitation/Article/ArticleTitle") is not None else ""
        parts = []
        for node in art.findall("MedlineCitation/Article/Abstract/AbstractText"):
            label = node.get("Label")
            body = "".join(node.itertext()).strip()
            if body:
                parts.append(f"{label}: {body}" if label else body)
        year_txt = art.findtext("MedlineCitation/Article/Journal/JournalIssue/PubDate/Year") or ""
        if pmid and title and parts:
            out.append(Abstract(pmid, title, art.findtext("MedlineCitation/Article/Journal/Title") or "", int(year_txt) if year_txt.isdigit() else None, "\n".join(parts)))
    return out


@dataclass
class Paper:
    identifier: str
    title: str
    journal: str
    year: int | None
    text: str
    url: str
    source: str
    kind: str = "article"


def search_europepmc(fetcher: Fetcher, query: str, limit: int = 5) -> list[Paper]:
    """Reviews, meta-analyses and guidelines with an abstract. Records without an abstract are skipped."""
    q = f"({query}) AND HAS_ABSTRACT:y AND (PUB_TYPE:\"review\" OR PUB_TYPE:\"meta-analysis\" OR PUB_TYPE:\"guideline\")"
    data = fetcher.get(
        "https://www.ebi.ac.uk/europepmc/webservices/rest/search"
        f"?format=json&resultType=core&pageSize={limit}&query={quote(q)}"
    ).json()
    out = []
    for r in (data.get("resultList") or {}).get("result", []):
        text = re.sub(r"<[^>]+>", "", html.unescape(r.get("abstractText") or "")).strip()
        title = re.sub(r"<[^>]+>", "", html.unescape(r.get("title") or "")).strip().rstrip(".")
        src, rid = r.get("source"), r.get("id")
        if not (text and title and src and rid):
            continue
        year = r.get("pubYear")
        out.append(Paper(
            identifier=f"PMID:{r['pmid']}" if r.get("pmid") else f"{src}:{rid}",
            title=title, journal=r.get("journalTitle") or "", year=int(year) if str(year).isdigit() else None,
            text=text, url=f"https://europepmc.org/article/{src}/{rid}", source="Europe PMC",
        ))
    return out


def search_trials(fetcher: Fetcher, query: str, limit: int = 3) -> list[Paper]:
    """Registered clinical studies. A trial record describes a study, not a finding."""
    data = fetcher.get(
        "https://clinicaltrials.gov/api/v2/studies"
        f"?query.term={quote(query)}&pageSize={limit}&filter.overallStatus=COMPLETED&format=json"
    ).json()
    out = []
    for st in data.get("studies", []):
        ps = st.get("protocolSection", {})
        ident = ps.get("identificationModule", {})
        nct = ident.get("nctId")
        title = ident.get("briefTitle")
        summary = (ps.get("descriptionModule") or {}).get("briefSummary", "").strip()
        if not (nct and title and summary):
            continue
        date = ((ps.get("statusModule") or {}).get("completionDateStruct") or {}).get("date", "")
        out.append(Paper(
            identifier=nct, title=title, journal="ClinicalTrials.gov registered study",
            year=int(date[:4]) if date[:4].isdigit() else None, text=summary,
            url=f"https://clinicaltrials.gov/study/{nct}", source="ClinicalTrials.gov", kind="trial",
        ))
    return out
