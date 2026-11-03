import { useState, useSyncExternalStore } from "react";
import { Route, Routes } from "react-router-dom";
import { useEffect } from "react";
import { auth, restoreSession } from "./api/client";
import { ProfileProvider, useProfile } from "./api/profile";
import { messageOf } from "./api/useAsync";
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
        <h1>Create your health profile</h1>
        {error && <Banner tone="danger">{error}</Banner>}
        <div>
          <label htmlFor="pname">Name</label>
          <input id="pname" value={name} onChange={(e) => setName(e.target.value)} />
        </div>
        <button className="btn" disabled={!name.trim()}>Create profile</button>
      </form>
    </div>
  );
}

function Authed() {
  const { profile, loading } = useProfile();
  if (loading) return <p className="muted" style={{ padding: "2rem" }}>Loading…</p>;
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
