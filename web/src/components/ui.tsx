import type { ReactNode } from "react";
import type { Async } from "../api/useAsync";

export function Page({ title, subtitle, children }: { title: string; subtitle?: string; children: ReactNode }) {
  return (
    <div className="page">
      <h1>{title}</h1>
      {subtitle && <p className="muted">{subtitle}</p>}
      <div style={{ height: ".9rem" }} />
      {children}
      <p className="disclaimer">CORTEX organises your records and explains them. It does not diagnose or replace a clinician.</p>
    </div>
  );
}

export function Card({ title, children }: { title?: string; children: ReactNode }) {
  return (
    <section className="card">
      {title && <h2>{title}</h2>}
      {children}
    </section>
  );
}

export function Badge({ tone, children }: { tone?: "warn" | "danger"; children: ReactNode }) {
  return <span className={`badge ${tone ?? ""}`}>{children}</span>;
}

export function Banner({ tone, children }: { tone: "danger" | "warn" | "info"; children: ReactNode }) {
  return <div role={tone === "danger" ? "alert" : "status"} className={`banner ${tone}`}>{children}</div>;
}

export function Empty({ children }: { children: ReactNode }) {
  return <p className="muted">{children}</p>;
}

// Renders loading and error states, then the content once data has arrived.
export function Load<T>({ q, children }: { q: Async<T>; children: (data: T) => ReactNode }) {
  if (q.loading && !q.data) return <p className="muted">Loading…</p>;
  if (q.error) {
    return (
      <Banner tone="danger">
        {q.error} <button className="btn secondary" onClick={q.reload}>Retry</button>
      </Banner>
    );
  }
  return <>{q.data !== null ? children(q.data) : null}</>;
}

export const fmtDate = (s?: string | null) => (s ? new Date(s).toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" }) : "—");
export const fmtNum = (n: number | null | undefined) => (n == null ? "—" : Number.isInteger(n) ? String(n) : n.toFixed(2).replace(/0+$/, ""));
