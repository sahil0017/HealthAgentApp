import argparse
import json
import statistics
import time
from dataclasses import asdict, dataclass
from pathlib import Path
from typing import Callable

from app.extraction.biomarkers import extract_candidates
from app.safety import has_directive
from app.workflows.health_question import build_health_question_graph

DATA = Path(__file__).parent / "data"


@dataclass
class Report:
    explainer: str
    extraction_precision: float
    extraction_recall: float
    answers: int
    explanations_produced: int
    explanations_withheld: int
    explanation_latency_p50_s: float | None
    explanation_latency_p95_s: float | None
    checks_passed: int
    checks_total: int
    failures: list[str]


def load_jsonl(name: str) -> list[dict]:
    return [json.loads(line) for line in (DATA / name).read_text(encoding="utf-8").splitlines() if line.strip()]


def evaluate_extraction() -> tuple[float, float]:
    predicted_total = expected_total = matched = 0
    for case in load_jsonl("extraction_cases.jsonl"):
        candidates, _ = extract_candidates([(1, case["text"])])
        predicted = {(c.code, c.value, c.unit) for c in candidates}
        expected = {(e["code"], e["value"], e["unit"]) for e in case["expected"]}
        predicted_total += len(predicted)
        expected_total += len(expected)
        matched += len(predicted & expected)
    precision = matched / predicted_total if predicted_total else 1.0
    recall = matched / expected_total if expected_total else 1.0
    return round(precision, 3), round(recall, 3)


def evaluate_answers(explain: Callable[[str, str], str | None]) -> dict:
    latencies: list[float] = []

    def timed(facts: str, question: str) -> str | None:
        started = time.perf_counter()
        output = explain(facts, question)
        latencies.append(time.perf_counter() - started)
        return output

    graph = build_health_question_graph(explain=timed)
    produced = withheld = checks_passed = checks_total = 0
    failures: list[str] = []
    cases = load_jsonl("answer_cases.jsonl")
    for case in cases:
        result = graph.invoke({"question": case["question"], "observations": case["observations"]})
        answer = result["answer"]
        expect = case["expect"]
        if "explanation" in answer.lower() and "AI-generated" in answer:
            produced += 1
        if any("withheld" in item for item in result["limitations"]):
            withheld += 1
        if "comparison_stated" in expect:
            checks_total += 1
            stated = "increased" in answer or "decreased" in answer or "did not change" in answer
            if stated == expect["comparison_stated"]:
                checks_passed += 1
            else:
                failures.append(f"{case['id']}: comparison_stated expected {expect['comparison_stated']}")
        if expect.get("unit_limitation"):
            checks_total += 1
            if any("different units" in item for item in result["limitations"]):
                checks_passed += 1
            else:
                failures.append(f"{case['id']}: unit limitation missing")
        if expect.get("no_directive"):
            checks_total += 1
            if not has_directive(answer):
                checks_passed += 1
            else:
                failures.append(f"{case['id']}: directive language in answer")
    return {
        "answers": len(cases),
        "explanations_produced": produced,
        "explanations_withheld": withheld,
        "latencies": latencies,
        "checks_passed": checks_passed,
        "checks_total": checks_total,
        "failures": failures,
    }


def run(explain: Callable[[str, str], str | None], explainer: str) -> Report:
    precision, recall = evaluate_extraction()
    answers = evaluate_answers(explain)
    latencies = answers.pop("latencies")
    return Report(
        explainer=explainer,
        extraction_precision=precision,
        extraction_recall=recall,
        explanation_latency_p50_s=round(statistics.median(latencies), 3) if latencies else None,
        explanation_latency_p95_s=round(_p95(latencies), 3) if latencies else None,
        **answers,
    )


def _p95(values: list[float]) -> float:
    ordered = sorted(values)
    return ordered[min(len(ordered) - 1, int(round(0.95 * (len(ordered) - 1))))]


def main() -> None:
    parser = argparse.ArgumentParser(description="Evaluate CORTEX AI extraction and answer behavior on synthetic cases.")
    parser.add_argument("--explainer", choices=["none", "model"], default="none")
    parser.add_argument("--repository", default="")
    parser.add_argument("--revision", default="")
    parser.add_argument("--out", type=Path, default=None)
    args = parser.parse_args()
    if args.explainer == "model":
        from functools import partial

        from app.inference.generate import generate_with_record
        from app.inference.registry import ModelRecord

        if not args.repository or not args.revision:
            parser.error("--explainer model requires --repository and --revision")
        record = ModelRecord(
            model_id="evaluation-candidate",
            provider="huggingface",
            repository=args.repository,
            revision=args.revision,
            task="text-generation",
            deployment="local",
            license="unreviewed",
            intended_use="evaluation-only",
            evaluation_status="research",
        )
        explain = partial(generate_with_record, record)
    else:
        explain = lambda facts, question: None  # noqa: E731
    report = asdict(run(explain, args.explainer))
    text = json.dumps(report, indent=2)
    if args.out:
        args.out.write_text(text, encoding="utf-8")
    print(text)


if __name__ == "__main__":
    main()
