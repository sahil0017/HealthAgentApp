import logging
from contextlib import asynccontextmanager

from fastapi import FastAPI
from fastapi import File, HTTPException, UploadFile
from fastapi.responses import JSONResponse
from pydantic import BaseModel, Field

from app import config
from app.research import agent as research_agent, sources as research_sources
from app.extraction.biomarkers import extract_candidates
from app.extraction.documents import read_pages
from app.inference.generate import generate_explanation
from app.inference.registry import REGISTRY, ModelRecord
from app.rag.service import EmbeddingUnavailable, service as embedding_service
from app.rag.retriever import build_from_config as build_retriever_from_config
from app.workflows.health_question import build_health_question_graph

logger = logging.getLogger(__name__)


@asynccontextmanager
async def lifespan(_: FastAPI):
    embedding_service.validate_configuration()
    try:
        embedding_service.load()
    except EmbeddingUnavailable:
        if config.EMBEDDING_REQUIRED:
            raise
        logger.warning("embedding model unavailable at startup; /readyz will report not_ready")
    yield


app = FastAPI(title="CORTEX AI", version="0.1.0", description="AI service boundary with LangGraph orchestration.", lifespan=lifespan)
health_question_graph = build_health_question_graph(explain=generate_explanation, retriever=build_retriever_from_config())


class Observation(BaseModel):
    name: str
    value: float
    unit: str
    observed_at: str | None = None
    reference_range: str | None = None


class RecordChunk(BaseModel):
    observation_id: str
    content: str = Field(max_length=1000)
    score: float = 0.0


class ResearchChunk(BaseModel):
    source_id: str
    identifier: str = ""
    title: str = Field(max_length=500)
    url: str = ""
    content: str = Field(max_length=2000)
    score: float = 0.0


class Question(BaseModel):
    question: str = Field(min_length=1, max_length=4000)
    observations: list[Observation] = Field(default_factory=list, max_length=500)
    record_chunks: list[RecordChunk] = Field(default_factory=list, max_length=20)
    research_chunks: list[ResearchChunk] = Field(default_factory=list, max_length=10)


class EmbedRequest(BaseModel):
    texts: list[str] = Field(min_length=1, max_length=64)
    kind: str = Field(default="passage", pattern="^(passage|query)$")


class ResearchRequest(BaseModel):
    term: str = Field(min_length=2, max_length=120)
    medlineplus_url: str | None = Field(default=None, max_length=300)


class DiscoverRequest(BaseModel):
    limit: int = Field(default=40, ge=1, le=100)
    offset: int = Field(default=0, ge=0, le=2000)


_fetcher = research_sources.Fetcher()


def _research_call(fn, *args):
    try:
        return fn(_fetcher, *args)
    except research_sources.EgressDenied as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc
    except Exception as exc:
        logger.warning("research lookup failed: %s", exc)
        raise HTTPException(status_code=502, detail="Upstream research source unavailable") from exc


@app.post("/v1/research/biomarker")
def research_biomarker(request: ResearchRequest) -> dict[str, object]:
    url = request.medlineplus_url
    if url and not url.startswith("https://medlineplus.gov/"):
        raise HTTPException(status_code=422, detail="medlineplus_url must be a medlineplus.gov URL")
    return {"proposal": _research_call(research_agent.research_biomarker, request.term, url)}


@app.post("/v1/research/literature")
def research_literature(request: ResearchRequest) -> dict[str, object]:
    return {"proposals": _research_call(research_agent.research_literature, request.term)}


@app.post("/v1/research/discover")
def research_discover(request: DiscoverRequest) -> dict[str, object]:
    return _research_call(research_agent.discover_biomarkers, request.limit, request.offset)


@app.get("/v1/research/unit")
def research_unit(unit: str) -> dict[str, object]:
    return {"unit": unit, "valid_ucum": _research_call(research_sources.is_valid_ucum, unit)}


@app.post("/v1/embed")
def embed_texts(request: EmbedRequest) -> dict[str, object]:
    if any(len(t) > 2000 for t in request.texts):
        raise HTTPException(status_code=422, detail="Text is too long")
    try:
        model, vectors = embedding_service.embed(request.texts, request.kind)
    except EmbeddingUnavailable as exc:
        raise HTTPException(status_code=503, detail="Embedding model is unavailable") from exc
    return {"model": model, "dimensions": len(vectors[0]), "vectors": vectors}


@app.get("/v1/embedding")
def embedding_status() -> dict[str, object]:
    return embedding_service.status()


class RerankRequest(BaseModel):
    query: str = Field(min_length=1, max_length=500)
    passages: list[str] = Field(min_length=1, max_length=50)


@app.post("/v1/rerank")
def rerank(request: RerankRequest) -> dict[str, object]:
    if not config.RERANKER_MODEL_ID:
        raise HTTPException(status_code=501, detail="Reranker is not configured")
    if any(len(p) > 2000 for p in request.passages):
        raise HTTPException(status_code=422, detail="Passage is too long")
    try:
        return {"scores": embedding_service.rerank(request.query, request.passages)}
    except EmbeddingUnavailable as exc:
        raise HTTPException(status_code=503, detail="Reranker is unavailable") from exc


@app.get("/readyz")
def ready() -> JSONResponse:
    status = embedding_service.status()
    return JSONResponse(status_code=200 if embedding_service.ready else 503, content={"status": "ready" if embedding_service.ready else "not_ready", "embedding": status})


@app.get("/healthz")
def health() -> dict[str, str]:
    return {"status": "ok", "service": "cortex-ai", "inference": config.INFERENCE_STATUS}


@app.get("/v1/models")
def models() -> list[ModelRecord]:
    return list(REGISTRY.values())


@app.get("/v1/capabilities")
def capabilities() -> dict[str, object]:
    return {
        "agents": ["medical_records", "lab_intelligence", "health_research"],
        "orchestration": "langgraph",
        "mcp": "served_by_api_gateway",
        "embeddings": embedding_service.status(),
        "model_inference": config.INFERENCE_STATUS,
    }


@app.post("/v1/extract/document")
async def extract_document(file: UploadFile = File(...)) -> dict[str, object]:
    raw = await file.read(15 * 1024 * 1024 + 1)
    if not raw or len(raw) > 15 * 1024 * 1024:
        raise HTTPException(status_code=413, detail="Document size is invalid")
    try:
        pages = read_pages(raw, file.filename)
    except Exception as exc:
        raise HTTPException(status_code=422, detail="Document text could not be read") from exc
    text = "\n".join(page_text for _, page_text in pages)
    if not text.strip():
        return {
            "biomarkers": [],
            "text_preview": "",
            "warnings": ["No selectable text was found. Scanned-document OCR is not configured yet."],
        }
    candidates, warnings = extract_candidates(pages)
    return {
        "biomarkers": [c.as_dict() for c in candidates],
        "text_preview": text[:2000],
        "warnings": ["Automated extraction can be wrong. Review every value, unit, date, and reference range before saving."]
        + warnings,
    }


@app.post("/v1/answer")
def answer(question: Question) -> dict[str, object]:
    result = health_question_graph.invoke(
        {
            "question": question.question,
            "observations": [o.model_dump() for o in question.observations],
            "record_chunks": [c.model_dump() for c in question.record_chunks],
            "research_chunks": [c.model_dump() for c in question.research_chunks],
        }
    )
    return {
        "answer": result["answer"],
        "sources": result.get("sources", []),
        "limitations": result["limitations"],
        "workflow": result["domains"],
    }
