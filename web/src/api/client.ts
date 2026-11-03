const BASE = "/api/v1";

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    public fields?: Record<string, string>,
  ) {
    super(message);
  }
}

type Listener = () => void;
const listeners = new Set<Listener>();

export type SessionState = "unknown" | "in" | "out";
let state: SessionState = "unknown";

// The session lives in an HttpOnly cookie set by the API, so the app never holds or shows a token.
// This only tracks whether the browser currently has a valid session.
export const auth = {
  get state(): SessionState {
    return state;
  },
  set(next: SessionState) {
    state = next;
    listeners.forEach((l) => l());
  },
  subscribe(l: Listener) {
    listeners.add(l);
    return () => listeners.delete(l);
  },
};

export interface RequestOptions {
  method?: string;
  body?: unknown;
  raw?: boolean;
  expectSession?: boolean;
}

// Every successful response is wrapped as {data, meta}; errors as {error:{code,message,fields}}.
export async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  // The client header is what lets the API accept cookie-authenticated writes (CSRF guard).
  const headers: Record<string, string> = { "X-Cortex-Client": "web" };
  let body: BodyInit | undefined;
  if (opts.body instanceof FormData) body = opts.body;
  else if (opts.body !== undefined) {
    headers["Content-Type"] = "application/json";
    body = JSON.stringify(opts.body);
  }
  const res = await fetch(BASE + path, { method: opts.method ?? "GET", headers, body });
  if (res.status === 204) return undefined as T;
  let payload: any = null;
  try {
    payload = await res.json();
  } catch {
    /* non-JSON body */
  }
  if (!res.ok) {
    if (res.status === 401 && opts.expectSession !== false) auth.set("out");
    const e = payload?.error;
    // A few handlers still return {error: "snake_case_code"} instead of the full envelope.
    if (typeof e === "string") throw new ApiError(res.status, e, e.replace(/_/g, " "));
    throw new ApiError(res.status, e?.code ?? "error", e?.message ?? res.statusText, e?.fields);
  }
  return (opts.raw ? payload : payload?.data) as T;
}

export const get = <T>(path: string) => request<T>(path);
export const post = <T>(path: string, body?: unknown) => request<T>(path, { method: "POST", body: body ?? {} });
export const put = <T>(path: string, body: unknown) => request<T>(path, { method: "PUT", body });
export const patch = <T>(path: string, body: unknown) => request<T>(path, { method: "PATCH", body });
export const del = <T>(path: string, body?: unknown) => request<T>(path, { method: "DELETE", body });

export type Provider = "google" | "apple";

export async function signIn(provider: Provider, idToken: string, nonce: string): Promise<void> {
  await request("/auth/session", { method: "POST", body: { provider, id_token: idToken, nonce }, expectSession: false });
  auth.set("in");
}

// Finds out whether the browser already has a session (the cookie is invisible to scripts).
export async function restoreSession(): Promise<void> {
  try {
    await request("/users/me", { expectSession: false });
    auth.set("in");
  } catch {
    auth.set("out");
  }
}

export async function signOut(): Promise<void> {
  try {
    await post("/auth/logout");
  } finally {
    auth.set("out");
  }
}

export async function signOutEverywhere(): Promise<void> {
  try {
    await post("/auth/logout-all");
  } finally {
    auth.set("out");
  }
}

export interface DeviceSession {
  id: string;
  device: string;
  user_agent: string;
  started_at: string;
  last_seen_at: string;
  expires_at: string;
  current: boolean;
}
export const listSessions = () => get<DeviceSession[]>("/auth/sessions");
export const revokeSession = (id: string) => del(`/auth/sessions/${id}`);

// The API rotates the cookie on refresh, so the session stays alive while the app is in use and ends after
// a long idle period or the 30-day sign-in cap.
const REFRESH_EVERY_MS = 3 * 60 * 60 * 1000;
let lastRefresh = Date.now();

export async function refreshSession(): Promise<void> {
  await post("/auth/refresh");
  lastRefresh = Date.now();
}

export function keepSessionFresh(): () => void {
  lastRefresh = Date.now();
  const tick = () => {
    if (auth.state === "in" && Date.now() - lastRefresh >= REFRESH_EVERY_MS) refreshSession().catch(() => undefined);
  };
  const timer = setInterval(tick, 10 * 60 * 1000);
  document.addEventListener("visibilitychange", tick);
  return () => {
    clearInterval(timer);
    document.removeEventListener("visibilitychange", tick);
  };
}

export interface AuthConfig { providers: { name: Provider; client_id: string }[] }
export const authConfig = () => request<AuthConfig>("/auth/config", { expectSession: false });
