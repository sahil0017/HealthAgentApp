import { useState } from "react";
import { del, get, patch, post } from "../api/client";
import { useProfile } from "../api/profile";
import type { LifestyleCategory, LifestyleCorrelations, LifestyleLog, LifestyleSummary, Symptom } from "../api/types";
import { messageOf, useAsync } from "../api/useAsync";
import { Badge, Banner, Card, Empty, Load, Page, fmtDate, fmtNum, humanize } from "../components/ui";

function AddSymptom({ profileId, onDone }: { profileId: string; onDone: () => void }) {
  const [name, setName] = useState("");
  const [severity, setSeverity] = useState(3);
  const [error, setError] = useState<string | null>(null);
  return (
    <form
      className="stack"
      onSubmit={async (e) => {
        e.preventDefault();
        try {
          await post(`/profiles/${profileId}/symptoms`, { name: name.trim(), severity });
          setName("");
          setError(null);
          onDone();
        } catch (err) {
          setError(messageOf(err));
        }
      }}
    >
      {error && <Banner tone="danger">{error}</Banner>}
      <label htmlFor="symptom-name">What are you feeling?</label>
      <input id="symptom-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="For example, headache" maxLength={120} />
      <label htmlFor="symptom-severity">How strong is it? {severity} of 10</label>
      <input id="symptom-severity" type="range" min={1} max={10} value={severity} onChange={(e) => setSeverity(Number(e.target.value))} />
      <button className="btn" disabled={!name.trim()}>Log symptom</button>
    </form>
  );
}

function AddLog({ profileId, categories, onDone }: { profileId: string; categories: LifestyleCategory[]; onDone: () => void }) {
  const [category, setCategory] = useState("sleep");
  const [value, setValue] = useState("");
  const [error, setError] = useState<string | null>(null);
  const current = categories.find((c) => c.code === category);
  return (
    <form
      className="stack"
      onSubmit={async (e) => {
        e.preventDefault();
        try {
          await post(`/profiles/${profileId}/lifestyle-logs`, { category, value: Number(value) });
          setValue("");
          setError(null);
          onDone();
        } catch (err) {
          setError(messageOf(err));
        }
      }}
    >
      {error && <Banner tone="danger">{error}</Banner>}
      <label htmlFor="log-category">What are you logging?</label>
      <select id="log-category" value={category} onChange={(e) => setCategory(e.target.value)}>
        {categories.map((c) => <option key={c.code} value={c.code}>{c.name}</option>)}
      </select>
      <label htmlFor="log-value">Amount{current ? ` (${current.unit})` : ""}</label>
      <input id="log-value" type="number" step="any" min={current?.min} max={current?.max} value={value} onChange={(e) => setValue(e.target.value)} />
      <button className="btn" disabled={value === "" || Number.isNaN(Number(value))}>Log for today</button>
    </form>
  );
}

export function Lifestyle() {
  const { profile } = useProfile();
  const id = profile!.id;
  const symptoms = useAsync(() => get<Symptom[]>(`/profiles/${id}/symptoms?limit=20`), [id]);
  const logs = useAsync(() => get<LifestyleLog[]>(`/profiles/${id}/lifestyle-logs?limit=20`), [id]);
  const categories = useAsync(() => get<LifestyleCategory[]>("/lifestyle-categories"), []);
  const summary = useAsync(() => get<LifestyleSummary>(`/profiles/${id}/lifestyle/summary?days=30`), [id]);
  const correlations = useAsync(() => get<LifestyleCorrelations>(`/profiles/${id}/lifestyle/correlations?days=90`), [id]);
  const refresh = () => {
    symptoms.reload();
    logs.reload();
    summary.reload();
    correlations.reload();
  };

  return (
    <Page title="Lifestyle & symptoms" subtitle="Track how you feel and what your days look like">
      <div className="grid">
        <Card title="Log a symptom"><AddSymptom profileId={id} onDone={refresh} /></Card>
        <Card title="Log your day">
          <Load q={categories}>{(list) => <AddLog profileId={id} categories={list} onDone={refresh} />}</Load>
        </Card>
      </div>

      <Card title="Symptom journal">
        <Load q={symptoms}>
          {(list) => (
            <>
              {list.length === 0 && <Empty title="Nothing logged yet">Log a symptom above and it appears here.</Empty>}
              {list.map((s) => (
                <div className="row" key={s.id}>
                  <div><b>{s.name}</b><p className="muted">{fmtDate(s.started_at)} · severity {s.severity} of 10</p></div>
                  <div>
                    {s.active ? (
                      <button className="btn secondary small" onClick={async () => { await patch(`/profiles/${id}/symptoms/${s.id}`, { ended_at: new Date().toISOString() }); refresh(); }}>Mark resolved</button>
                    ) : <Badge tone="ok">Resolved</Badge>}{" "}
                    <button className="btn ghost small" aria-label={`Delete ${s.name}`} onClick={async () => { await del(`/profiles/${id}/symptoms/${s.id}`); refresh(); }}>Delete</button>
                  </div>
                </div>
              ))}
            </>
          )}
        </Load>
      </Card>

      <Card title="Last 30 days">
        <Load q={summary}>
          {(s) => (
            <>
              {s.categories.every((c) => c.days_logged === 0) && <Empty title="No daily logs yet">Sleep, exercise, food, water, mood and stress show up here.</Empty>}
              {s.categories.filter((c) => c.days_logged > 0).map((c) => (
                <div className="row" key={c.category}>
                  <div><b>{c.name}</b><p className="muted">{c.days_logged} {c.days_logged === 1 ? "day" : "days"} logged</p></div>
                  <div>{fmtNum(c.average_per_day)} {c.unit} a day</div>
                </div>
              ))}
            </>
          )}
        </Load>
      </Card>

      <Card title="Patterns">
        <Load q={correlations}>
          {(c) => (
            <>
              {c.correlations.filter((r) => r.status === "ok").length === 0 && <Empty title="Not enough data yet">Log a few weeks of symptoms and daily habits to see patterns.</Empty>}
              {c.correlations.filter((r) => r.status === "ok").map((r) => (
                <div className="row" key={r.category}>
                  <div><b>{r.name}</b><p className="muted">{r.description}</p></div>
                  <Badge tone="neutral">{humanize(r.strength)}</Badge>
                </div>
              ))}
              <p className="muted">{c.note}</p>
            </>
          )}
        </Load>
      </Card>

      <Card title="Recent daily logs">
        <Load q={logs}>
          {(list) => (
            <>
              {list.length === 0 && <Empty title="No entries yet" />}
              {list.map((l) => (
                <div className="row" key={l.id}>
                  <div><b>{humanize(l.category)}</b><p className="muted">{fmtDate(l.logged_on)}</p></div>
                  <div>{fmtNum(l.value)} {l.unit} <button className="btn ghost small" aria-label={`Delete ${l.category} entry`} onClick={async () => { await del(`/profiles/${id}/lifestyle-logs/${l.id}`); refresh(); }}>Delete</button></div>
                </div>
              ))}
            </>
          )}
        </Load>
      </Card>
    </Page>
  );
}
