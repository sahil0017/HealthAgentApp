import type { Provider } from "./client";

declare global {
  interface Window {
    google?: {
      accounts: {
        id: {
          initialize(cfg: { client_id: string; nonce: string; callback: (r: { credential: string }) => void }): void;
          renderButton(el: HTMLElement, opts: Record<string, unknown>): void;
        };
      };
    };
    AppleID?: {
      auth: {
        init(cfg: { clientId: string; scope: string; redirectURI: string; usePopup: boolean; nonce: string }): void;
        signIn(): Promise<{ authorization: { id_token: string } }>;
      };
    };
  }
}

const SDK: Record<Provider, { src: string; ready: () => boolean }> = {
  google: { src: "https://accounts.google.com/gsi/client", ready: () => !!window.google?.accounts?.id },
  apple: { src: "https://appleid.cdn-apple.com/appleauth/static/jsapi/appleid/1/en_US/appleid.auth.js", ready: () => !!window.AppleID },
};

export function loadSdk(provider: Provider): Promise<void> {
  const { src, ready } = SDK[provider];
  if (ready()) return Promise.resolve();
  return new Promise((resolve, reject) => {
    const s = document.createElement("script");
    s.src = src;
    s.async = true;
    s.onload = () => (ready() ? resolve() : reject(new Error(`${provider} sign-in did not load`)));
    s.onerror = () => reject(new Error(`${provider} sign-in could not be loaded`));
    document.head.appendChild(s);
  });
}

// A fresh random value per sign-in attempt. The provider echoes it inside the signed ID token and the
// API checks it, so a captured token cannot be replayed.
export function newNonce(): string {
  const b = new Uint8Array(16);
  crypto.getRandomValues(b);
  return Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
}

export function renderGoogleButton(el: HTMLElement, clientId: string, nonce: string, onToken: (idToken: string) => void) {
  window.google!.accounts.id.initialize({ client_id: clientId, nonce, callback: (r) => onToken(r.credential) });
  window.google!.accounts.id.renderButton(el, { theme: "outline", size: "large", text: "continue_with", shape: "pill", width: 280 });
}

export async function appleIdToken(clientId: string, nonce: string): Promise<string> {
  window.AppleID!.auth.init({ clientId, scope: "", redirectURI: window.location.origin, usePopup: true, nonce });
  const r = await window.AppleID!.auth.signIn();
  return r.authorization.id_token;
}
