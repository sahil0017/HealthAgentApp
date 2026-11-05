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

    return [list_biomarkers, list_flagged_labs, get_trend, list_medications]


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
            available_tools=["list_biomarkers", "list_flagged_labs", "get_trend", "list_medications"],
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
