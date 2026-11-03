import logging
import operator
import re
from collections.abc import Callable
from typing import Annotated, Any, TypedDict

from langgraph.graph import END, START, StateGraph

from app.agents import health_research, lab_intelligence, medical_records
from app.rag.retriever import Retriever
from app.safety import assess, check_explanation, has_directive

logger = logging.getLogger(__name__)

Explainer = Callable[[str, str], str | None]

LAB_TERMS = re.compile(
    r"\b(lab|labs|test|tests|result|results|level|levels|value|values|changed|compare|increase|increased|decrease|decreased|a1c|hba1c|glucose|cholesterol)\b"
)
RESEARCH_TERMS = re.compile(r"\b(research|study|studies|evidence|guideline|guidelines|trial|trials|publication|publications)\b")
STANDARD_LIMITATION = "This educational prototype does not provide diagnosis or treatment advice."
CLINICIAN_LIMITATION = (
    "Questions about diagnosis, medication or dosing need a clinician who knows your history; "
    "this answer only summarises what is in your records."
)


class HealthQuestionState(TypedDict, total=False):
    question: str
    observations: list[dict[str, Any]]
    record_chunks: list[dict[str, Any]]
    research_chunks: list[dict[str, Any]]
    domains: list[str]
    findings: Annotated[list[str], operator.add]
    sources: Annotated[list[dict[str, str]], operator.add]
    limitations: Annotated[list[str], operator.add]
    answer: str
    urgent: bool
    triage: str
    confidence: str
    uncertainty: str
    suggested_questions: list[str]


def triage_node(state: HealthQuestionState) -> dict[str, Any]:
    result = assess(state["question"])
    return {"urgent": result.urgent, "triage": result.level}


def _route_after_triage(state: HealthQuestionState) -> str:
    return "urgent" if state.get("urgent") else "orchestrate"


def urgent_response(state: HealthQuestionState) -> dict[str, Any]:
    result = assess(state["question"])
    return {
        "answer": result.message,
        "domains": [],
        "confidence": "not_applicable",
        "uncertainty": "CORTEX did not search your records because this may need urgent human help.",
        "limitations": [STANDARD_LIMITATION],
        "suggested_questions": [],
    }


def assess_confidence(state: HealthQuestionState, explanation_used: bool) -> tuple[str, str]:
    records = [s for s in state.get("sources", []) if s.get("type") == "user_record"]
    research = [s for s in state.get("sources", []) if s.get("type") == "research_source"]
    if not records and not research:
        return "insufficient", "There is no approved record or research source behind this answer, so treat it as general information only."
    if records and len(state.get("observations", [])) >= 2 and not any("not compared" in item or "not enough" in item for item in state.get("limitations", [])):
        note = "Based on your approved records; confirm anything important with your clinician."
        if explanation_used:
            note += " The plain-language explanation is AI-generated."
        return "high", note
    return "limited", "Only part of the question could be tied to a source. Missing or undated results limit what can be concluded."


def suggest_questions(state: HealthQuestionState) -> list[str]:
    names: dict[str, int] = {}
    for o in state.get("observations", []):
        names[o["name"]] = names.get(o["name"], 0) + 1
    suggestions = [f"How has my {name} changed over time?" for name, count in sorted(names.items()) if count >= 2][:2]
    if names and not suggestions:
        suggestions.append(f"What does my latest {sorted(names)[0]} result mean?")
    if not names:
        suggestions.append("What records should I upload to get useful answers?")
    return suggestions[:3]


def orchestrate(state: HealthQuestionState) -> dict[str, Any]:
    question = state["question"].lower()
    domains = ["medical_records"]
    if state["observations"] or LAB_TERMS.search(question):
        domains.append("lab_intelligence")
    # Retrieved approved sources decide relevance; the keyword match only surfaces the "no sources" limitation.
    if state.get("research_chunks") or RESEARCH_TERMS.search(question):
        domains.append("health_research")
    extra: dict[str, Any] = {}
    if has_directive(state["question"]):
        extra["limitations"] = [CLINICIAN_LIMITATION]
    return {"domains": domains, **extra}


def _specialist(name: str, run: Callable[[HealthQuestionState], dict[str, Any]]):
    def node(state: HealthQuestionState) -> dict[str, Any]:
        if name not in state["domains"]:
            return {}
        return run(state)

    return node


def _synthesizer(explain: Explainer):
    def synthesize(state: HealthQuestionState) -> dict[str, Any]:
        question = state["question"]
        facts = "\n".join(state.get("findings", []))
        answer = f"You asked: {question}\n\n{facts}"
        limitations: list[str] = [STANDARD_LIMITATION]
        try:
            explanation = explain(facts, question)
        except Exception:
            logger.exception("explanation model failed")
            explanation = None
            limitations.append("The AI explanation model is unavailable; showing the recorded facts only.")
        if explanation:
            policy = check_explanation(explanation, facts)
            if not policy.passed:
                logger.warning("explanation withheld: %s", "; ".join(policy.violations))
                limitations.append(
                    "The AI explanation was withheld because it did not pass safety checks; showing the recorded facts only."
                )
                explanation = None
        if explanation:
            answer += (
                "\n\nPlain-language explanation (AI-generated; check it against the facts above):\n"
                f"{explanation}"
            )
        confidence, uncertainty = assess_confidence({**state, "limitations": state.get("limitations", [])}, bool(explanation))
        return {
            "answer": answer,
            "limitations": limitations,
            "confidence": confidence,
            "uncertainty": uncertainty,
            "suggested_questions": suggest_questions(state),
        }

    return synthesize


def build_health_question_graph(explain: Explainer = lambda facts, question: None, retriever: Retriever | None = None):
    graph = StateGraph(HealthQuestionState)
    graph.add_node("triage", triage_node)
    graph.add_node("urgent", urgent_response)
    graph.add_node("orchestrate", orchestrate)
    graph.add_node("medical_records", _specialist("medical_records", medical_records.run))
    graph.add_node("lab_intelligence", _specialist("lab_intelligence", lab_intelligence.run))
    graph.add_node("health_research", _specialist("health_research", lambda state: health_research.run(state, retriever)))
    graph.add_node("synthesize", _synthesizer(explain))
    graph.add_edge(START, "triage")
    graph.add_conditional_edges("triage", _route_after_triage, {"urgent": "urgent", "orchestrate": "orchestrate"})
    graph.add_edge("urgent", END)
    graph.add_edge("orchestrate", "medical_records")
    graph.add_edge("medical_records", "lab_intelligence")
    graph.add_edge("lab_intelligence", "health_research")
    graph.add_edge("health_research", "synthesize")
    graph.add_edge("synthesize", END)
    return graph.compile()
