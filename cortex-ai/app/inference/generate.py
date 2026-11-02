from app import config
from app.inference.hf_loader import load_chat_model
from app.inference.registry import ModelRecord, get_deployable_model

MAX_NEW_TOKENS = 180
SYSTEM_PROMPT = (
    "You explain a person's own recorded lab values in plain language. "
    "Use only the numbers and facts provided. Do not add numbers, dates or sources that are not provided. "
    "Cite research only with the bracketed tags given, such as [R1]. "
    "Do not diagnose, name conditions, or recommend medication or treatment. "
    "If the records are insufficient to answer, say so. Keep the answer under 110 words."
)


def generate_explanation(facts: str, question: str) -> str | None:
    if config.OLLAMA_MODEL:
        from app.inference.ollama import generate_with_ollama

        return generate_with_ollama(facts, question)
    if not config.INFERENCE_ENABLED or not config.MODEL_ID:
        return None
    record = get_deployable_model(config.MODEL_ID)
    if record is None:
        raise RuntimeError(f"model {config.MODEL_ID!r} is not registered as deployable")
    return generate_with_record(record, facts, question)


def generate_with_record(record: ModelRecord, facts: str, question: str) -> str:
    import torch

    tokenizer, model, device = load_chat_model(record.repository, record.revision)
    messages = [
        {"role": "system", "content": SYSTEM_PROMPT},
        {"role": "user", "content": f"Question: {question}\n\nRecorded facts:\n{facts}"},
    ]
    inputs = tokenizer.apply_chat_template(
        messages, add_generation_prompt=True, tokenize=True, return_dict=True, return_tensors="pt"
    ).to(device)
    with torch.inference_mode():
        output = model.generate(
            **inputs,
            max_new_tokens=MAX_NEW_TOKENS,
            do_sample=False,
            repetition_penalty=1.05,
            pad_token_id=tokenizer.eos_token_id,
        )
    generated = output[0][inputs["input_ids"].shape[1]:]
    return tokenizer.decode(generated, skip_special_tokens=True).strip()
