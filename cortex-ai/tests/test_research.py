from urllib.parse import unquote

import httpx
import pytest

from app.research import agent
from app.research.sources import EgressDenied, Fetcher, parse_pubmed_xml


def make_fetcher(handler):
    return Fetcher(min_interval=0, retries=0, client=httpx.Client(transport=httpx.MockTransport(handler)))


def test_egress_allowlist_blocks_other_hosts():
    f = make_fetcher(lambda r: httpx.Response(200, text="x"))
    with pytest.raises(EgressDenied):
        f.get("https://evil.example.com/x")
    with pytest.raises(EgressDenied):
        f.get("http://medlineplus.gov/x")


def loinc_handler(request: httpx.Request) -> httpx.Response:
    if "clinicaltables" in request.url.host:
        return httpx.Response(
            200,
            json=[2, ["4548-4", "17856-6"], {"SHORTNAME": ["HbA1c MFr Bld", "HbA1c MFr Bld HPLC"], "RELATEDNAMES2": [["A1C", "Glycohemoglobin"], ["A1C"]], "PROPERTY": ["MFr", "MFr"]},
                  [["4548-4", "Hemoglobin A1c/Hemoglobin.total in Blood", "Hemoglobin A1c"], ["17856-6", "Hemoglobin A1c by HPLC", "Hemoglobin A1c"]]],
        )
    return httpx.Response(200, text='<meta name="description" content="Measures glucose attached to hemoglobin." />')


def test_biomarker_proposal_is_cited_and_has_no_units_or_ranges():
    p = agent.research_biomarker(make_fetcher(loinc_handler), "Hemoglobin A1c", "https://medlineplus.gov/lab-tests/x/")
    assert p["payload"]["code"] == "HEMOGLOBIN_A1C"
    assert "A1C" in p["payload"]["synonyms"]
    assert p["payload"]["supported_units"] == []
    assert p["payload"]["loinc_nums"] == ["17856-6", "4548-4"]
    assert {e["source"] for e in p["evidence"]} == {"LOINC", "MedlinePlus"}


def test_unrelated_loinc_results_are_rejected():
    def handler(request):
        return httpx.Response(200, json=[1, ["1-1"], {}, [["1-1", "Something else", "Banana"]]])

    assert agent.research_biomarker(make_fetcher(handler), "Creatinine") is None


def test_parse_pubmed_xml_skips_books_and_missing_abstracts():
    xml = """<PubmedArticleSet><PubmedBookArticle/><PubmedArticle><MedlineCitation><PMID>1</PMID><Article>
    <Journal><Title>J</Title><JournalIssue><PubDate><Year>2021</Year></PubDate></JournalIssue></Journal>
    <ArticleTitle>T</ArticleTitle><Abstract><AbstractText Label="A">Body</AbstractText></Abstract></Article></MedlineCitation></PubmedArticle>
    <PubmedArticle><MedlineCitation><PMID>2</PMID><Article><ArticleTitle>No abstract</ArticleTitle></Article></MedlineCitation></PubmedArticle></PubmedArticleSet>"""
    out = parse_pubmed_xml(xml)
    assert [a.pmid for a in out] == ["1"] and out[0].text == "A: Body" and out[0].year == 2021


def test_health_research_uses_approved_chunks_with_citations():
    from app.agents import health_research

    out = health_research.run({"question": "q", "research_chunks": [{"source_id": "s1", "identifier": "PMID:1", "title": "T", "content": "Body", "url": "u"}]})
    assert out["findings"][0].startswith("[R1]") and out["sources"][0]["identifier"] == "PMID:1"
    assert health_research.run({"question": "q"})["limitations"] == [health_research.NOT_CONNECTED]


def test_guidance_and_panel_loinc_items_are_not_proposed():
    def handler(request):
        return httpx.Response(
            200,
            json=[2, ["1-1", "2-2"], {"PROPERTY": ["Find", "-"]},
                  [["1-1", "Guidance for myelography", "Guidance for myelography"], ["2-2", "Myelography panel", "Myelography"]]],
        )

    assert agent.research_biomarker(make_fetcher(handler), "Myelography") is None


def ucum_handler(table):
    def handler(request: httpx.Request) -> httpx.Response:
        parts = request.url.raw_path.decode().split("/")
        value, src, dst = float(parts[-5]), unquote(parts[-3]), unquote(parts[-1])
        fn = table.get((src, dst))
        if fn is None:
            return httpx.Response(200, text="<Response>ERROR: Did you wish to convert between mass and moles?</Response>")
        return httpx.Response(200, text=f"<Response><ResultQuantity>{fn(value)}</ResultQuantity></Response>")

    return handler


def test_conversion_is_derived_from_ucum_with_factor_and_offset():
    f = make_fetcher(ucum_handler({("mg/dL", "g/L"): lambda v: v * 0.01, ("Cel", "[degF]"): lambda v: v * 1.8 + 32}))
    got = agent.research_conversion(f, "mg/dL", "g/L")
    assert got and got["factor"] == pytest.approx(0.01) and got["offset"] == 0
    assert got["evidence"][0]["source"] == "UCUM"
    temp = agent.research_conversion(f, "Cel", "[degF]")
    assert temp and temp["factor"] == pytest.approx(1.8) and temp["offset"] == pytest.approx(32)


def test_conversion_is_not_proposed_when_units_are_incompatible_or_nonlinear():
    f = make_fetcher(ucum_handler({("x", "y"): lambda v: v * v + 1}))
    assert agent.research_conversion(f, "mg/dL", "mmol/L") is None
    assert agent.research_conversion(f, "x", "y") is None
    assert agent.research_conversion(f, "mg/dL", "mg/dL") is None
