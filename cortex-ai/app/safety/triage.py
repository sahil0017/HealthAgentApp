import re
from dataclasses import dataclass

EMERGENCY = re.compile(
    r"\b(chest (?:pain|pressure|tightness)|crushing pain|can'?t breathe|cannot breathe|trouble breathing|difficulty breathing|"
    r"shortness of breath|face (?:is )?droop\w*|slurred speech|sudden (?:weakness|numbness|confusion|vision loss)|stroke|heart attack|"
    r"severe bleeding|bleeding (?:heavily|won'?t stop)|coughing (?:up )?blood|vomiting blood|anaphyla\w*|throat (?:is )?closing|"
    r"overdos\w*|took too many|passed out|unconscious|seizure)\b",
    re.IGNORECASE,
)
SELF_HARM = re.compile(
    r"\b(kill myself|end my life|suicid\w*|want to die|hurt myself|self[- ]harm)\b",
    re.IGNORECASE,
)

EMERGENCY_MESSAGE = (
    "Some of what you describe can be a medical emergency. Call your local emergency number or go to the nearest "
    "emergency department now. Do not wait for an app to answer, and do not drive yourself if you feel faint."
)
SELF_HARM_MESSAGE = (
    "You deserve support right now. If you may act on thoughts of harming yourself, call your local emergency number. "
    "In the US you can call or text 988 (Suicide & Crisis Lifeline); elsewhere, contact your local crisis line or a trusted "
    "person who can stay with you."
)


@dataclass(frozen=True)
class Triage:
    level: str  # "none", "emergency" or "crisis"
    message: str = ""

    @property
    def urgent(self) -> bool:
        return self.level != "none"


def assess(question: str) -> Triage:
    if SELF_HARM.search(question):
        return Triage("crisis", SELF_HARM_MESSAGE)
    if EMERGENCY.search(question):
        return Triage("emergency", EMERGENCY_MESSAGE)
    return Triage("none")
