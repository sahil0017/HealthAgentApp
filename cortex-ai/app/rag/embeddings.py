import hashlib
import json
import os
import threading
from pathlib import Path
from typing import Protocol


class Embedder(Protocol):
    def embed_passages(self, texts: list[str]) -> list[list[float]]: ...

    def embed_queries(self, texts: list[str]) -> list[list[float]]: ...


class SentenceTransformerEmbedder:
    """Pinned Hugging Face sentence embedder. Calls are serialised because the model is not guaranteed thread-safe."""

    def __init__(
        self,
        model_id: str,
        revision: str,
        local_files_only: bool = False,
        query_prefix: str = "",
        max_seq_length: int = 256,
        batch_size: int = 16,
        device: str | None = None,
    ):
        from sentence_transformers import SentenceTransformer

        self._model = SentenceTransformer(
            model_id, revision=revision, local_files_only=local_files_only, device=device or None
        )
        if max_seq_length:
            self._model.max_seq_length = min(max_seq_length, self._model.max_seq_length or max_seq_length)
        self._query_prefix = query_prefix
        self._batch_size = batch_size
        self._lock = threading.Lock()
        self.dimensions = int(self._model.get_embedding_dimension())

    def embed_passages(self, texts: list[str]) -> list[list[float]]:
        return self._encode(texts)

    def embed_queries(self, texts: list[str]) -> list[list[float]]:
        return self._encode([self._query_prefix + text for text in texts])

    def _encode(self, texts: list[str]) -> list[list[float]]:
        with self._lock:
            vectors = self._model.encode(
                texts, normalize_embeddings=True, convert_to_numpy=True, batch_size=self._batch_size, show_progress_bar=False
            )
        return [vector.tolist() for vector in vectors]


class PassageCache:
    """Persists passage vectors only; queries are never written to disk."""

    def __init__(self, inner: Embedder, path: Path, namespace: str):
        self._inner = inner
        self._path = path
        self._namespace = namespace
        self._vectors: dict[str, list[float]] = (
            json.loads(path.read_text(encoding="utf-8")) if path.exists() else {}
        )

    def embed_passages(self, texts: list[str]) -> list[list[float]]:
        keys = [self._key(text) for text in texts]
        missing: dict[str, str] = {}
        for key, text in zip(keys, texts):
            if key not in self._vectors:
                missing.setdefault(key, text)
        if missing:
            vectors = self._inner.embed_passages(list(missing.values()))
            self._vectors.update(zip(missing.keys(), vectors))
            self._save()
        return [self._vectors[key] for key in keys]

    def embed_queries(self, texts: list[str]) -> list[list[float]]:
        return self._inner.embed_queries(texts)

    def _key(self, text: str) -> str:
        return hashlib.sha256(f"{self._namespace}\n{text}".encode("utf-8")).hexdigest()

    def _save(self) -> None:
        self._path.parent.mkdir(parents=True, exist_ok=True)
        temporary = self._path.with_suffix(".tmp")
        temporary.write_text(json.dumps(self._vectors), encoding="utf-8")
        os.replace(temporary, self._path)
