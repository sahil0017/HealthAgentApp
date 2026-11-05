import os


def _flag(name: str) -> bool:
    return os.getenv(name, "false").lower() in {"1", "true", "yes"}


def _pinned(model_var: str, revision_var: str) -> tuple[str, str]:
    model = os.getenv(model_var, "").strip()
    revision = os.getenv(revision_var, "").strip()
    if model and not revision:
        raise RuntimeError(f"{revision_var} must pin a model revision when {model_var} is set")
    return model, revision


INFERENCE_ENABLED = _flag("CORTEX_INFERENCE_ENABLED")
MODEL_ID = os.getenv("CORTEX_MODEL_ID", "").strip()

RESEARCH_CORPUS_PATH = os.getenv("CORTEX_RESEARCH_CORPUS", "").strip()
CACHE_DIR = os.getenv("CORTEX_CACHE_DIR", os.path.expanduser("~/.cache/cortex-ai"))
EMBEDDING_MODEL_ID, EMBEDDING_REVISION = _pinned("CORTEX_EMBEDDING_MODEL", "CORTEX_EMBEDDING_REVISION")
RERANKER_MODEL_ID, RERANKER_REVISION = _pinned("CORTEX_RERANKER_MODEL", "CORTEX_RERANKER_REVISION")

OLLAMA_URL = os.getenv("CORTEX_OLLAMA_URL", "http://localhost:11434").strip()
OLLAMA_MODEL = os.getenv("CORTEX_OLLAMA_MODEL", "").strip()
OLLAMA_TIMEOUT_SECONDS = float(os.getenv("CORTEX_OLLAMA_TIMEOUT_SECONDS", "60"))

# Hugging Face Inference Providers (OpenAI-compatible). Used only by the agent loop, which the API
# gateway gates behind the user's "cloud_model" permission, so no record data reaches it by default.
HF_TOKEN = os.getenv("CORTEX_HF_TOKEN", "").strip()
HF_API_URL = os.getenv("CORTEX_HF_API_URL", "https://router.huggingface.co/v1").strip().rstrip("/")
HF_AGENT_MODEL = os.getenv("CORTEX_HF_AGENT_MODEL", "Qwen/Qwen2.5-7B-Instruct").strip()
HF_TIMEOUT_SECONDS = float(os.getenv("CORTEX_HF_TIMEOUT_SECONDS", "45"))
# GitHub Copilot SDK: signs in with this GitHub token (needs Copilot access). Empty disables it.
COPILOT_TOKEN = os.getenv("CORTEX_COPILOT_GITHUB_TOKEN", "").strip()
COPILOT_MODEL = os.getenv("CORTEX_COPILOT_MODEL", "auto").strip()
COPILOT_VISION_MODEL = os.getenv("CORTEX_COPILOT_VISION_MODEL", "").strip() or COPILOT_MODEL
COPILOT_TIMEOUT_SECONDS = float(os.getenv("CORTEX_COPILOT_TIMEOUT_SECONDS", "90"))

INFERENCE_STATUS = "ollama" if OLLAMA_MODEL else ("enabled" if INFERENCE_ENABLED and MODEL_ID else "disabled")

ENVIRONMENT = os.getenv("CORTEX_ENV", "development").strip().lower()
EMBEDDING_REQUIRED = _flag("CORTEX_EMBEDDING_REQUIRED") or ENVIRONMENT == "production"
EMBEDDING_QUERY_PREFIX = os.getenv(
    "CORTEX_EMBEDDING_QUERY_PREFIX", "Represent this sentence for searching relevant passages: " if EMBEDDING_MODEL_ID.startswith("BAAI/bge") else ""
)
EMBEDDING_MAX_SEQ_LENGTH = int(os.getenv("CORTEX_EMBEDDING_MAX_SEQ_LENGTH", "256"))
EMBEDDING_BATCH_SIZE = int(os.getenv("CORTEX_EMBEDDING_BATCH_SIZE", "16"))
EMBEDDING_DEVICE = os.getenv("CORTEX_EMBEDDING_DEVICE", "").strip()
EMBEDDING_OFFLINE = _flag("CORTEX_HF_OFFLINE")
QUERY_CACHE_SIZE = int(os.getenv("CORTEX_QUERY_CACHE_SIZE", "512"))
