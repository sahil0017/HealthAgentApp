from pydantic import BaseModel, Field

DEPLOYABLE_STATUSES = frozenset({"evaluated", "approved"})


class ModelRecord(BaseModel):
    model_id: str
    provider: str
    repository: str
    revision: str
    task: str
    deployment: str
    quantization: str | None = None
    license: str
    intended_use: str
    evaluation_status: str = Field(description="research, evaluated, approved, or retired")


# Candidates are deliberately not registered until license, revision and evaluation are reviewed.
REGISTRY: dict[str, ModelRecord] = {}


def get_deployable_model(model_id: str) -> ModelRecord | None:
    record = REGISTRY.get(model_id)
    if record is None or record.evaluation_status not in DEPLOYABLE_STATUSES:
        return None
    return record
