import { useState } from "react";
import { get } from "../api/client";
import { useProfile } from "../api/profile";
import type { BiomarkerSummary, BiomarkerTrend, PanelCoverage } from "../api/types";
import { useAsync } from "../api/useAsync";
import { TrendChart } from "../components/TrendChart";
import { Badge, Card, Empty, Load, Page, fmtDate, fmtNum, humanize, plural } from "../components/ui";

const TREND: Record<string, string> = { rising: "Going up", falling: "Going down", stable: "Steady", insufficient_data: "Not enough readings to see a trend" };
const trendText = (d: string) => TREND[d] ?? humanize(d);
const FLAGS: Record<string, string> = { low: "Below range", high: "Above range", critical_low: "Well below range", critical_high: "Well above range", normal: "In range" };
const flagText = (f?: string | null) => (f && f !== "unknown" ? FLAGS[f] ?? humanize(f) : "No range to compare");
const flagTone = (f?: string | null) => (f === "normal" ? "ok" : f && f !== "unknown" ? "warn" : "neutral");

function Detail({ code, onBack }: { code: string; onBack: () => void }) {
  const { profile } = useProfile();
  const id = profile!.id;
  const q = useAsync(() => get<BiomarkerTrend>(`/profiles/${id}/biomarkers/${code}/trend`), [id, code]);
  return (
    <Page title="Biomarker trend">
      <p><button className="btn ghost small" onClick={onBack}>← All biomarkers</button></p>
      <Load q={q}>
        {(t) => {
          const range = t.points.find((p) => p.range)?.range;
          return (
            <>
              <Card title={t.display_name}>
                <p className="muted">{trendText(t.trend.direction)} · {plural(t.trend.points, "verified reading")} over {plural(t.trend.span_days, "day")} · measured in {t.canonical_unit}</p>
                <TrendChart points={t.points} unit={t.canonical_unit} low={range?.low} high={range?.high} />
                {t.unverified_count > 0 && <p className="muted">{plural(t.unverified_count, "reading")} still waiting for your confirmation {t.unverified_count === 1 ? "is" : "are"} not shown.</p>}
              </Card>
              <Card title="Readings">
                {[...t.points].reverse().map((p) => (
                  <div className="row" key={p.observation_id}>
                    <div><b>{fmtNum(p.value)} {p.unit}</b><p className="muted">{fmtDate(p.observed_at)}</p></div>
                    <Badge tone={flagTone(p.flag)}>{flagText(p.flag)}</Badge>
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
          if (items.length === 0) return <Card><Empty title="No biomarkers yet">Upload a lab report in the Vault and confirm its values. They will appear here with trends.</Empty></Card>;
          const groups = new Map<string, BiomarkerSummary[]>();
          items.forEach((b) => groups.set(b.category, [...(groups.get(b.category) ?? []), b]));
          return [...groups.entries()].map(([cat, list]) => (
            <Card title={humanize(cat)} key={cat}>
              {list.map((b) => (
                <button key={b.biomarker_code} className="row link" onClick={() => setCode(b.biomarker_code)}>
                  <div>
                    <b>{b.display_name}</b>
                    <p className="muted">{trendText(b.trend.direction)} · {plural(b.verified_readings, "reading")}{b.unverified_count > 0 ? ` · ${b.unverified_count} to confirm` : ""}</p>
                  </div>
                  <div className="right">
                    <b>{fmtNum(b.latest?.value)} {b.unit}</b>
                    <p><Badge tone={flagTone(b.latest?.flag)}>{flagText(b.latest?.flag)}</Badge></p>
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
                  <div><b>{p.label}</b><p className="muted">{p.days_since_last == null ? "Never recorded" : p.days_since_last === 0 ? "Today" : `${plural(p.days_since_last, "day")} ago`}{p.interval_days > 0 ? ` · usually every ${p.interval_days >= 330 ? "year" : `${Math.round(p.interval_days / 30)} months`}` : ""}</p></div>
                  <Badge tone={p.status === "overdue" ? "warn" : p.status === "current" ? "ok" : "neutral"}>{p.status === "never_measured" ? "Not tested yet" : humanize(p.status)}</Badge>
                </div>
              ))}
            </Card>
          ) : null
        }
      </Load>
    </Page>
  );
}
