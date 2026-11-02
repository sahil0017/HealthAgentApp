import io

from pypdf import PdfReader

MAX_PAGES = 100


def read_pages(raw: bytes, filename: str | None) -> list[tuple[int, str]]:
    if filename and filename.lower().endswith(".pdf"):
        reader = PdfReader(io.BytesIO(raw))
        return [(number, page.extract_text() or "") for number, page in enumerate(reader.pages[:MAX_PAGES], start=1)]
    return [(1, raw.decode("utf-8", errors="replace"))]
