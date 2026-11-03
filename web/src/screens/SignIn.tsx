import { useState } from "react";
import { messageOf } from "../api/useAsync";
import { signIn } from "../api/client";
import { Banner } from "../components/ui";

export function SignIn() {
  const [token, setToken] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await signIn(token.trim());
    } catch (err) {
      setError(messageOf(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="center">
      <form className="card stack" onSubmit={submit}>
        <h1>CORTEX</h1>
        <p className="muted">Your personal health records, organised and explained.</p>
        {error && <Banner tone="danger">{error}</Banner>}
        <div>
          <label htmlFor="token">Access token</label>
          <input id="token" type="password" value={token} onChange={(e) => setToken(e.target.value)} autoComplete="off" />
        </div>
        <button className="btn" disabled={busy || !token.trim()}>{busy ? "Signing in…" : "Sign in"}</button>
      </form>
    </div>
  );
}
