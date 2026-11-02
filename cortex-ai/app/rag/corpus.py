from pathlib import Path
from typing import Literal

from pydantic import BaseModel, Field, ValidationError

SourceType = Literal[
    "peer_reviewed_study",
    "systematic_review",
    "clinical_guideline",
    "government_health_resource",
    "preprint",
    "general_health_resource",
]


class ResearchSource(BaseModel):
    source_id: str = Field(min_length=1, max_length=120)
    title: str = Field(min_length=1, max_length=500)
    text: str = Field(min_length=1, max_length=200_000)
    source_type: SourceType
    verification_status: Literal["verified", "unverified"]
    identifier: str | None = Field(default=None, max_length=200, description="DOI, PMID or other identifier")


def load_corpus(path: Path) -> list[ResearchSource]:
    sources: list[ResearchSource] = []
    with path.open(encoding="utf-8") as handle:
        for line_number, line in enumerate(handle, start=1):
            if not line.strip():
                continue
            try:
                sources.append(ResearchSource.model_validate_json(line))
            except ValidationError as exc:
                raise ValueError(f"invalid research source on line {line_number}: {exc.errors()[0]['msg']}") from exc
    identifiers = [source.source_id for source in sources]
    if len(identifiers) != len(set(identifiers)):
        raise ValueError("research source ids must be unique")
    return sources
