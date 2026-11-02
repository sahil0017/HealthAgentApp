import hashlib
import json

import pytest

from app import config
from app.rag.chunking import chunk_text
from app.rag.corpus import ResearchSource, load_corpus
from app.rag.index import HybridIndex, tokenize
from app.rag.retriever import Retriever
from app.workflows.health_question import build_health_question_graph

DIMENSIONS = 64


class HashingEmbedder:
    def embed_passages(self, texts):
        return self._embed(texts)

    def embed_queries(self, texts):
        return self._embed(texts)

    def _embed(self, texts):
        vectors = []
        for text in texts:
            vector = [0.0] * DIMENSIONS
            for token in tokenize(text):
                bucket = int(hashlib.sha256(token.encode()).hexdigest(), 16) % DIMENSIONS
                vector[bucket] += 1.0
            vectors.append(vector)
        return vectors


class InsulinReranker:
    def score(self, query, passages):
        return [1.0 if "insulin" in passage else 0.0 for passage in passages]


def source(source_id, text, verification="verified", source_type="peer_reviewed_study", identifier="10.1000/x"):
    return ResearchSource(
        source_id=source_id,
        title=f"Title {source_id}",
        text=text,
        source_type=source_type,
        verification_status=verification,
        identifier=identifier,
    )


def test_chunks_cover_text_with_offsets_and_overlap():
    text = "word " * 400
    chunks = chunk_text(text, size=200, overlap=50)
    assert len(chunks) > 1
    for chunk in chunks:
        assert text[chunk.start:chunk.end] == chunk.text
    assert chunks[1].start < chunks[0].end


def test_chunk_size_must_exceed_overlap():
    with pytest.raises(ValueError):
        chunk_text("abc", size=10, overlap=10)


def test_hybrid_index_prefers_exact_term_match():
    embedder = HashingEmbedder()
    index = HybridIndex()
    texts = ["HbA1c guidance for glucose control", "unrelated cardiology trial results"]
    for number, text in enumerate(texts):
        index.add(f"c{number}", f"s{number}", text, embedder.embed_passages([text])[0])
    hits = index.search("HbA1c", embedder.embed_queries(["HbA1c"])[0], k=2)
    assert hits[0].chunk.source_id == "s0"


def test_unverified_sources_are_excluded_by_default():
    sources = [source("a", "HbA1c trial evidence"), source("b", "HbA1c preprint", verification="unverified")]
    retriever = Retriever.build(sources, HashingEmbedder())
    citations = retriever.retrieve("HbA1c", k=5)
    assert {c.source_id for c in citations} == {"a"}


def test_citations_carry_source_metadata():
    retriever = Retriever.build([source("guideline", "HbA1c targets for adults")], HashingEmbedder())
    citation = retriever.retrieve("HbA1c targets", k=1)[0]
    assert citation.title == "Title guideline"
    assert citation.source_type == "peer_reviewed_study"
    assert citation.identifier == "10.1000/x"
    assert citation.verification_status == "verified"


def test_reranker_decides_final_order():
    sources = [source("a", "HbA1c and glucose"), source("b", "HbA1c and insulin")]
    reranked = Retriever.build(sources, HashingEmbedder(), InsulinReranker()).retrieve("HbA1c glucose insulin", k=2)
    assert reranked[0].source_id == "b"


def test_load_corpus_rejects_duplicate_ids(tmp_path):
    row = {"source_id": "dup", "title": "T", "text": "x", "source_type": "preprint", "verification_status": "verified"}
    path = tmp_path / "corpus.jsonl"
    path.write_text("\n".join(json.dumps(row) for _ in range(2)) + "\n", encoding="utf-8")
    with pytest.raises(ValueError, match="unique"):
        load_corpus(path)


def test_load_corpus_reports_invalid_line(tmp_path):
    path = tmp_path / "corpus.jsonl"
    path.write_text('{"source_id": "x"}\n', encoding="utf-8")
    with pytest.raises(ValueError, match="line 1"):
        load_corpus(path)


def test_research_question_returns_citations_in_answer():
    retriever = Retriever.build([source("hba1c", "HbA1c changes are discussed in diabetes guidance.")], HashingEmbedder())
    graph = build_health_question_graph(retriever=retriever)
    result = graph.invoke(
        {
            "question": "What does the research say about HbA1c?",
            "observations": [],
        }
    )
    assert "health_research" in result["domains"]
    assert any(s["type"] == "research_source" and s["source_id"] == "hba1c" for s in result["sources"])
    assert any("HbA1c" in finding for finding in result["findings"])
    assert "External medical research search is not connected yet." not in result["limitations"]


def test_research_question_without_matches_states_limitation():
    retriever = Retriever.build([source("other", "cardiology trial")], HashingEmbedder())
    graph = build_health_question_graph(retriever=retriever)
    result = graph.invoke({"question": "What does the research say about kidneys?", "observations": []})
    assert "No verified research sources matched this question." in result["limitations"]


def test_pinned_model_requires_revision(monkeypatch):
    monkeypatch.setenv("CORTEX_TEST_MODEL", "org/model")
    monkeypatch.delenv("CORTEX_TEST_REVISION", raising=False)
    with pytest.raises(RuntimeError, match="must pin a model revision"):
        config._pinned("CORTEX_TEST_MODEL", "CORTEX_TEST_REVISION")


def test_build_from_config_is_disabled_without_corpus(monkeypatch):
    from app.rag.retriever import build_from_config

    monkeypatch.setattr(config, "RESEARCH_CORPUS_PATH", "")
    assert build_from_config() is None


def test_passage_cache_persists_and_skips_recomputation(tmp_path):
    from app.rag.embeddings import PassageCache

    class CountingEmbedder(HashingEmbedder):
        calls = 0

        def embed_passages(self, texts):
            CountingEmbedder.calls += len(texts)
            return super().embed_passages(texts)

    path = tmp_path / "passages.json"
    PassageCache(CountingEmbedder(), path, "model@rev").embed_passages(["alpha beta"])
    second = PassageCache(CountingEmbedder(), path, "model@rev")
    second.embed_passages(["alpha beta"])
    assert CountingEmbedder.calls == 1


def test_query_embeddings_are_never_cached(tmp_path):
    from app.rag.embeddings import PassageCache

    path = tmp_path / "passages.json"
    PassageCache(HashingEmbedder(), path, "model@rev").embed_queries(["private question about my labs"])
    assert not path.exists()
