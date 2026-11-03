import { useState, useSyncExternalStore } from "react";
import { Route, Routes } from "react-router-dom";
import { useEffect } from "react";
import { auth, keepSessionFresh, restoreSession } from "./api/client";
import { ProfileProvider, useProfile } from "./api/profile";
import { messageOf } from "./api/useAsync";
import { Icon } from "./components/Icon";
import { Shell } from "./components/Shell";
import { Banner } from "./components/ui";
import { Ask } from "./screens/Ask";
import { Biomarkers } from "./screens/Biomarkers";
import { Documents } from "./screens/Documents";
import { Privacy } from "./screens/Privacy";
import { SignIn } from "./screens/SignIn";
import { Timeline } from "./screens/Timeline";
import { Today } from "./screens/Today";

function CreateProfile() {
  const { create } = useProfile();
  const [name, setName] = useState("");
  const [error, setError] = useState<string | null>(null);
  return (
    <div className="center">
      <form
        className="card stack"
        onSubmit={(e) => {
          e.preventDefault();
          create(name.trim()).catch((err) => setError(messageOf(err)));
        }}
      >
        <div className="brand"><span className="mark"><Icon name="heart" strokeWidth={2.4} /></span>Sehat</div>
        <h1 style={{ textAlign: "center" }}>Welcome. Let's set up your profile</h1>
        <p className="muted" style={{ textAlign: "center" }}>Your profile keeps your reports and results together. You can add family members later.</p>
        {error && <Banner tone="danger">{error}</Banner>}
        <div>
          <label htmlFor="pname">Whose records are these?</label>
          <input id="pname" value={name} onChange={(e) => setName(e.target.value)} placeholder="Your name" autoFocus autoComplete="name" />
        </div>
        <button className="btn" disabled={!name.trim()}>Continue</button>
      </form>
    </div>
  );
}

function Authed() {
  const { profile, loading } = useProfile();
  if (loading) return <div className="center"><div className="card" style={{ width: 280 }}><div className="skeleton" /><div className="skeleton" style={{ width: "60%" }} /></div></div>;
  if (!profile) return <CreateProfile />;
  return (
    <Routes>
      <Route element={<Shell />}>
        <Route index element={<Today />} />
        <Route path="documents" element={<Documents />} />
        <Route path="biomarkers" element={<Biomarkers />} />
        <Route path="ask" element={<Ask />} />
        <Route path="timeline" element={<Timeline />} />
        <Route path="privacy" element={<Privacy />} />
      </Route>
    </Routes>
  );
}

export function App() {
  const session = useSyncExternalStore(auth.subscribe, () => auth.state);
  useEffect(() => {
    if (session !== "in") return;
    return keepSessionFresh();
  }, [session]);
  useEffect(() => {
    void restoreSession();
  }, []);
  if (session === "unknown") return <p className="muted" style={{ padding: "2rem" }}>Loading…</p>;
  if (session === "out") return <SignIn />;
  return (
    <ProfileProvider>
      <Authed />
    </ProfileProvider>
  );
}
