import { useState } from "react";
import { get } from "../api/client";
import { useProfile } from "../api/profile";
import type { BiomarkerSummary, BiomarkerTrend, PanelCoverage } from "../api/types";
import { useAsync } from "../api/useAsync";
import { TrendChart } from "../components/TrendChart";
import { Badge, Card, Empty, Load, Page, fmtDate, fmtNum } from "../components/ui";

function Detail({ code, onBack }: { code: string; onBack: () => void }) {
  const { profile } = useProfile();
  const id = profile!.id;
  const q = useAsync(() => get<BiomarkerTrend>(`/profiles/${id}/biomarkers/${code}/trend`), [id, code]);
  return (
    <Page title="Biomarker trend">
      <button className="btn secondary" onClick={onBack}>← All biomarkers</button>
      <div style={{ height: ".9rem" }} />
      <Load q={q}>
        {(t) => {
          const range = t.points.find((p) => p.range)?.range;
          return (
            <>
              <Card title={t.display_name}>
                <p className="muted">{t.trend.direction.replace(/_/g, " ")} across {t.trend.points} verified readings over {t.trend.span_days} days · unit {t.canonical_unit}</p>
                <TrendChart points={t.points} unit={t.canonical_unit} low={range?.low} high={range?.high} />
                {t.unverified_count > 0 && <p className="muted">{t.unverified_count} unverified reading(s) are not shown.</p>}
              </Card>
              <Card title="Readings">
                {[...t.points].reverse().map((p) => (
                  <div className="row" key={p.observation_id}>
                    <div><b>{fmtNum(p.value)} {p.unit}</b><p className="muted">{fmtDate(p.observed_at)}</p></div>
                    <Badge tone={p.flag && p.flag !== "normal" ? "warn" : undefined}>{p.flag || "no range"}</Badge>
                  </div>
                ))}
              </Card>
            </>
          );
        }}
      </Load>
    </Page>
  );
}

export function Biomarkers() {
  const { profile } = useProfile();
  const id = profile!.id;
  const [code, setCode] = useState<string | null>(null);
  const summary = useAsync(() => get<BiomarkerSummary[]>(`/profiles/${id}/biomarkers/summary`), [id]);
  const coverage = useAsync(() => get<PanelCoverage[]>(`/profiles/${id}/biomarkers/coverage`), [id]);

  if (code) return <Detail code={code} onBack={() => setCode(null)} />;

  return (
    <Page title="Biomarkers" subtitle="Verified readings, grouped by panel">
      <Load q={summary}>
        {(items) => {
          if (items.length === 0) return <Card><Empty>Upload and confirm a report to see your biomarkers here.</Empty></Card>;
          const groups = new Map<string, BiomarkerSummary[]>();
          items.forEach((b) => groups.set(b.category, [...(groups.get(b.category) ?? []), b]));
          return [...groups.entries()].map(([cat, list]) => (
            <Card title={cat.toUpperCase()} key={cat}>
              {list.map((b) => (
                <button key={b.biomarker_code} className="row" style={{ width: "100%", background: "none", border: 0, textAlign: "left", cursor: "pointer" }} onClick={() => setCode(b.biomarker_code)}>
                  <div>
                    <b>{b.display_name}</b>
                    <p className="muted">{b.trend.direction.replace(/_/g, " ")} · {b.verified_readings} reading(s){b.unverified_count > 0 ? ` · ${b.unverified_count} unverified` : ""}</p>
                  </div>
                  <div style={{ textAlign: "right" }}>
                    <b>{fmtNum(b.latest?.value)} {b.unit}</b>
                    {b.latest?.flag && b.latest.flag !== "normal" && <p><Badge tone="warn">{b.latest.flag}</Badge></p>}
                  </div>
                </button>
              ))}
            </Card>
          ));
        }}
      </Load>
      <Load q={coverage}>
        {(panels) =>
          panels.length > 0 ? (
            <Card title="Test coverage">
              {panels.map((p) => (
                <div className="row" key={p.category}>
                  <div><b>{p.label}</b><p className="muted">{p.days_since_last == null ? "Never recorded" : `${p.days_since_last} days ago`} · every ~{p.interval_days} days</p></div>
                  <Badge tone={p.status === "overdue" || p.status === "missing" ? "warn" : undefined}>{p.status.replace(/_/g, " ")}</Badge>
                </div>
              ))}
            </Card>
          ) : null
        }
      </Load>
    </Page>
  );
}
