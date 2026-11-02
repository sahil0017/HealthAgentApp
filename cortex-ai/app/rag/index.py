import math
import re
from collections import Counter
from dataclasses import dataclass

TOKEN_PATTERN = re.compile(r"[a-z0-9]+")
RRF_K = 60
BM25_K1 = 1.5
BM25_B = 0.75


def tokenize(text: str) -> list[str]:
    return TOKEN_PATTERN.findall(text.lower())


@dataclass(frozen=True)
class IndexedChunk:
    chunk_id: str
    source_id: str
    text: str
    vector: tuple[float, ...]
    terms: Counter


@dataclass(frozen=True)
class SearchHit:
    chunk: IndexedChunk
    score: float


class HybridIndex:
    def __init__(self) -> None:
        self._chunks: list[IndexedChunk] = []

    def __len__(self) -> int:
        return len(self._chunks)

    def add(self, chunk_id: str, source_id: str, text: str, vector: list[float]) -> None:
        self._chunks.append(
            IndexedChunk(chunk_id, source_id, text, tuple(vector), Counter(tokenize(text)))
        )

    def search(self, query_text: str, query_vector: list[float], k: int) -> list[SearchHit]:
        if not self._chunks or k <= 0:
            return []
        dense = _rank(self._chunks, lambda c: _cosine(query_vector, c.vector))
        lexical = _rank(self._chunks, lambda c: _bm25(query_text, c, self._chunks))
        fused: dict[str, float] = {}
        by_id = {c.chunk_id: c for c in self._chunks}
        for ranking in (dense, lexical):
            for position, chunk in enumerate(ranking, start=1):
                fused[chunk.chunk_id] = fused.get(chunk.chunk_id, 0.0) + 1.0 / (RRF_K + position)
        ordered = sorted(fused.items(), key=lambda item: item[1], reverse=True)[:k]
        return [SearchHit(by_id[chunk_id], score) for chunk_id, score in ordered]


def _rank(chunks: list[IndexedChunk], scorer) -> list[IndexedChunk]:
    scored = [(scorer(chunk), chunk) for chunk in chunks]
    scored = [item for item in scored if item[0] > 0]
    scored.sort(key=lambda item: item[0], reverse=True)
    return [chunk for _, chunk in scored]


def _cosine(a: list[float], b: tuple[float, ...]) -> float:
    dot = sum(x * y for x, y in zip(a, b))
    norm = math.sqrt(sum(x * x for x in a)) * math.sqrt(sum(y * y for y in b))
    return dot / norm if norm else 0.0


def _bm25(query_text: str, chunk: IndexedChunk, corpus: list[IndexedChunk]) -> float:
    query_terms = set(tokenize(query_text))
    if not query_terms:
        return 0.0
    average_length = sum(sum(c.terms.values()) for c in corpus) / len(corpus)
    length = sum(chunk.terms.values())
    score = 0.0
    for term in query_terms:
        frequency = chunk.terms.get(term, 0)
        if not frequency:
            continue
        containing = sum(1 for c in corpus if term in c.terms)
        idf = math.log(1 + (len(corpus) - containing + 0.5) / (containing + 0.5))
        score += idf * frequency * (BM25_K1 + 1) / (
            frequency + BM25_K1 * (1 - BM25_B + BM25_B * length / average_length)
        )
    return score
