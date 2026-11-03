import { useState } from "react";
import { del, get, post } from "../api/client";
import { useProfile } from "../api/profile";
import type { Answer } from "../api/types";
import { messageOf, useAsync } from "../api/useAsync";
import { Badge, Banner, Card, Empty, Load, Page, fmtDate } from "../components/ui";

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
      <p className="muted"><Badge>{a.confidence ?? "insufficient"} confidence</Badge> {a.uncertainty}</p>
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
          <button className={`btn ${rating === "helpful" ? "" : "secondary"}`} onClick={() => void rate("helpful")} aria-label="Helpful">👍</button>
          <button className={`btn ${rating === "not_helpful" ? "" : "secondary"}`} onClick={() => void rate("not_helpful")} aria-label="Not helpful">👎</button>
          {onChange && <button className="btn secondary" onClick={async () => { await del(`/profiles/${profileId}/questions/${a.id}`); onChange(); }}>Delete</button>}
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
  const history = useAsync(() => get<Answer[]>(`/profiles/${id}/questions`), [id]);

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
    <Page title="Ask CORTEX" subtitle="Answers are grounded in your records and cite their sources">
      <Card>
        <form className="stack" onSubmit={(e) => { e.preventDefault(); if (text.trim()) void ask(text.trim()); }}>
          <label htmlFor="q">Your question</label>
          <textarea id="q" rows={3} value={text} onChange={(e) => setText(e.target.value)} placeholder="How has my HbA1c changed this year?" />
          <button className="btn" disabled={busy || !text.trim()}>{busy ? "Thinking…" : "Ask"}</button>
        </form>
      </Card>
      {error && <Banner tone="danger">{error}</Banner>}
      {latest && (
        <>
          <AnswerCard a={latest} profileId={id} />
          {latest.suggested_questions?.map((s) => (
            <button key={s} className="btn secondary" style={{ margin: "0 .5rem .5rem 0" }} onClick={() => void ask(s)}>{s}</button>
          ))}
        </>
      )}
      <h2 style={{ marginTop: "1.5rem" }}>Previous questions</h2>
      <Load q={history}>
        {(items) => (items.length === 0 ? <Empty>No saved questions yet.</Empty> : items.map((a) => <AnswerCard key={a.id} a={a} profileId={id} onChange={history.reload} />))}
      </Load>
    </Page>
  );
}
