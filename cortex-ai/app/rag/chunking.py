from dataclasses import dataclass


@dataclass(frozen=True)
class Chunk:
    text: str
    start: int
    end: int


def chunk_text(text: str, size: int = 800, overlap: int = 120) -> list[Chunk]:
    if size <= overlap:
        raise ValueError("chunk size must be greater than overlap")
    chunks: list[Chunk] = []
    start = 0
    while start < len(text):
        end = min(start + size, len(text))
        if end < len(text):
            cut = text.rfind(" ", start + size // 2, end)
            if cut > start:
                end = cut
        window = text[start:end]
        piece = window.strip()
        if piece:
            offset = start + (len(window) - len(window.lstrip()))
            chunks.append(Chunk(piece, offset, offset + len(piece)))
        if end >= len(text):
            break
        start = max(end - overlap, start + 1)
    return chunks
