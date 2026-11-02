from collections import defaultdict
from typing import Any


def run(state: dict[str, Any]) -> dict[str, Any]:
    if not state["observations"]:
        return {}
    by_name: dict[str, list[dict[str, Any]]] = defaultdict(list)
    for observation in state["observations"]:
        by_name[observation["name"]].append(observation)

    findings: list[str] = []
    limitations: list[str] = []
    for name, items in sorted(by_name.items()):
        dated = sorted(items, key=lambda o: o.get("observed_at") or "")
        if len(dated) < 2:
            continue
        previous, latest = dated[-2], dated[-1]
        if previous["unit"] != latest["unit"]:
            limitations.append(
                f"{name} values use different units ({previous['unit']} and {latest['unit']}), so they were not compared."
            )
            continue
        change = latest["value"] - previous["value"]
        direction = "increased" if change > 0 else "decreased" if change < 0 else "did not change"
        findings.append(
            f"{name} {direction} from {previous['value']:g} to {latest['value']:g} {latest['unit']} "
            "between the two most recent recorded values."
        )
    if not findings and not limitations:
        limitations.append("There are not enough dated results to compare changes over time.")
    return {"findings": findings, "limitations": limitations}
