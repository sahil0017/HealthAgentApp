import { useState } from "react";
import { del, get, listSessions, put, revokeSession, signOutEverywhere } from "../api/client";
import { useProfile } from "../api/profile";
import type { AccessEvent } from "../api/types";
import { messageOf, useAsync } from "../api/useAsync";
import { Banner, Card, Empty, Load, Page, fmtDate, humanize } from "../components/ui";

interface ConsentRecord { purpose: string; granted: boolean }

const PURPOSES: Record<string, string> = {
  health_data: "Store my health data",
  medical_record_storage: "Store my medical documents",
  document_processing: "Read and extract my documents",
  ai_processing: "Use AI to answer questions about my records",
  research: "Let Sehat research medical literature for me",
  notifications: "Send me reminders",
  data_sharing: "Share my data with people I choose",
};

const DEVICE_NAMES: Record<string, string> = { web: "Web browser", ios: "iPhone or iPad" };

export function Privacy() {
  const { profile, refresh } = useProfile();
  const id = profile!.id;
  const consents = useAsync(() => get<ConsentRecord[]>(`/profiles/${id}/consents`), [id]);
  const access = useAsync(() => get<AccessEvent[]>(`/profiles/${id}/access-history`), [id]);
  const devices = useAsync(() => listSessions(), []);
  const [error, setError] = useState<string | null>(null);
  const [confirm, setConfirm] = useState("");
  const [showAll, setShowAll] = useState(false);

  const [pending, setPending] = useState<Record<string, boolean>>({});

  async function toggle(purpose: string, granted: boolean) {
    setPending((p) => ({ ...p, [purpose]: granted }));
    try {
      await put(`/profiles/${id}/consents/${purpose}`, { granted, policy_version: "1.0" });
      consents.reload();
    } catch (e) {
      setError(messageOf(e));
    } finally {
      setPending((p) => {
        const { [purpose]: _drop, ...rest } = p;
        return rest;
      });
    }
  }

  async function endSession(sessionId: string) {
    try {
      await revokeSession(sessionId);
      devices.reload();
    } catch (e) {
      setError(messageOf(e));
    }
  }

  async function exportData() {
    try {
      const data = await get<unknown>(`/profiles/${id}/export`);
      const url = URL.createObjectURL(new Blob([JSON.stringify(data, null, 2)], { type: "application/json" }));
      const a = document.createElement("a");
      a.href = url;
      a.download = "cortex-export.json";
      a.click();
      URL.revokeObjectURL(url);
    } catch (e) {
      setError(messageOf(e));
    }
  }

  async function deleteAll() {
    try {
      await del(`/profiles/${id}`, { confirm: "DELETE" });
      await refresh();
    } catch (e) {
      setError(messageOf(e));
    }
  }

  return (
    <Page title="Privacy" subtitle="You decide what Sehat stores and what it may do with it">
      {error && <Banner tone="danger">{error}</Banner>}
      <Card title="Consent">
        <Load q={consents}>
          {(list) => (
            <>
              {Object.entries(PURPOSES).map(([purpose, label]) => {
                const on = pending[purpose] ?? list.find((c) => c.purpose === purpose)?.granted ?? false;
                return (
                  <label className="setting" key={purpose}>
                    <span>{label}</span>
                    <span className="switch">
                      <input type="checkbox" role="switch" checked={on} onChange={(e) => void toggle(purpose, e.target.checked)} aria-label={label} />
                      <span />
                    </span>
                  </label>
                );
              })}
            </>
          )}
        </Load>
      </Card>

      <Card title="Access history">
        <Load q={access}>
          {(events) => {
            if (events.length === 0) return <Empty title="No activity yet">Every time your records are viewed or changed, it is listed here.</Empty>;
            const shown = events.slice(0, showAll ? 100 : 6);
            return (
              <>
                {shown.map((e) => (
                  <div className="row" key={e.id}>
                    <div><b>{humanize(e.action.replace(/[._]/g, " "))}</b><p className="muted">{humanize(e.resource_type)}</p></div>
                    <span className="muted">{fmtDate(e.created_at)}</span>
                  </div>
                ))}
                {events.length > 6 && <button className="btn ghost small" onClick={() => setShowAll(!showAll)}>{showAll ? "Show fewer" : `Show ${Math.min(events.length, 100) - 6} more`}</button>}
              </>
            );
          }}
        </Load>
      </Card>

      <Card title="Signed-in devices">
        <Load q={devices}>
          {(list) => (
            <>
              {list.map((d) => (
                <div className="row" key={d.id}>
                  <div>
                    <b>{DEVICE_NAMES[d.device] ?? "Other device"}{d.current ? " (this device)" : ""}</b>
                    <p className="muted">Last active {fmtDate(d.last_seen_at)}</p>
                  </div>
                  {!d.current && <button className="btn secondary" onClick={() => void endSession(d.id)}>Sign out</button>}
                </div>
              ))}
              {list.length > 1 && (
                <div style={{ marginTop: ".75rem" }}>
                  <button className="btn danger" onClick={() => void signOutEverywhere().catch((e) => setError(messageOf(e)))}>Sign out everywhere</button>
                </div>
              )}
            </>
          )}
        </Load>
      </Card>

      <Card title="Your data">
        <div className="stack">
          <div><button className="btn secondary" onClick={() => void exportData()}>Export everything (JSON)</button></div>
          <div>
            <label htmlFor="del">Type DELETE to permanently erase this profile and all its records</label>
            <div className="inline">
              <input id="del" value={confirm} onChange={(e) => setConfirm(e.target.value)} style={{ flex: 1 }} />
              <button className="btn danger" disabled={confirm !== "DELETE"} onClick={() => void deleteAll()}>Delete profile</button>
            </div>
          </div>
        </div>
      </Card>
    </Page>
  );
}
