import type { TrendPoint } from "../api/types";

const W = 600;
const H = 220;
const PAD = { l: 44, r: 12, t: 12, b: 28 };

// Plain SVG line chart: no charting dependency, scales with its container.
export function TrendChart({ points, unit, low, high }: { points: TrendPoint[]; unit: string; low?: number | null; high?: number | null }) {
  if (points.length === 0) return <p className="muted">No verified readings to chart.</p>;
  const xs = points.map((p) => new Date(p.observed_at).getTime());
  const ys = points.map((p) => p.value);
  const lo = Math.min(...ys, low ?? Infinity);
  const hi = Math.max(...ys, high ?? -Infinity);
  const yMin = lo === hi ? lo - 1 : lo - (hi - lo) * 0.1;
  const yMax = lo === hi ? hi + 1 : hi + (hi - lo) * 0.1;
  const xMin = Math.min(...xs);
  const xSpan = Math.max(...xs) - xMin || 1;
  const x = (t: number) => PAD.l + (xs.length === 1 ? (W - PAD.l - PAD.r) / 2 : ((t - xMin) / xSpan) * (W - PAD.l - PAD.r));
  const y = (v: number) => PAD.t + (1 - (v - yMin) / (yMax - yMin)) * (H - PAD.t - PAD.b);
  const path = points.map((p, i) => `${i ? "L" : "M"}${x(xs[i]).toFixed(1)},${y(p.value).toFixed(1)}`).join(" ");
  const label = (v: number) => (Math.abs(v) >= 100 ? v.toFixed(0) : v.toFixed(1));

  return (
    <svg className="chart" viewBox={`0 0 ${W} ${H}`} role="img" aria-label={`Trend chart in ${unit}, ${points.length} readings`}>
      {low != null && high != null && (
        <rect x={PAD.l} width={W - PAD.l - PAD.r} y={y(high)} height={Math.max(0, y(low) - y(high))} fill="#0f766e" opacity={0.1}>
          <title>{`Reference range ${low}–${high} ${unit}`}</title>
        </rect>
      )}
      {[yMin, (yMin + yMax) / 2, yMax].map((v) => (
        <g key={v}>
          <line x1={PAD.l} x2={W - PAD.r} y1={y(v)} y2={y(v)} stroke="#e1e7eb" />
          <text x={PAD.l - 6} y={y(v) + 4} fontSize="11" textAnchor="end" fill="#5b6b78">{label(v)}</text>
        </g>
      ))}
      <path d={path} fill="none" stroke="#0f766e" strokeWidth={2} />
      {points.map((p, i) => (
        <circle key={p.observation_id} cx={x(xs[i])} cy={y(p.value)} r={4} fill={p.flag === "normal" || !p.flag ? "#0f766e" : "#b45309"}>
          <title>{`${new Date(p.observed_at).toLocaleDateString()}: ${p.value} ${unit}`}</title>
        </circle>
      ))}
      <text x={PAD.l} y={H - 8} fontSize="11" fill="#5b6b78">{new Date(xMin).toLocaleDateString()}</text>
      <text x={W - PAD.r} y={H - 8} fontSize="11" textAnchor="end" fill="#5b6b78">{new Date(xMin + xSpan).toLocaleDateString()}</text>
    </svg>
  );
}
