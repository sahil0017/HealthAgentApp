from functools import lru_cache
from typing import Any


@lru_cache(maxsize=1)
def load_chat_model(repository: str, revision: str) -> tuple[Any, Any, str]:
    import torch
    from transformers import AutoModelForCausalLM, AutoTokenizer

    device = "mps" if torch.backends.mps.is_available() else "cpu"
    dtype = torch.float16 if device == "mps" else torch.float32
    tokenizer = AutoTokenizer.from_pretrained(repository, revision=revision)
    model = AutoModelForCausalLM.from_pretrained(repository, revision=revision, dtype=dtype).to(device).eval()
    return tokenizer, model, device
