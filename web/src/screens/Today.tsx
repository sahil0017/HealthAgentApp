import { useState } from "react";
import { Link } from "react-router-dom";
import { Icon } from "../components/Icon";
import { get, patch, post } from "../api/client";
import { useProfile } from "../api/profile";
import type { FollowUp, Overview } from "../api/types";
import { messageOf, useAsync } from "../api/useAsync";
import { Badge, Banner, Card, Empty, Load, Page, fmtDate, fmtNum, humanize } from "../components/ui";

const FIX_AT: Record<string, string> = { consent: "/privacy", documents: "/documents", reviewed: "/documents", verified: "/documents" };

function greeting(): string {
  const h = new Date().getHours();
  return h < 12 ? "Good morning" : h < 18 ? "Good afternoon" : "Good evening";
}

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
        <input aria-label="Follow-up title" placeholder="e.g. Repeat HbA1c" value={title} onChange={(e) => setTitle(e.target.value)} style={{ flex: "2 1 200px" }} />
        <input aria-label="Due date" type="date" value={due} onChange={(e) => setDue(e.target.value)} style={{ flex: "1 1 150px" }} />
        <button className="btn" disabled={!title.trim()}>Add</button>
      </div>
    </form>
  );
}

export function Today() {
  const { profile } = useProfile();
  const id = profile!.id;
  const q = useAsync(() => get<Overview>(`/profiles/${id}/overview`), [id]);
  const followUps = useAsync(() => get<FollowUp[]>(`/profiles/${id}/follow-ups`), [id]);
  const reload = () => {
    q.reload();
    followUps.reload();
  };

  return (
    <Page title={`${greeting()}, ${profile!.display_name.split(" ")[0]}`} subtitle="Here is where your health record stands today">
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
              <Link className="stat" to="/documents"><b>{o.summary.documents}</b>Documents</Link>
              <Link className="stat" to="/biomarkers"><b>{o.summary.biomarkers_tracked}</b>Biomarkers</Link>
              <div className="stat"><b>{o.summary.active_goals}</b>Goals</div>
              <div className="stat"><b>{o.summary.open_follow_ups}</b>Follow-ups</div>
            </div>

            <div className="grid">
              <Card title="Data completeness">
                <p><b>{o.completeness.score}%</b> complete</p>
                <div className="progress" role="progressbar" aria-valuenow={o.completeness.score} aria-valuemin={0} aria-valuemax={100}>
                  <div style={{ width: `${o.completeness.score}%` }} />
                </div>
                <ul className="checks">
                  {o.completeness.items.map((i) => (
                    <li key={i.key} className={i.done ? "done" : ""}>
                      <span className="dot">{i.done && <Icon name="check" width={12} height={12} strokeWidth={3} />}</span>
                      <span>
                        {i.label}
                        {!i.done && <span className="muted"> — {i.detail} {FIX_AT[i.key] && <Link to={FIX_AT[i.key]}>Do it now</Link>}</span>}
                      </span>
                    </li>
                  ))}
                </ul>
              </Card>

              <Card title="Recent changes">
                {o.recent_changes.length === 0 && <Empty title="No changes yet">Confirm values from a report and trends appear here.</Empty>}
                {o.recent_changes.map((c) => (
                  <div className="row" key={c.biomarker_code}>
                    <div><b>{c.name}</b><p className="muted">{fmtDate(c.observed_on)}</p></div>
                    <div>{fmtNum(c.value)} {c.unit}{c.change != null && <span className="muted"> ({c.change > 0 ? "+" : ""}{fmtNum(c.change)})</span>}</div>
                  </div>
                ))}
              </Card>
            </div>

            <Card title="Goals">
              {o.goals.length === 0 && <Empty title="No goals yet">Goals you set will show their progress here.</Empty>}
              {o.goals.map((g) => (
                <div className="row" key={g.id}>
                  <div><b>{g.title}</b><p className="muted">Target {fmtNum(g.target_min)}–{fmtNum(g.target_max)} {g.unit}</p></div>
                  <Badge tone={g.progress.status === "on_target" ? undefined : "warn"}>{humanize(g.progress.status)}</Badge>
                </div>
              ))}
            </Card>

            <Card title="Follow-ups">
              <Load q={followUps}>
                {(list) => {
                  const open = list.filter((f) => f.status === "open");
                  return (
                    <>
                      {open.length === 0 && <Empty title="Nothing pending">Add a reminder below, such as repeating a test.</Empty>}
                      {open.map((f) => (
                        <div className="row" key={f.id}>
                          <div><b>{f.title}</b><p className="muted">{f.due_on ? `Due ${fmtDate(f.due_on)}` : "No due date"}</p></div>
                          <button className="btn secondary small" onClick={async () => { await patch(`/follow-ups/${f.id}`, { status: "done" }); reload(); }}>Mark done</button>
                        </div>
                      ))}
                    </>
                  );
                }}
              </Load>
              {o.suggested_follow_ups.map((s) => (
                <p className="muted" key={s.title}>Suggested: {s.title} — {s.reason}</p>
              ))}
              <AddFollowUp profileId={id} onDone={reload} />
            </Card>
          </>
        )}
      </Load>
    </Page>
  );
}
