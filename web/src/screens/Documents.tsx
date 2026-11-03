import { useEffect, useRef, useState } from "react";
import { Icon } from "../components/Icon";
import { del, get, post } from "../api/client";
import { useProfile } from "../api/profile";
import type { Biomarker, MedicalDocument, SearchHit } from "../api/types";
import { messageOf, useAsync } from "../api/useAsync";
import { Badge, Banner, Card, Empty, Load, Page, humanize } from "../components/ui";

const READY = ["review_required", "manual_review_required"];
const BUSY = ["queued", "processing", "extracting"];
const STATUS: Record<string, string> = { queued: "Reading…", processing: "Reading…", extracting: "Reading…", review_required: "Ready to review", manual_review_required: "Needs a careful look", approved: "Confirmed", failed: "Couldn't read" };

function Review({ doc, profileId, onDone }: { doc: MedicalDocument; profileId: string; onDone: () => void }) {
  const [rows, setRows] = useState<Biomarker[]>(doc.extraction?.biomarkers ?? []);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const set = (i: number, patch: Partial<Biomarker>) => setRows((r) => r.map((x, j) => (j === i ? { ...x, ...patch } : x)));

  return (
    <div className="stack">
      {error && <Banner tone="danger">{error}</Banner>}
      {doc.extraction?.warnings.map((w) => <Banner key={w} tone="warn">{w}</Banner>)}
      {rows.length === 0 && <Empty title="No values found">Nothing could be read from this report automatically.</Empty>}
      {rows.map((b, i) => (
        <div className="inline" key={b.candidate_id ?? i}>
          <input aria-label={`Name ${i + 1}`} value={b.name} onChange={(e) => set(i, { name: e.target.value })} style={{ flex: 2, minWidth: 140 }} />
          <input aria-label={`Value ${i + 1}`} type="number" step="any" value={b.value} onChange={(e) => set(i, { value: Number(e.target.value) })} style={{ flex: 1, minWidth: 80 }} />
          <input aria-label={`Unit ${i + 1}`} value={b.unit} onChange={(e) => set(i, { unit: e.target.value })} style={{ flex: 1, minWidth: 80 }} />
          <input aria-label={`Date ${i + 1}`} type="date" value={b.observed_at?.slice(0, 10) ?? ""} onChange={(e) => set(i, { observed_at: e.target.value })} style={{ flex: 1, minWidth: 140 }} />
          <button className="btn secondary" onClick={() => setRows((r) => r.filter((_, j) => j !== i))} aria-label={`Remove ${b.name}`}>✕</button>
        </div>
      ))}
      <div className="inline">
        <button
          className="btn"
          disabled={busy || rows.length === 0}
          onClick={async () => {
            setBusy(true);
            try {
              await post(`/profiles/${profileId}/documents/${doc.id}/approve`, { biomarkers: rows });
              onDone();
            } catch (e) {
              setError(messageOf(e));
            } finally {
              setBusy(false);
            }
          }}
        >
          Confirm and save values
        </button>
      </div>
    </div>
  );
}

export function Documents() {
  const { profile } = useProfile();
  const id = profile!.id;
  const q = useAsync(() => get<MedicalDocument[]>(`/profiles/${id}/documents`), [id]);
  const [msg, setMsg] = useState<{ tone: "info" | "danger"; text: string } | null>(null);
  const [open, setOpen] = useState<string | null>(null);
  const [term, setTerm] = useState("");
  const [hits, setHits] = useState<SearchHit[] | null>(null);
  const file = useRef<HTMLInputElement>(null);

  async function upload(f: File) {
    const form = new FormData();
    form.append("file", f);
    try {
      setMsg({ tone: "info", text: "Uploading…" });
      await post(`/profiles/${id}/documents`, form);
      setMsg({ tone: "info", text: "Uploaded. We are reading it now; it will be ready to review in a moment." });
      q.reload();
    } catch (e) {
      setMsg({ tone: "danger", text: messageOf(e) });
    } finally {
      if (file.current) file.current.value = "";
    }
  }

  async function search(e: React.FormEvent) {
    e.preventDefault();
    if (!term.trim()) return setHits(null);
    try {
      setHits(await get<SearchHit[]>(`/profiles/${id}/documents/search?q=${encodeURIComponent(term.trim())}`));
    } catch (err) {
      setMsg({ tone: "danger", text: messageOf(err) });
    }
  }

  const [over, setOver] = useState(false);
  const busyDocs = (q.data ?? []).some((d) => BUSY.includes(d.status));
  // Extraction runs in the background, so check back while any document is still being read.
  useEffect(() => {
    if (!busyDocs) return;
    const t = setInterval(q.reload, 3000);
    return () => clearInterval(t);
  }, [busyDocs, q.reload]);

  return (
    <Page title="Vault" subtitle="Your reports and documents">
      {msg && <Banner tone={msg.tone}>{msg.text}</Banner>}
      <label
        className={`drop ${over ? "over" : ""}`}
        onDragOver={(e) => { e.preventDefault(); setOver(true); }}
        onDragLeave={() => setOver(false)}
        onDrop={(e) => { e.preventDefault(); setOver(false); const f = e.dataTransfer.files?.[0]; if (f) void upload(f); }}
      >
        <Icon name="upload" />
        <b>Add a report</b>
        <span className="muted">Drop a file here or tap to choose · PDF, PNG or JPEG</span>
        <input ref={file} aria-label="File" type="file" accept="application/pdf,image/png,image/jpeg" onChange={(e) => e.target.files?.[0] && void upload(e.target.files[0])} />
      </label>
      <div style={{ height: "1rem" }} />

      <Card title="Search inside your reports">
        <form className="inline" onSubmit={search}>
          <input aria-label="Search" placeholder="e.g. HbA1c" value={term} onChange={(e) => setTerm(e.target.value)} style={{ flex: "1 1 200px" }} />
          <button className="btn" disabled={term.trim().length < 2}>Search</button>
        </form>
        {hits?.length === 0 && <Empty title="No matches">Try another word, or check the spelling.</Empty>}
        {hits?.map((h) => (
          <div className="row" key={`${h.document_id}-${h.page}`}>
            <div><b>{h.file_name}</b> <span className="muted">page {h.page}</span><p className="muted">{h.snippet}</p></div>
          </div>
        ))}
      </Card>

      <Load q={q}>
        {(docs) => (
          <Card title="Your documents">
            {docs.length === 0 && <Empty title="No documents yet">Add your first lab report above and Sehat will read the values for you to confirm.</Empty>}
            {docs.map((d) => (
              <div key={d.id}>
                <div className="row">
                  <div>
                    <b>{d.file_name}</b>
                    <p className="muted">{humanize(d.document_type)} · version {d.document_version}</p>
                  </div>
                  <div className="inline">
                    <Badge tone={d.status === "failed" ? "danger" : READY.includes(d.status) ? "warn" : d.status === "approved" ? "ok" : "neutral"}>{STATUS[d.status] ?? humanize(d.status)}</Badge>
                    {READY.includes(d.status) && <button className="btn small" onClick={() => setOpen(open === d.id ? null : d.id)}>{open === d.id ? "Close" : "Review"}</button>}
                    {d.status === "failed" && <button className="btn secondary small" onClick={async () => { await post(`/profiles/${id}/documents/${d.id}/retry`); q.reload(); }}>Try again</button>}
                    <button className="btn ghost small" aria-label={`Delete ${d.file_name}`} onClick={async () => { if (confirm(`Delete ${d.file_name}? This cannot be undone.`)) { await del(`/documents/${d.id}`); q.reload(); } }}>Delete</button>
                  </div>
                </div>
                {open === d.id && <Review doc={d} profileId={id} onDone={() => { setOpen(null); q.reload(); }} />}
              </div>
            ))}
          </Card>
        )}
      </Load>
      <p className="muted">Values you confirm are saved as verified readings and appear under Biomarkers.</p>
    </Page>
  );
}
