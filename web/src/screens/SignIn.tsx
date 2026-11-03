import { useEffect, useMemo, useRef, useState } from "react";
import { authConfig, signIn, type AuthConfig, type Provider } from "../api/client";
import { appleIdToken, loadSdk, newNonce, renderGoogleButton } from "../api/identity";
import { messageOf } from "../api/useAsync";
import { Banner } from "../components/ui";

export function SignIn() {
  const [cfg, setCfg] = useState<AuthConfig | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const nonce = useMemo(newNonce, []);
  const googleEl = useRef<HTMLDivElement>(null);

  const google = cfg?.providers.find((p) => p.name === "google");
  const apple = cfg?.providers.find((p) => p.name === "apple");

  async function finish(provider: Provider, idToken: string, usedNonce: string) {
    setBusy(true);
    setError(null);
    try {
      await signIn(provider, idToken, usedNonce);
    } catch (e) {
      setError(messageOf(e));
      setBusy(false);
    }
  }

  useEffect(() => {
    authConfig().then(setCfg, (e) => setError(messageOf(e)));
  }, []);

  useEffect(() => {
    if (!google) return;
    loadSdk("google")
      .then(() => googleEl.current && renderGoogleButton(googleEl.current, google.client_id, nonce, (t) => void finish("google", t, nonce)))
      .catch((e) => setError(messageOf(e)));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [google?.client_id, nonce]);

  async function withApple() {
    if (!apple) return;
    // Apple's popup is a separate attempt from the Google button, so it gets its own nonce.
    const n = newNonce();
    try {
      await loadSdk("apple");
      await finish("apple", await appleIdToken(apple.client_id, n), n);
    } catch (e) {
      // Closing the Apple popup is a cancellation, not an error worth showing.
      const code = (e as { error?: string })?.error;
      if (code !== "popup_closed_by_user" && code !== "user_cancelled_authorize") setError(messageOf(e));
    }
  }

  return (
    <div className="center">
      <div className="card stack" style={{ textAlign: "center" }}>
        <h1>CORTEX</h1>
        <p className="muted">Your personal health records, organised and explained.</p>
        {error && <Banner tone="danger">{error}</Banner>}
        {cfg && cfg.providers.length === 0 && <Banner tone="warn">Sign-in is not set up on this server yet.</Banner>}
        {!cfg && !error && <p className="muted">Loading…</p>}
        <div style={{ display: "grid", justifyContent: "center", gap: ".75rem", opacity: busy ? 0.5 : 1, pointerEvents: busy ? "none" : "auto" }}>
          {google && <div ref={googleEl} />}
          {apple && <button className="btn apple" onClick={() => void withApple()}>Continue with Apple</button>}
        </div>
        <p className="muted" style={{ fontSize: ".78rem" }}>We never see your password. Your records are saved to your account.</p>
      </div>
    </div>
  );
}
