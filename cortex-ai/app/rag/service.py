import logging
import math
import threading
import time
from collections import OrderedDict
from typing import Any

from app import config
from app.rag.embeddings import Embedder
from app.rag.hashing import DIMENSIONS, HashingEmbedder

logger = logging.getLogger(__name__)


class EmbeddingUnavailable(RuntimeError):
    pass


class EmbeddingService:
    """Owns the embedding model lifecycle: load once, report readiness, validate output, cache queries in memory only."""

    def __init__(self) -> None:
        self._lock = threading.Lock()
        self._embedder: Embedder | None = None
        self._model_name = ""
        self._state = "not_started"
        self._error = ""
        self._loaded_seconds = 0.0
        self._queries: OrderedDict[str, list[float]] = OrderedDict()
        self._query_lock = threading.Lock()
        self._reranker: Any = None
        self._reranker_error = ""

    @property
    def configured_model(self) -> str:
        if config.EMBEDDING_MODEL_ID:
            return f"{config.EMBEDDING_MODEL_ID}@{config.EMBEDDING_REVISION}"
        return HashingEmbedder.model_id

    def validate_configuration(self) -> None:
        if config.EMBEDDING_REQUIRED and not config.EMBEDDING_MODEL_ID:
            raise RuntimeError("CORTEX_EMBEDDING_MODEL and CORTEX_EMBEDDING_REVISION are required in this environment")

    def load(self) -> None:
        with self._lock:
            if self._state == "ready":
                return
            self._state = "loading"
            started = time.monotonic()
            try:
                self.validate_configuration()
                if config.EMBEDDING_MODEL_ID:
                    from app.rag.embeddings import SentenceTransformerEmbedder

                    embedder: Embedder = SentenceTransformerEmbedder(
                        config.EMBEDDING_MODEL_ID,
                        config.EMBEDDING_REVISION,
                        local_files_only=config.EMBEDDING_OFFLINE,
                        query_prefix=config.EMBEDDING_QUERY_PREFIX,
                        max_seq_length=config.EMBEDDING_MAX_SEQ_LENGTH,
                        batch_size=config.EMBEDDING_BATCH_SIZE,
                        device=config.EMBEDDING_DEVICE,
                    )
                    dimensions = embedder.dimensions  # type: ignore[attr-defined]
                    if dimensions != DIMENSIONS:
                        raise RuntimeError(f"model produces {dimensions} dimensions; the database column is {DIMENSIONS}")
                else:
                    embedder = HashingEmbedder()
                self._embedder = embedder
                self._model_name = self.configured_model
                embedder.embed_queries(["warm up"])
                self._state, self._error = "ready", ""
            except Exception as exc:
                self._state, self._error = "failed", f"{type(exc).__name__}: {exc}"
                logger.exception("embedding model failed to load")
                raise EmbeddingUnavailable(self._error) from exc
            finally:
                self._loaded_seconds = time.monotonic() - started

    def status(self) -> dict[str, object]:
        return {
            "state": self._state,
            "model": self.configured_model,
            "dimensions": DIMENSIONS,
            "offline": config.EMBEDDING_OFFLINE,
            "required": config.EMBEDDING_REQUIRED,
            "load_seconds": round(self._loaded_seconds, 2),
            "reranker": config.RERANKER_MODEL_ID and f"{config.RERANKER_MODEL_ID}@{config.RERANKER_REVISION}" or None,
            "error": self._error or None,
        }

    @property
    def ready(self) -> bool:
        return self._state == "ready"

    def embed(self, texts: list[str], kind: str) -> tuple[str, list[list[float]]]:
        if not self.ready:
            self.load()
        assert self._embedder is not None
        if kind == "query":
            return self._model_name, [self._query(text) for text in texts]
        return self._model_name, self._validated(self._embedder.embed_passages(texts), len(texts))

    def _query(self, text: str) -> list[float]:
        with self._query_lock:
            cached = self._queries.get(text)
            if cached is not None:
                self._queries.move_to_end(text)
                return cached
        assert self._embedder is not None
        vector = self._validated(self._embedder.embed_queries([text]), 1)[0]
        with self._query_lock:
            self._queries[text] = vector
            while len(self._queries) > config.QUERY_CACHE_SIZE:
                self._queries.popitem(last=False)
        return vector

    @staticmethod
    def _validated(vectors: list[list[float]], expected: int) -> list[list[float]]:
        if len(vectors) != expected:
            raise EmbeddingUnavailable("embedding model returned the wrong number of vectors")
        for vector in vectors:
            if len(vector) != DIMENSIONS or not all(math.isfinite(x) for x in vector):
                raise EmbeddingUnavailable("embedding model returned an invalid vector")
        return vectors

    def rerank(self, query: str, passages: list[str]) -> list[float]:
        if not config.RERANKER_MODEL_ID:
            raise EmbeddingUnavailable("reranker is not configured")
        with self._lock:
            if self._reranker is None:
                from app.rag.rerank import CrossEncoderReranker

                self._reranker = CrossEncoderReranker(
                    config.RERANKER_MODEL_ID, config.RERANKER_REVISION, local_files_only=config.EMBEDDING_OFFLINE
                )
        return self._reranker.score(query, passages)

    def shared_embedder(self) -> Embedder:
        if not self.ready:
            self.load()
        assert self._embedder is not None
        return self._embedder


service = EmbeddingService()


def embed(texts: list[str], kind: str) -> tuple[str, list[list[float]]]:
    return service.embed(texts, kind)
