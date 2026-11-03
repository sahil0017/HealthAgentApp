import pytest

torch = pytest.importorskip("torch")

from app import config
from app.inference import generate, registry


class Encoded(dict):
    def to(self, device):
        return self


class FakeTokenizer:
    eos_token_id = 0

    def __init__(self):
        self.messages = None

    def apply_chat_template(self, messages, **kwargs):
        self.messages = messages
        return Encoded(input_ids=torch.tensor([[1, 2, 3]]), attention_mask=torch.tensor([[1, 1, 1]]))

    def decode(self, ids, skip_special_tokens=True):
        return " Your HbA1c rose. " if ids.tolist() == [7, 8] else "unexpected"


class FakeModel:
    def __init__(self):
        self.kwargs = None

    def generate(self, input_ids, attention_mask=None, **kwargs):
        self.kwargs = kwargs
        return torch.cat([input_ids, torch.tensor([[7, 8]])], dim=1)


def approved_record(status="approved"):
    return registry.ModelRecord(
        model_id="cortex-health-reasoner",
        provider="huggingface",
        repository="org/model",
        revision="abc123",
        task="text-generation",
        deployment="local",
        license="apache-2.0",
        intended_use="educational-health-record-explanation",
        evaluation_status=status,
    )


@pytest.fixture
def enabled(monkeypatch):
    monkeypatch.setattr(config, "INFERENCE_ENABLED", True)
    monkeypatch.setattr(config, "MODEL_ID", "cortex-health-reasoner")
    monkeypatch.setitem(registry.REGISTRY, "cortex-health-reasoner", approved_record())


def test_disabled_inference_returns_none(monkeypatch):
    monkeypatch.setattr(config, "INFERENCE_ENABLED", False)
    assert generate.generate_explanation("facts", "question") is None


def test_generates_with_pinned_revision_and_decodes_only_new_tokens(monkeypatch, enabled):
    tokenizer, model = FakeTokenizer(), FakeModel()
    loaded = []

    def fake_loader(repository, revision):
        loaded.append((repository, revision))
        return tokenizer, model, "cpu"

    monkeypatch.setattr(generate, "load_chat_model", fake_loader)
    assert generate.generate_explanation("HbA1c increased", "Why?") == "Your HbA1c rose."
    assert loaded == [("org/model", "abc123")]
    roles = [m["role"] for m in tokenizer.messages]
    assert roles == ["system", "user"]
    assert "HbA1c increased" in tokenizer.messages[1]["content"]
    assert model.kwargs["do_sample"] is False


@pytest.mark.parametrize("status", ["research", "retired"])
def test_non_deployable_status_is_refused(monkeypatch, enabled, status):
    monkeypatch.setitem(registry.REGISTRY, "cortex-health-reasoner", approved_record(status))
    with pytest.raises(RuntimeError):
        generate.generate_explanation("facts", "question")
