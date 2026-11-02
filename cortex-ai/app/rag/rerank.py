from typing import Protocol


class Reranker(Protocol):
    def score(self, query: str, passages: list[str]) -> list[float]: ...


class CrossEncoderReranker:
    def __init__(self, model_id: str, revision: str, local_files_only: bool = False):
        from sentence_transformers import CrossEncoder

        self._model = CrossEncoder(model_id, revision=revision, local_files_only=local_files_only)

    def score(self, query: str, passages: list[str]) -> list[float]:
        return [float(s) for s in self._model.predict([(query, passage) for passage in passages], batch_size=16)]
