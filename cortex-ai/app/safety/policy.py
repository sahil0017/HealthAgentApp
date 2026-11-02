import re
from dataclasses import dataclass, field

NUMBER = re.compile(r"\d+(?:\.\d+)?")
CITATION = re.compile(r"\[(R\d+)\]")
DIRECTIVE = re.compile(
    r"\b(you have|you are diagnosed|diagnos(?:is|es|ed|e)|you should (?:take|stop|start|increase|reduce)|"
    r"prescri\w*|take \d+\s*(?:mg|mcg|g|ml)|dosage|dose of|cure|treatment plan)\b",
    re.IGNORECASE,
)


@dataclass(frozen=True)
class PolicyResult:
    violations: list[str] = field(default_factory=list)

    @property
    def passed(self) -> bool:
        return not self.violations


def has_directive(text: str) -> bool:
    return bool(DIRECTIVE.search(text))


def check_explanation(text: str, facts: str) -> PolicyResult:
    violations: list[str] = []
    fact_numbers = set(NUMBER.findall(facts))
    unsupported = sorted({n for n in NUMBER.findall(text) if n not in fact_numbers})
    if unsupported:
        violations.append(f"numbers not present in the recorded facts: {', '.join(unsupported[:5])}")
    allowed_citations = set(CITATION.findall(facts))
    unknown = sorted(set(CITATION.findall(text)) - allowed_citations)
    if unknown:
        violations.append(f"citations do not match retrieved sources: {', '.join(unknown[:5])}")
    if has_directive(text):
        violations.append("diagnosis or treatment language")
    return PolicyResult(violations)
