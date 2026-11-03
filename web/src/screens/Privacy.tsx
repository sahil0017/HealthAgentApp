import { useState } from "react";
import { del, get, put } from "../api/client";
import { useProfile } from "../api/profile";
import type { AccessEvent } from "../api/types";
import { messageOf, useAsync } from "../api/useAsync";
import { Banner, Card, Empty, Load, Page, fmtDate } from "../components/ui";

interface ConsentRecord { purpose: string; granted: boolean }

const PURPOSES: Record<string, string> = {
  health_data: "Store my health data",
  medical_record_storage: "Store my medical documents",
  document_processing: "Read and extract my documents",
  ai_processing: "Use AI to answer questions about my records",
  research: "Let CORTEX research medical literature for me",
  notifications: "Send me reminders",
  data_sharing: "Share my data with people I choose",
};

export function Privacy() {
  const { profile, refresh } = useProfile();
  const id = profile!.id;
  const consents = useAsync(() => get<ConsentRecord[]>(`/profiles/${id}/consents`), [id]);
  const access = useAsync(() => get<AccessEvent[]>(`/profiles/${id}/access-history`), [id]);
  const [error, setError] = useState<string | null>(null);
  const [confirm, setConfirm] = useState("");

  async function toggle(purpose: string, granted: boolean) {
    try {
      await put(`/profiles/${id}/consents/${purpose}`, { granted, policy_version: "1.0" });
      consents.reload();
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
    <Page title="Privacy centre" subtitle="You control what CORTEX stores and what it may do with it">
      {error && <Banner tone="danger">{error}</Banner>}
      <Card title="Consent">
        <Load q={consents}>
          {(list) => (
            <>
              {Object.entries(PURPOSES).map(([purpose, label]) => {
                const on = list.find((c) => c.purpose === purpose)?.granted ?? false;
                return (
                  <label className="row" key={purpose} style={{ color: "inherit", fontSize: "1rem", margin: 0 }}>
                    <span>{label}</span>
                    <input type="checkbox" role="switch" checked={on} onChange={(e) => void toggle(purpose, e.target.checked)} style={{ width: 24, minHeight: 24 }} aria-label={label} />
                  </label>
                );
              })}
            </>
          )}
        </Load>
      </Card>

      <Card title="Access history">
        <Load q={access}>
          {(events) => (events.length === 0 ? <Empty>No activity yet.</Empty> : events.slice(0, 50).map((e) => (
            <div className="row" key={e.id}>
              <div><b>{e.action.replace(/[._]/g, " ")}</b><p className="muted">{e.resource_type}</p></div>
              <span className="muted">{fmtDate(e.created_at)}</span>
            </div>
          )))}
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
