from pathlib import Path

from pydantic import BaseModel

from app import config
from app.rag.chunking import chunk_text
from app.rag.corpus import ResearchSource, load_corpus
from app.rag.embeddings import Embedder
from app.rag.index import HybridIndex
from app.rag.rerank import Reranker

EXCERPT_CHARS = 400
CANDIDATES = 20


class Citation(BaseModel):
    source_id: str
    title: str
    source_type: str
    verification_status: str
    identifier: str | None
    excerpt: str
    score: float


class Retriever:
    def __init__(self, embedder: Embedder, index: HybridIndex, reranker: Reranker | None = None):
        self._embedder = embedder
        self._index = index
        self._reranker = reranker
        self._sources: dict[str, ResearchSource] = {}

    @classmethod
    def build(
        cls,
        sources: list[ResearchSource],
        embedder: Embedder,
        reranker: Reranker | None = None,
        include_unverified: bool = False,
    ) -> "Retriever":
        retriever = cls(embedder, HybridIndex(), reranker)
        eligible = [s for s in sources if include_unverified or s.verification_status == "verified"]
        pieces: list[tuple[str, str, str]] = []
        for source in eligible:
            retriever._sources[source.source_id] = source
            for number, chunk in enumerate(chunk_text(source.text)):
                pieces.append((f"{source.source_id}#{number}", source.source_id, chunk.text))
        if pieces:
            vectors = embedder.embed_passages([text for _, _, text in pieces])
            for (chunk_id, source_id, text), vector in zip(pieces, vectors):
                retriever._index.add(chunk_id, source_id, text, vector)
        return retriever

    def retrieve(self, question: str, k: int = 3) -> list[Citation]:
        if not len(self._index):
            return []
        query_vector = self._embedder.embed_queries([question])[0]
        hits = self._index.search(question, query_vector, k=CANDIDATES)
        scores = [hit.score for hit in hits]
        if self._reranker and hits:
            scores = self._reranker.score(question, [hit.chunk.text for hit in hits])
        ranked = sorted(zip(scores, hits), key=lambda pair: pair[0], reverse=True)[:k]
        citations: list[Citation] = []
        for score, hit in ranked:
            source = self._sources[hit.chunk.source_id]
            citations.append(
                Citation(
                    source_id=source.source_id,
                    title=source.title,
                    source_type=source.source_type,
                    verification_status=source.verification_status,
                    identifier=source.identifier,
                    excerpt=hit.chunk.text[:EXCERPT_CHARS],
                    score=float(score),
                )
            )
        return citations


def build_from_config() -> Retriever | None:
    if not config.RESEARCH_CORPUS_PATH or not config.EMBEDDING_MODEL_ID:
        return None
    from app.rag.embeddings import PassageCache
    from app.rag.rerank import CrossEncoderReranker
    from app.rag.service import service

    embedder = service.shared_embedder()
    cache = PassageCache(
        embedder,
        Path(config.CACHE_DIR) / "passages.json",
        namespace=f"{config.EMBEDDING_MODEL_ID}@{config.EMBEDDING_REVISION}",
    )
    reranker = (
        CrossEncoderReranker(config.RERANKER_MODEL_ID, config.RERANKER_REVISION)
        if config.RERANKER_MODEL_ID
        else None
    )
    sources = load_corpus(Path(config.RESEARCH_CORPUS_PATH))
    return Retriever.build(sources, cache, reranker)
