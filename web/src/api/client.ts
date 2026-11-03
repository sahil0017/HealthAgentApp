const BASE = "/api/v1";
const TOKEN_KEY = "cortex.token";

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

export const auth = {
  get token(): string | null {
    return sessionStorage.getItem(TOKEN_KEY);
  },
  set(token: string | null) {
    if (token) sessionStorage.setItem(TOKEN_KEY, token);
    else sessionStorage.removeItem(TOKEN_KEY);
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
  token?: string;
  raw?: boolean;
}

// Every successful response is wrapped as {data, meta}; errors as {error:{code,message,fields}}.
export async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = {};
  const token = opts.token ?? auth.token;
  if (token) headers.Authorization = `Bearer ${token}`;
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
    if (res.status === 401 && !opts.token) auth.set(null);
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

export async function signIn(devToken: string): Promise<void> {
  const s = await request<{ access_token: string }>("/auth/session", { method: "POST", token: devToken });
  auth.set(s.access_token);
}

export async function signOut(): Promise<void> {
  try {
    await post("/auth/logout");
  } finally {
    auth.set(null);
  }
}
