"""GitHub Copilot SDK agent for the health assistant and the prescription reader.

The Copilot runtime normally ships with shell, file and web tools. Both sessions here run in "empty" mode with
every built-in tool removed and any other permission request rejected, so the model can only call the tools
defined in this file. Those tools read a snapshot the API sends with the request, so the model never gets a
connection to the database and sees only what it asks for.
"""

import asyncio
import base64
import tempfile
from typing import Any

from pydantic import BaseModel, Field

from app import config


class CopilotUnavailable(Exception):
    """No token is configured, the runtime is missing, or the model could not answer."""


def configured() -> bool:
    return bool(config.COPILOT_TOKEN)


_client = None
_client_lock = asyncio.Lock()


async def _get_client():
    """One long-lived runtime process shared by all requests."""
    global _client
    if _client is not None:
        return _client
    async with _client_lock:
        if _client is None:
            try:
                from copilot import CopilotClient

                client = CopilotClient(
                    mode="empty",
                    base_directory=tempfile.mkdtemp(prefix="cortex-copilot-"),
                    github_token=config.COPILOT_TOKEN,
                )
                await client.start()
            except Exception as exc:
                raise CopilotUnavailable("runtime_unavailable") from exc
            _client = client
    return _client


async def _reject_everything_else(request, invocation):
    from copilot.rpc import PermissionDecisionApproveOnce, PermissionDecisionReject

    kind = getattr(request, "kind", "")
    if str(getattr(kind, "value", kind)) == "custom-tool":
        return PermissionDecisionApproveOnce()
    return PermissionDecisionReject(feedback="Only the provided record tools are allowed.")


class _NameArgs(BaseModel):
    name: str = Field(description="Biomarker name, for example HbA1c or Hemoglobin")


class _NoArgs(BaseModel):
    pass


def _find_trend(snapshot: dict, query: str) -> str:
    q = query.strip().lower()
    trends = snapshot.get("trends") or []
    for t in trends:
        if q in (str(t.get("code", "")).lower(), str(t.get("name", "")).lower()):
            return t.get("out", "")
    for t in trends:
        n = str(t.get("name", "")).lower()
        if n and (q in n or n in q):
            return t.get("out", "")
    return f'There is no verified result named "{query}" in the record.'


def _find_report(snapshot: dict, query: str) -> str:
    q = query.strip().lower()
    reports = snapshot.get("reports") or []
    if q in ("latest", "newest", "last", "most recent", "recent") and reports:
        return reports[0].get("out", "")
    if q in ("previous", "earlier", "older", "before") and len(reports) > 1:
        return reports[1].get("out", "")
    for r in reports:
        if q == str(r.get("id", "")).lower():
            return r.get("out", "")
    for r in reports:
        title = str(r.get("title", "")).lower()
        if q and ((title and (q in title or title in q)) or q in str(r.get("date", ""))):
            return r.get("out", "")
    return f'There is no report matching "{query}". ' + str(snapshot.get("list_reports", ""))


def _record_tools(snapshot: dict, calls: list[dict]):
    from copilot import define_tool

    @define_tool(name="list_biomarkers", description="List the names of the biomarkers in the person's record.", skip_permission=True)
    async def list_biomarkers(params: _NoArgs) -> str:
        calls.append({"tool": "list_biomarkers", "args": {}})
        return snapshot.get("list_biomarkers", "")

    @define_tool(
        name="list_flagged_labs",
        description="List the latest results that are outside their reference range, with recent readings.",
        skip_permission=True,
    )
    async def list_flagged_labs(params: _NoArgs) -> str:
        calls.append({"tool": "list_flagged_labs", "args": {}})
        return snapshot.get("list_flagged_labs", "")

    @define_tool(
        name="get_trend",
        description="Get the latest value, reference range and recent readings for one biomarker.",
        skip_permission=True,
    )
    async def get_trend(params: _NameArgs) -> str:
        calls.append({"tool": "get_trend", "args": {"name": params.name}})
        return _find_trend(snapshot, params.name)

    @define_tool(
        name="list_medications",
        description="List the medicines on the person's saved prescriptions with their daily schedule.",
        skip_permission=True,
    )
    async def list_medications(params: _NoArgs) -> str:
        calls.append({"tool": "list_medications", "args": {}})
        return snapshot.get("list_medications", "")

    @define_tool(
        name="get_care_plan",
        description="Get open follow-ups (with due dates), symptoms still active and reports waiting for review.",
        skip_permission=True,
    )
    async def get_care_plan(params: _NoArgs) -> str:
        calls.append({"tool": "get_care_plan", "args": {}})
        return snapshot.get("get_care_plan", "")

    @define_tool(
        name="list_reports",
        description="List the uploaded lab reports with lab, date and how many results are outside range.",
        skip_permission=True,
    )
    async def list_reports(params: _NoArgs) -> str:
        calls.append({"tool": "list_reports", "args": {}})
        return snapshot.get("list_reports", "")

    @define_tool(
        name="get_report",
        description="Get every result in one report with the previous value of each test. Name can be a report id, lab name, date, 'latest' or 'previous'.",
        skip_permission=True,
    )
    async def get_report(params: _NameArgs) -> str:
        calls.append({"tool": "get_report", "args": {"name": params.name}})
        return _find_report(snapshot, params.name)

    return [list_biomarkers, list_flagged_labs, get_trend, list_medications, get_care_plan, list_reports, get_report]


async def _send(session, prompt: str, attachments: list[dict] | None = None) -> str:
    kwargs: dict[str, Any] = {}
    if attachments:
        kwargs["attachments"] = attachments
    try:
        event = await asyncio.wait_for(session.send_and_wait(prompt, **kwargs), timeout=config.COPILOT_TIMEOUT_SECONDS)
    except Exception as exc:
        raise CopilotUnavailable("model_failed") from exc
    return (getattr(getattr(event, "data", None), "content", "") or "").strip() if event else ""


def _transcript(history: list[dict], message: str) -> str:
    lines = []
    for turn in history[-6:]:
        who = "Person" if turn.get("role") == "user" else "Assistant"
        lines.append(f"{who}: {str(turn.get('content', ''))[:1200]}")
    lines.append(f"Person: {message}")
    return "\n".join(lines)


async def run_agent(message: str, system: str, history: list[dict], snapshot: dict) -> dict:
    """Run one tool-using turn and return the answer and the tool calls the model made."""
    if not configured():
        raise CopilotUnavailable("model_not_configured")
    client = await _get_client()
    calls: list[dict] = []
    try:
        session = await client.create_session(
            on_permission_request=_reject_everything_else,
            model=config.COPILOT_MODEL,
            tools=_record_tools(snapshot, calls),
            available_tools=["list_biomarkers", "list_flagged_labs", "get_trend", "list_medications", "get_care_plan", "list_reports", "get_report"],
            system_message={"mode": "replace", "content": system},
        )
    except Exception as exc:
        raise CopilotUnavailable("session_failed") from exc
    try:
        answer = await _send(session, _transcript(history, message))
    finally:
        try:
            await session.disconnect()
        except Exception:
            pass
    return {"answer": answer, "calls": calls[:8], "model": config.COPILOT_MODEL}


class Medicine(BaseModel):
    name: str = Field(description="Medicine name exactly as written (brand or generic). Do not correct or guess.")
    strength: str = Field(default="", description="Strength as written, e.g. 500 mg. Empty if not written.")
    form: str = Field(default="", description="Tablet, capsule, syrup, injection, drops, etc., as written or abbreviated (Tab, Cap, Syp).")
    dose: str = Field(default="", description="How much per time, as written, e.g. 1 tab, 5 ml.")
    frequency: str = Field(default="", description="How often, exactly as written: 1-0-1, BD, TDS, twice daily, HS, SOS...")
    timing: str = Field(default="", description="Relation to food or time of day, as written: after food, empty stomach, at night...")
    duration: str = Field(default="", description="How long, as written, e.g. 5 days, 1 month.")
    instructions: str = Field(default="", description="Any other note written for this medicine.")
    confidence: float = Field(description="0 to 1: how sure you are you read this line correctly. Use under 0.6 when handwriting is unclear.")


class PrescriptionReading(BaseModel):
    medicines: list[Medicine] = Field(description="Every medicine line on the prescription, in order.")
    doctor: str = Field(default="", description="Doctor name if printed or written.")
    date: str = Field(default="", description="Prescription date as written.")
    unreadable: list[str] = Field(default_factory=list, description="Parts you could not read; describe where they are.")
    is_prescription: bool = Field(description="False if the image is not a medical prescription.")


_READ_SYSTEM = (
    "You transcribe prescriptions. Copy each medicine exactly as written. Never guess a name you cannot read: "
    "include the line, give your best reading, and set a low confidence. Never add medicines that are not on the page. "
    "Leave a field empty when it is not written; never infer a dose, timing or duration. Do not give advice. Call the submit_prescription tool exactly once with what you read."
)


async def read_prescription(image: bytes, mime: str) -> dict:
    """Read a prescription photo into structured medicines. Nothing is saved; the person confirms every line."""
    if not configured():
        raise CopilotUnavailable("model_not_configured")
    client = await _get_client()
    from copilot import define_tool

    captured: dict[str, Any] = {}

    @define_tool(name="submit_prescription", description="Submit the transcribed prescription.", skip_permission=True)
    async def submit_prescription(params: PrescriptionReading) -> str:
        captured.update(params.model_dump())
        return "Received."

    try:
        session = await client.create_session(
            on_permission_request=_reject_everything_else,
            model=config.COPILOT_VISION_MODEL,
            tools=[submit_prescription],
            available_tools=["submit_prescription"],
            system_message={"mode": "replace", "content": _READ_SYSTEM},
        )
    except Exception as exc:
        raise CopilotUnavailable("session_failed") from exc
    try:
        await _send(
            session,
            "Transcribe this prescription and submit it.",
            attachments=[{"type": "blob", "data": base64.b64encode(image).decode("ascii"), "mimeType": mime}],
        )
    finally:
        try:
            await session.disconnect()
        except Exception:
            pass
    if not captured:
        raise CopilotUnavailable("no_reading")
    captured["model"] = config.COPILOT_VISION_MODEL
    return captured


class LabName(BaseModel):
    label: str = Field(description="The test name exactly as given in the input.")
    name: str = Field(description="Standard clinical name, e.g. 'Total Cholesterol', 'Platelet Count', 'Vitamin B12', 'ESR'.")
    is_test: bool = Field(description="False when the label is not a measured test: reference-range wording (More than 40, Optimal, Desirable), a heading, a note or a fragment. True otherwise.")
    panel: str = Field(description="Standard panel: Complete Blood Count, Lipid Profile, Liver Function, Kidney Function, Thyroid Profile, Glycaemic Control, Vitamins, Electrolytes, Iron Studies, Hormones, Inflammation, Urine, Other.")


class LabNames(BaseModel):
    tests: list[LabName]


_STANDARDISE_SYSTEM = (
    "You map laboratory test labels printed on lab reports to their standard clinical names and panels. "
    "You receive only the labels, never values or identity. Keep abbreviations only when they are the standard name (ESR, TSH, HbA1c). "
    "Do not invent tests, do not merge different tests, and return one entry per input label. "
    "Each label may come with the report line it was read from; use it to expand abbreviations and to spot labels that are really reference-range wording or headings (set is_test false for those). "
    "Call the submit_names tool exactly once."
)


async def standardise_labs(labels: list[str], context: dict[str, str] | None = None) -> dict:
    """Map raw lab-report labels to standard names and panels. Labels only; no values or identity."""
    if not configured():
        raise CopilotUnavailable("model_not_configured")
    client = await _get_client()
    from copilot import define_tool

    captured: dict[str, Any] = {}

    @define_tool(name="submit_names", description="Submit the standard names.", skip_permission=True)
    async def submit_names(params: LabNames) -> str:
        captured.update(params.model_dump())
        return "Received."

    try:
        session = await client.create_session(
            on_permission_request=_reject_everything_else,
            model=config.COPILOT_MODEL,
            tools=[submit_names],
            available_tools=["submit_names"],
            system_message={"mode": "replace", "content": _STANDARDISE_SYSTEM},
        )
    except Exception as exc:
        raise CopilotUnavailable("session_failed") from exc
    try:
        context = context or {}
        lines = [f"- {label}" + (f"   [report line: {context[label][:120]}]" if context.get(label) else "") for label in labels]
        await _send(session, "Labels:\n" + "\n".join(lines))
    finally:
        try:
            await session.disconnect()
        except Exception:
            pass
    wanted = set(labels)
    tests = [t for t in captured.get("tests", []) if t.get("label") in wanted and t.get("name", "").strip()]
    if not tests:
        raise CopilotUnavailable("no_names")
    return {"tests": tests, "model": config.COPILOT_MODEL}


class ExtractedTest(BaseModel):
    name: str = Field(description="Test name exactly as printed in the report.")
    value: str = Field(description="The numeric result exactly as printed, e.g. '13.2' or '0.85'. Digits only, no unit.")
    unit: str = Field(default="", description="Unit exactly as printed, e.g. 'g/dL', 'mg/dL', 'x10^3/uL'. Empty if none printed.")
    reference_range: str = Field(default="", description="The printed reference or normal range for this test, as written. Empty if none.")
    panel: str = Field(default="", description="Standard panel: Complete Blood Count, Lipid Profile, Liver Function, Kidney Function, Thyroid Profile, Glycaemic Control, Vitamins, Electrolytes, Iron Studies, Hormones, Inflammation, Urine, Other.")


class ExtractedMedicine(BaseModel):
    name: str = Field(description="Medicine name exactly as written.")
    strength: str = Field(default="")
    form: str = Field(default="")
    dose: str = Field(default="")
    frequency: str = Field(default="", description="Exactly as written: 1-0-1, BD, TDS, twice daily...")
    timing: str = Field(default="")
    duration: str = Field(default="")
    instructions: str = Field(default="")


class DocumentExtraction(BaseModel):
    document_type: str = Field(description="One of LAB_REPORT, PRESCRIPTION, DISCHARGE_SUMMARY, RADIOLOGY_REPORT, VACCINATION_RECORD, MEDICAL_CERTIFICATE, OTHER.")
    provider: str = Field(default="", description="Laboratory, clinic or hospital that issued it, if printed.")
    report_date: str = Field(default="", description="Date of sample collection or report as YYYY-MM-DD, or empty.")
    doctor: str = Field(default="", description="Prescribing doctor if this is a prescription.")
    tests: list[ExtractedTest] = Field(default_factory=list)
    medicines: list[ExtractedMedicine] = Field(default_factory=list)


_EXTRACT_SYSTEM = (
    "You extract structured data from the text of a medical document, which may come from any lab "
    "(Orange Health, Thyrocare, Dr Lal PathLabs, Metropolis, Apollo, Tata 1mg, SRL and others), a clinic or a hospital, "
    "in any layout, including tables whose columns were flattened onto separate lines and OCR text with errors. "
    "Copy every test result and every prescribed medicine exactly as printed. Never invent, estimate or correct a value, "
    "never convert units, and never include a test that has no numeric result. Skip patient identity, addresses, barcodes, page headers and footers, "
    "method names and interpretive notes. If the same test appears twice, keep both. "
    "Leave a field empty when it is not printed. Call the submit_document tool exactly once."
)


async def extract_document(text: str) -> dict:
    """Structured reading of one document's text. The caller removes identity details before sending."""
    if not configured():
        raise CopilotUnavailable("model_not_configured")
    client = await _get_client()
    from copilot import define_tool

    captured: dict[str, Any] = {}

    @define_tool(name="submit_document", description="Submit the extracted document.", skip_permission=True)
    async def submit_document(params: DocumentExtraction) -> str:
        captured.update(params.model_dump())
        return "Received."

    try:
        session = await client.create_session(
            on_permission_request=_reject_everything_else,
            model=config.COPILOT_MODEL,
            tools=[submit_document],
            available_tools=["submit_document"],
            system_message={"mode": "replace", "content": _EXTRACT_SYSTEM},
        )
    except Exception as exc:
        raise CopilotUnavailable("session_failed") from exc
    try:
        await _send(session, "Document text:\n\n" + text)
    finally:
        try:
            await session.disconnect()
        except Exception:
            pass
    if not captured:
        raise CopilotUnavailable("no_extraction")
    captured["model"] = config.COPILOT_MODEL
    return captured


class InsightItem(BaseModel):
    title: str = Field(description="A short heading, at most 9 words.")
    detail: str = Field(description="One or two plain sentences that explain it and say what to do.")
    tone: str = Field(description="good, watch, act or info.")


class Insights(BaseModel):
    headline: str = Field(description="One confident sentence that sums up the whole input.")
    items: list[InsightItem]


_INSIGHT_PROMPTS = {
    "research": (
        "You help a person prepare to understand their own health results. From the facts, write the 6 most useful "
        "questions they should research or ask their doctor. Each title is the question; the detail says why it matters for them, citing their own numbers."
    ),
    "lifestyle": (
        "You are a lifestyle coach working from the person's own check-ins and symptoms. Give 4 to 6 specific changes "
        "for the next 7 days, each tied to a number or pattern in the facts. Use tone good for what is working, watch or act for what needs change."
    ),
    "history": (
        "You read a person's lab results and list what they say about the person's medical history: conditions or risks "
        "the results point to, and what is stable. Each item is one finding with the tests behind it. Say 'points to' or 'consistent with', never diagnose. "
        "If the facts list 'Known history', do not repeat anything already there, even worded differently, and never list the same finding twice."
    ),
    "visit": (
        "You prepare a person with no medical background for a doctor visit, from their own record. The headline is one plain sentence on what "
        "the doctor most needs to know. Give 4 to 6 items, each a plain-language point: say what a test measures in a few words instead of using jargon, "
        "cite their own number, and say why it is worth mentioning. Use tone act for the most important, watch for things to keep an eye on, good for reassuring facts. "
        "Never diagnose or suggest treatment."
    ),
    "upload": (
        "You summarise one uploaded medical report. The headline gives the overall picture. Items cover the weak spots "
        "(out-of-range results grouped by system), what is healthy, and the best next steps."
    ),
    "biomarker": (
        "You explain one lab test to the person who took it. The headline says where they stand in plain words. Items cover: "
        "what this test measures, what their number and trend mean, common reasons it moves in this direction, "
        "and what to do next (food, habits, retest timing, when to see a doctor)."
    ),
    "labs": (
        "You summarise a person's whole lab history across many uploads. The headline gives the overall picture. Items cover "
        "the biggest weaknesses, what is improving or getting worse over time, and what to do next."
    ),
}

_INSIGHT_RULES = (
    " Speak directly to the person. Be specific, practical and confident. Use only the facts given, never invent numbers, "
    "and do not prescribe medicines or doses. Indian food, climate and care context is relevant. Call the submit_insights tool exactly once."
)


async def build_insights(kind: str, facts: str) -> dict:
    """Structured coaching from facts the caller has already stripped of identity."""
    prompt = _INSIGHT_PROMPTS.get(kind)
    if prompt is None:
        raise CopilotUnavailable("unknown_kind")
    if not configured():
        raise CopilotUnavailable("model_not_configured")
    client = await _get_client()
    from copilot import define_tool

    captured: dict[str, Any] = {}

    @define_tool(name="submit_insights", description="Submit the insights.", skip_permission=True)
    async def submit_insights(params: Insights) -> str:
        captured.update(params.model_dump())
        return "Received."

    try:
        session = await client.create_session(
            on_permission_request=_reject_everything_else,
            model=config.COPILOT_MODEL,
            tools=[submit_insights],
            available_tools=["submit_insights"],
            system_message={"mode": "replace", "content": prompt + _INSIGHT_RULES},
        )
    except Exception as exc:
        raise CopilotUnavailable("session_failed") from exc
    try:
        await _send(session, "Facts:\n" + facts)
    finally:
        try:
            await session.disconnect()
        except Exception:
            pass
    items = [i for i in captured.get("items", []) if i.get("title", "").strip()]
    if not items:
        raise CopilotUnavailable("no_insights")
    tones = {"good", "watch", "act", "info"}
    for i in items:
        if i.get("tone") not in tones:
            i["tone"] = "info"
    return {"headline": captured.get("headline", ""), "items": items[:8], "model": config.COPILOT_MODEL}


class _Queries(BaseModel):
    queries: list[str] = Field(description="Two or three short literature search queries (3 to 8 words each) using medical terms.")


class _Verdict(BaseModel):
    index: int = Field(description="Index of the candidate.")
    relevant: bool = Field(description="True only if the candidate directly helps answer the question.")
    summary: str = Field(description="One or two plain-language sentences on what this source says about the question, using only its text.")


class _Verdicts(BaseModel):
    verdicts: list[_Verdict]


async def _submit(system: str, prompt: str, model_cls, tool_name: str) -> dict:
    if not configured():
        raise CopilotUnavailable("model_not_configured")
    client = await _get_client()
    from copilot import define_tool

    captured: dict[str, Any] = {}

    @define_tool(name=tool_name, description="Submit the result.", skip_permission=True)
    async def submit(params: model_cls) -> str:
        captured.update(params.model_dump())
        return "Received."

    try:
        session = await client.create_session(
            on_permission_request=_reject_everything_else,
            model=config.COPILOT_MODEL,
            tools=[submit],
            available_tools=[tool_name],
            system_message={"mode": "replace", "content": system},
        )
    except Exception as exc:
        raise CopilotUnavailable("session_failed") from exc
    try:
        await _send(session, prompt)
    finally:
        try:
            await session.disconnect()
        except Exception:
            pass
    if not captured:
        raise CopilotUnavailable("no_result")
    return captured


async def plan_queries(question: str) -> list[str]:
    out = await _submit(
        "You turn a person's health question into literature search queries. Call submit_queries exactly once.",
        f"Question: {question}", _Queries, "submit_queries",
    )
    return [q.strip() for q in out.get("queries", []) if q.strip()][:3]


async def judge_sources(question: str, candidates: list[dict]) -> list[dict]:
    listing = "\n\n".join(f"[{i}] {c['title']} ({c['publisher']})\n{c['text'][:900]}" for i, c in enumerate(candidates))
    out = await _submit(
        "You pick the sources that help answer a person's health question and explain each in plain language, "
        "using only the text given. Never add facts, numbers or advice that are not in the text. Call submit_verdicts exactly once.",
        f"Question: {question}\n\nCandidates:\n{listing}", _Verdicts, "submit_verdicts",
    )
    return out.get("verdicts", [])
