import type { ReactNode } from "react";
import type { Async } from "../api/useAsync";

export function Page({ title, subtitle, children }: { title: string; subtitle?: string; children: ReactNode }) {
  return (
    <div className="page">
      <div className="page-head">
        <h1>{title}</h1>
        {subtitle && <p className="muted">{subtitle}</p>}
      </div>
      {children}
      <p className="disclaimer">Sehat organises your records and explains them. It does not diagnose or replace a clinician.</p>
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

export function Badge({ tone, children }: { tone?: "warn" | "danger" | "ok" | "neutral"; children: ReactNode }) {
  return <span className={`badge ${tone ?? ""}`}>{children}</span>;
}

export function Banner({ tone, children }: { tone: "danger" | "warn" | "info"; children: ReactNode }) {
  return <div role={tone === "danger" ? "alert" : "status"} className={`banner ${tone}`}>{children}</div>;
}

export function Empty({ title, children }: { title?: string; children?: ReactNode }) {
  return <div className="empty">{title && <b>{title}</b>}{children && <p>{children}</p>}</div>;
}

export function Skeleton() {
  return (
    <div aria-busy="true" aria-label="Loading">
      <div className="skeleton tall" />
      <div className="skeleton tall" />
      <div className="skeleton" style={{ width: "60%" }} />
    </div>
  );
}

// Renders loading and error states, then the content once data has arrived.
export function Load<T>({ q, children }: { q: Async<T>; children: (data: T) => ReactNode }) {
  if (q.loading && !q.data) return <Skeleton />;
  if (q.error) {
    return (
      <Banner tone="danger">
        <div className="inline" style={{ justifyContent: "space-between" }}>
          <span>{q.error}</span>
          <button className="btn secondary small" onClick={q.reload}>Try again</button>
        </div>
      </Banner>
    );
  }
  return <>{q.data !== null ? children(q.data) : null}</>;
}

export const fmtDate = (s?: string | null) => (s ? new Date(s).toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" }) : "—");
export const fmtNum = (n: number | null | undefined) => (n == null ? "—" : Number.isInteger(n) ? String(n) : n.toFixed(2).replace(/0+$/, ""));

const SMALL = new Set(["and", "of", "the"]);
// "kidney_function" / "OTHER" -> "Kidney function" / "Other".
export const humanize = (s?: string | null) => {
  const t = (s ?? "").replace(/_/g, " ").trim().toLowerCase();
  return t ? t.charAt(0).toUpperCase() + t.slice(1) : "";
};
export const plural = (n: number, one: string, many = one + "s") => `${n} ${n === 1 ? one : many}`;
export const titleCase = (s: string) => s.split(" ").map((w) => (SMALL.has(w) ? w : w.charAt(0).toUpperCase() + w.slice(1).toLowerCase())).join(" ");
