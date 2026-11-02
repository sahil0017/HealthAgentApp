import logging
import operator
import re
from collections.abc import Callable
from typing import Annotated, Any, TypedDict

from langgraph.graph import END, START, StateGraph

from app.agents import health_research, lab_intelligence, medical_records
from app.rag.retriever import Retriever
from app.safety import check_explanation

logger = logging.getLogger(__name__)

Explainer = Callable[[str, str], str | None]

LAB_TERMS = re.compile(
    r"\b(lab|labs|test|tests|result|results|level|levels|value|values|changed|compare|increase|increased|decrease|decreased|a1c|hba1c|glucose|cholesterol)\b"
)
RESEARCH_TERMS = re.compile(r"\b(research|study|studies|evidence|guideline|guidelines|trial|trials|publication|publications)\b")
STANDARD_LIMITATION = "This educational prototype does not provide diagnosis or treatment advice."


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


def orchestrate(state: HealthQuestionState) -> dict[str, Any]:
    question = state["question"].lower()
    domains = ["medical_records"]
    if state["observations"] or LAB_TERMS.search(question):
        domains.append("lab_intelligence")
    if RESEARCH_TERMS.search(question):
        domains.append("health_research")
    return {"domains": domains}


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
        return {"answer": answer, "limitations": limitations}

    return synthesize


def build_health_question_graph(explain: Explainer = lambda facts, question: None, retriever: Retriever | None = None):
    graph = StateGraph(HealthQuestionState)
    graph.add_node("orchestrate", orchestrate)
    graph.add_node("medical_records", _specialist("medical_records", medical_records.run))
    graph.add_node("lab_intelligence", _specialist("lab_intelligence", lab_intelligence.run))
    graph.add_node("health_research", _specialist("health_research", lambda state: health_research.run(state, retriever)))
    graph.add_node("synthesize", _synthesizer(explain))
    graph.add_edge(START, "orchestrate")
    graph.add_edge("orchestrate", "medical_records")
    graph.add_edge("medical_records", "lab_intelligence")
    graph.add_edge("lab_intelligence", "health_research")
    graph.add_edge("health_research", "synthesize")
    graph.add_edge("synthesize", END)
    return graph.compile()
