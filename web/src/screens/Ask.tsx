import { useState } from "react";
import { del, get, post } from "../api/client";
import { useProfile } from "../api/profile";
import type { Answer } from "../api/types";
import { messageOf, useAsync } from "../api/useAsync";
import { Badge, Banner, Card, Empty, Load, Page, fmtDate } from "../components/ui";

const STARTERS = ["What was my latest HbA1c?", "Which of my results are out of range?", "What should I ask my doctor?"];

function AnswerCard({ a, profileId, onChange }: { a: Answer; profileId: string; onChange?: () => void }) {
  const [rating, setRating] = useState<string | null>(a.feedback ?? null);
  async function rate(r: "helpful" | "not_helpful") {
    if (!a.id) return;
    await post(`/profiles/${profileId}/questions/${a.id}/feedback`, { rating: r });
    setRating(r);
  }
  return (
    <Card>
      {a.question && <h2>{a.question}</h2>}
      {a.urgent && <Banner tone="danger">This may need urgent medical attention. Contact a clinician or emergency services now.</Banner>}
      <p style={{ whiteSpace: "pre-wrap" }}>{a.answer}</p>
      <p className="muted"><Badge tone={a.confidence === "high" ? "ok" : a.confidence === "medium" ? undefined : "warn"}>{a.confidence ?? "insufficient"} confidence</Badge> {a.uncertainty}</p>
      {(a.limitations ?? []).length > 0 && <ul className="muted">{(a.limitations ?? []).map((l) => <li key={l}>{l}</li>)}</ul>}
      {(a.sources ?? []).length > 0 && (
        <>
          <h2>Sources</h2>
          {(a.sources ?? []).map((s) => (
            <p className="muted" key={`${s.type}-${s.identifier ?? ""}-${s.description}`}>
              <Badge>{s.type}</Badge> {s.url ? <a href={s.url} target="_blank" rel="noreferrer">{s.description}</a> : s.description}
              {s.verification_status ? ` · ${s.verification_status}` : ""}
            </p>
          ))}
        </>
      )}
      {a.id && (
        <div className="inline">
          <button className={`btn small ${rating === "helpful" ? "" : "secondary"}`} onClick={() => void rate("helpful")} aria-label="Helpful">👍</button>
          <button className={`btn small ${rating === "not_helpful" ? "" : "secondary"}`} onClick={() => void rate("not_helpful")} aria-label="Not helpful">👎</button>
          {onChange && <button className="btn ghost small" onClick={async () => { await del(`/profiles/${profileId}/questions/${a.id}`); onChange(); }}>Delete</button>}
          {a.created_at && <span className="muted">{fmtDate(a.created_at)}</span>}
        </div>
      )}
    </Card>
  );
}

export function Ask() {
  const { profile } = useProfile();
  const id = profile!.id;
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [latest, setLatest] = useState<Answer | null>(null);
  // The list endpoint wraps the answers as {questions: [...]}.
  const history = useAsync(async () => {
    const r = await get<Answer[] | { questions: Answer[] }>(`/profiles/${id}/questions`);
    return Array.isArray(r) ? r : r?.questions ?? [];
  }, [id]);

  async function ask(question: string) {
    setBusy(true);
    setError(null);
    try {
      setLatest(await post<Answer>(`/profiles/${id}/questions`, { question }));
      setText("");
      history.reload();
    } catch (e) {
      setError(messageOf(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Page title="Ask Sehat" subtitle="Answers are grounded in your records and cite their sources">
      <Card>
        <form className="stack" onSubmit={(e) => { e.preventDefault(); if (text.trim()) void ask(text.trim()); }}>
          <label htmlFor="q">Your question</label>
          <textarea
            id="q"
            rows={3}
            value={text}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => { if (e.key === "Enter" && (e.metaKey || e.ctrlKey) && text.trim() && !busy) void ask(text.trim()); }}
            placeholder="How has my HbA1c changed this year?"
          />
          <div className="inline" style={{ justifyContent: "space-between" }}>
            <span className="hint">Answers only use your own records and approved sources.</span>
            <button className="btn" disabled={busy || !text.trim()}>{busy ? "Thinking…" : "Ask"}</button>
          </div>
        </form>
      </Card>
      {!latest && (
        <div className="chips" style={{ marginBottom: "1rem" }} aria-label="Example questions">
          {STARTERS.map((s) => <button key={s} className="chip" onClick={() => void ask(s)} disabled={busy}>{s}</button>)}
        </div>
      )}
      {error && <Banner tone="danger">{error}</Banner>}
      {busy && <div className="card" aria-busy="true"><div className="skeleton" /><div className="skeleton" style={{ width: "70%" }} /></div>}
      {latest && (
        <>
          <AnswerCard a={latest} profileId={id} />
          {latest.suggested_questions?.map((s) => (
            <button key={s} className="chip" onClick={() => void ask(s)}>{s}</button>
          ))}
        </>
      )}
      <h2 style={{ marginTop: "1.5rem" }}>Earlier questions</h2>
      <Load q={history}>
        {(items) => (items.length === 0 ? <Empty title="No questions yet">Ask something above and your answers will be saved here.</Empty> : items.map((a) => <AnswerCard key={a.id} a={a} profileId={id} onChange={history.reload} />))}
      </Load>
    </Page>
  );
}
