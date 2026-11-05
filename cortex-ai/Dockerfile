FROM ghcr.io/astral-sh/uv:0.13.6-python3.12-trixie-slim AS model
ARG CORTEX_INFERENCE=1
ARG EMBEDDING_MODEL=BAAI/bge-small-en-v1.5
ARG EMBEDDING_REVISION=5c38ec7c405ec4b44b94cc5a9bb96e735b38267a
ARG RERANKER_MODEL=
ARG RERANKER_REVISION=
ENV HF_HOME=/models
RUN mkdir -p /models \
    && if [ "$CORTEX_INFERENCE" = "1" ]; then \
         uv pip install --system huggingface_hub \
         && python -c "import os; from huggingface_hub import snapshot_download; \
snapshot_download(os.environ['EMBEDDING_MODEL'], revision=os.environ['EMBEDDING_REVISION']); \
m=os.environ.get('RERANKER_MODEL'); r=os.environ.get('RERANKER_REVISION'); \
(snapshot_download(m, revision=r) if m and r else None)"; \
       fi

FROM ghcr.io/astral-sh/uv:0.13.6-python3.12-trixie-slim
ARG CORTEX_INFERENCE=1
ARG EMBEDDING_MODEL=BAAI/bge-small-en-v1.5
ARG EMBEDDING_REVISION=5c38ec7c405ec4b44b94cc5a9bb96e735b38267a
ARG RERANKER_MODEL=
ARG RERANKER_REVISION=
ENV PYTHONUNBUFFERED=1 PYTHONDONTWRITEBYTECODE=1 HF_HOME=/models
WORKDIR /app
COPY requirements.txt requirements-inference.txt ./
RUN --mount=type=cache,target=/root/.cache/uv,sharing=locked \
    uv pip install --system --no-compile-bytecode -r requirements.txt
RUN --mount=type=cache,target=/root/.cache/uv,sharing=locked \
    if [ "$CORTEX_INFERENCE" = "1" ]; then \
      uv pip install --system --no-compile-bytecode torch==2.15.0 --index-url https://download.pytorch.org/whl/cpu \
      && uv pip install --system --no-compile-bytecode -r requirements-inference.txt; \
    fi
# The Copilot SDK needs its runtime on disk; stage it at build time so the non-root user never downloads it.
ENV COPILOT_CLI_EXTRACT_DIR=/opt/copilot COPILOT_SKIP_CLI_DOWNLOAD=1
RUN COPILOT_SKIP_CLI_DOWNLOAD=0 python -m copilot download-runtime && chmod -R a+rX /opt/copilot
ARG COVERAGE=0
RUN --mount=type=cache,target=/root/.cache/uv,sharing=locked \
    if [ "$COVERAGE" = "1" ]; then uv pip install --system --no-compile-bytecode coverage; fi
COPY app ./app
COPY --from=model --chown=10001:10001 /models /models
RUN useradd --create-home --uid 10001 cortex
USER 10001:10001
ENV CORTEX_HF_OFFLINE=${CORTEX_INFERENCE} HF_HUB_OFFLINE=${CORTEX_INFERENCE} CORTEX_EMBEDDING_MODEL=${EMBEDDING_MODEL} CORTEX_EMBEDDING_REVISION=${EMBEDDING_REVISION} \
    CORTEX_RERANKER_MODEL=${RERANKER_MODEL} CORTEX_RERANKER_REVISION=${RERANKER_REVISION}
EXPOSE 8000
HEALTHCHECK --interval=10s --timeout=5s --start-period=90s --retries=6 \
  CMD python -c "import urllib.request,sys; sys.exit(0 if urllib.request.urlopen('http://127.0.0.1:8000/readyz', timeout=4).status==200 else 1)"
CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8000", "--workers", "1"]
