import { NavLink, Outlet } from "react-router-dom";
import { signOut } from "../api/client";
import { useProfile } from "../api/profile";

export const TABS = [
  { to: "/", label: "Today", icon: "◉" },
  { to: "/documents", label: "Vault", icon: "▤" },
  { to: "/biomarkers", label: "Biomarkers", icon: "∿" },
  { to: "/ask", label: "Ask AI", icon: "✦" },
  { to: "/timeline", label: "Timeline", icon: "≡" },
  { to: "/privacy", label: "Privacy", icon: "⛨" },
];

// One set of destinations: a sidebar on wide screens, a bottom tab bar on narrow ones (CSS only).
export function Shell() {
  const { profiles, profile, select } = useProfile();
  return (
    <div className="shell">
      <nav className="nav" aria-label="Primary">
        <div className="brand">CORTEX</div>
        {TABS.map((t) => (
          <NavLink key={t.to} to={t.to} end={t.to === "/"} className={({ isActive }) => (isActive ? "active" : "")}>
            <span className="icon" aria-hidden>{t.icon}</span>
            <span>{t.label}</span>
          </NavLink>
        ))}
        <div className="side-extra">
          {profiles.length > 1 && (
            <select aria-label="Profile" value={profile?.id} onChange={(e) => select(e.target.value)}>
              {profiles.map((p) => <option key={p.id} value={p.id}>{p.display_name}</option>)}
            </select>
          )}
          <button className="btn secondary" onClick={() => void signOut()}>Sign out</button>
        </div>
      </nav>
      <main className="content"><Outlet /></main>
    </div>
  );
}
