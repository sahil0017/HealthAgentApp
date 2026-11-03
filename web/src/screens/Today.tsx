import { useState } from "react";
import { get, patch, post } from "../api/client";
import { useProfile } from "../api/profile";
import type { Overview } from "../api/types";
import { messageOf, useAsync } from "../api/useAsync";
import { Badge, Banner, Card, Empty, Load, Page, fmtDate, fmtNum } from "../components/ui";

function AddFollowUp({ profileId, onDone }: { profileId: string; onDone: () => void }) {
  const [title, setTitle] = useState("");
  const [due, setDue] = useState("");
  const [error, setError] = useState<string | null>(null);
  return (
    <form
      className="stack"
      onSubmit={async (e) => {
        e.preventDefault();
        try {
          await post(`/profiles/${profileId}/follow-ups`, { kind: "other", title: title.trim(), due_on: due || null });
          setTitle("");
          setDue("");
          setError(null);
          onDone();
        } catch (err) {
          setError(messageOf(err));
        }
      }}
    >
      {error && <Banner tone="danger">{error}</Banner>}
      <div className="inline">
        <input aria-label="Follow-up title" placeholder="e.g. Repeat HbA1c" value={title} onChange={(e) => setTitle(e.target.value)} style={{ flex: 2 }} />
        <input aria-label="Due date" type="date" value={due} onChange={(e) => setDue(e.target.value)} style={{ flex: 1 }} />
        <button className="btn" disabled={!title.trim()}>Add</button>
      </div>
    </form>
  );
}

export function Today() {
  const { profile } = useProfile();
  const id = profile!.id;
  const q = useAsync(() => get<Overview>(`/profiles/${id}/overview`), [id]);

  return (
    <Page title="Today" subtitle={profile!.display_name}>
      <Load q={q}>
        {(o) => (
          <>
            {o.attention.length > 0 && (
              <Card title="Needs your attention">
                {o.attention.map((a, i) => (
                  <div className="row" key={`${a.kind}-${a.ref_id ?? i}`}>
                    <div>
                      <b>{a.title}</b>
                      <p className="muted">{a.detail}</p>
                    </div>
                    <Badge tone={a.severity === "action" ? "warn" : undefined}>{a.severity === "action" ? "Action" : "Info"}</Badge>
                  </div>
                ))}
              </Card>
            )}

            <div className="stats">
              <div className="stat"><b>{o.summary.documents}</b>Documents</div>
              <div className="stat"><b>{o.summary.biomarkers_tracked}</b>Biomarkers</div>
              <div className="stat"><b>{o.summary.active_goals}</b>Goals</div>
              <div className="stat"><b>{o.summary.open_follow_ups}</b>Follow-ups</div>
            </div>

            <div className="grid">
              <Card title="Data completeness">
                <p><b>{o.completeness.score}%</b> complete</p>
                <div className="progress" role="progressbar" aria-valuenow={o.completeness.score} aria-valuemin={0} aria-valuemax={100}>
                  <div style={{ width: `${o.completeness.score}%` }} />
                </div>
                {o.completeness.items.filter((i) => !i.done).map((i) => (
                  <p className="muted" key={i.key}>○ {i.label} — {i.detail}</p>
                ))}
              </Card>

              <Card title="Recent changes">
                {o.recent_changes.length === 0 && <Empty>No verified changes yet.</Empty>}
                {o.recent_changes.map((c) => (
                  <div className="row" key={c.biomarker_code}>
                    <div><b>{c.name}</b><p className="muted">{fmtDate(c.observed_on)}</p></div>
                    <div>{fmtNum(c.value)} {c.unit}{c.change != null && <span className="muted"> ({c.change > 0 ? "+" : ""}{fmtNum(c.change)})</span>}</div>
                  </div>
                ))}
              </Card>
            </div>

            <Card title="Goals">
              {o.goals.length === 0 && <Empty>No goals yet.</Empty>}
              {o.goals.map((g) => (
                <div className="row" key={g.id}>
                  <div><b>{g.title}</b><p className="muted">Target {fmtNum(g.target_min)}–{fmtNum(g.target_max)} {g.unit}</p></div>
                  <Badge tone={g.progress.status === "on_target" ? undefined : "warn"}>{g.progress.status.replace(/_/g, " ")}</Badge>
                </div>
              ))}
            </Card>

            <Card title="Follow-ups">
              {o.reminders.length === 0 && <Empty>Nothing pending.</Empty>}
              {o.reminders.map((f) => (
                <div className="row" key={f.id}>
                  <div><b>{f.title}</b><p className="muted">Due {fmtDate(f.due_on)}</p></div>
                  <button className="btn secondary" onClick={async () => { await patch(`/follow-ups/${f.id}`, { status: "done" }); q.reload(); }}>Done</button>
                </div>
              ))}
              {o.suggested_follow_ups.map((s) => (
                <p className="muted" key={s.title}>Suggested: {s.title} — {s.reason}</p>
              ))}
              <AddFollowUp profileId={id} onDone={q.reload} />
            </Card>
          </>
        )}
      </Load>
    </Page>
  );
}
