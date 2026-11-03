import { useEffect, useState } from "react";
import { NavLink, Outlet, useLocation } from "react-router-dom";
import { signOut } from "../api/client";
import { useProfile } from "../api/profile";
import { ErrorBoundary } from "./ErrorBoundary";
import { Icon, type IconName } from "./Icon";

export const TABS: { to: string; label: string; icon: IconName }[] = [
  { to: "/", label: "Today", icon: "today" },
  { to: "/documents", label: "Vault", icon: "vault" },
  { to: "/biomarkers", label: "Biomarkers", icon: "biomarkers" },
  { to: "/ask", label: "Ask AI", icon: "ask" },
  { to: "/timeline", label: "Timeline", icon: "timeline" },
  { to: "/privacy", label: "Privacy", icon: "privacy" },
];

const WIDE = "(min-width: 900px)";

function useWide() {
  const query = typeof window !== "undefined" && window.matchMedia ? window.matchMedia(WIDE) : null;
  const [wide, setWide] = useState(query?.matches ?? false);
  useEffect(() => {
    if (!query) return;
    const on = () => setWide(query.matches);
    query.addEventListener("change", on);
    return () => query.removeEventListener("change", on);
  }, [query]);
  return wide;
}

function Brand() {
  return <div className="brand"><span className="mark"><Icon name="heart" strokeWidth={2.4} /></span>Sehat</div>;
}

function ProfilePicker() {
  const { profiles, profile, select } = useProfile();
  if (profiles.length < 2) return null;
  return (
    <select aria-label="Profile" value={profile?.id} onChange={(e) => select(e.target.value)}>
      {profiles.map((p) => <option key={p.id} value={p.id}>{p.display_name}</option>)}
    </select>
  );
}

// One set of destinations: a sidebar on wide screens, a top bar plus bottom tab bar on narrow ones.
// Profile switching and sign-out live in the sidebar on desktop and in the top bar on phones.
export function Shell() {
  const { pathname } = useLocation();
  const wide = useWide();
  return (
    <div className="shell">
      {!wide && (
        <header className="topbar">
          <Brand />
          <div className="who">
            <ProfilePicker />
            <button className="btn ghost small" onClick={() => void signOut()} aria-label="Sign out"><Icon name="logout" width={18} height={18} /></button>
          </div>
        </header>
      )}
      <nav className="nav" aria-label="Primary">
        {wide && <Brand />}
        {TABS.map((t) => (
          <NavLink key={t.to} to={t.to} end={t.to === "/"} className={({ isActive }) => (isActive ? "active" : "")}>
            <Icon name={t.icon} />
            <span>{t.label}</span>
          </NavLink>
        ))}
        {wide && (
          <div className="side-extra">
            <ProfilePicker />
            <button className="btn secondary" onClick={() => void signOut()}>Sign out</button>
          </div>
        )}
      </nav>
      <main className="content"><ErrorBoundary resetKey={pathname}><Outlet /></ErrorBoundary></main>
    </div>
  );
}
