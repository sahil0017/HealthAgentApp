import { ApiError, auth, listSessions, refreshSession, restoreSession, request, revokeSession, signIn, signOut, signOutEverywhere } from "./client";

function mockFetch(status: number, body: unknown) {
  const fn = vi.fn().mockResolvedValue({
    status,
    ok: status >= 200 && status < 300,
    statusText: "x",
    json: async () => body,
  });
  vi.stubGlobal("fetch", fn);
  return fn;
}

afterEach(() => vi.unstubAllGlobals());

afterEach(() => auth.set("unknown"));

describe("api client", () => {
  it("unwraps the data envelope and never sends a bearer token", async () => {
    const f = mockFetch(200, { data: { a: 1 }, meta: {} });
    expect(await request("/x")).toEqual({ a: 1 });
    expect(f.mock.calls[0][0]).toBe("/api/v1/x");
    const h = f.mock.calls[0][1].headers;
    expect(h.Authorization).toBeUndefined();
    expect(h["X-Cortex-Client"]).toBe("web");
  });

  it("sends JSON bodies with a content type", async () => {
    const f = mockFetch(200, { data: 1 });
    await request("/x", { method: "POST", body: { k: "v" } });
    expect(f.mock.calls[0][1]?.body).toBe('{"k":"v"}');
    expect(f.mock.calls[0][1]?.headers["Content-Type"]).toBe("application/json");
  });

  it("raises ApiError from the error envelope", async () => {
    mockFetch(400, { error: { code: "BAD", message: "nope", fields: { a: "b" } } });
    await expect(request("/x")).rejects.toMatchObject({ status: 400, code: "BAD", message: "nope", fields: { a: "b" } });
  });

  it("accepts the legacy string error shape", async () => {
    mockFetch(400, { error: "file_required" });
    const e = (await request("/x").catch((x: unknown) => x)) as ApiError;
    expect(e).toBeInstanceOf(ApiError);
    expect(e.message).toBe("file required");
  });

  it("marks the session signed out on 401", async () => {
    auth.set("in");
    mockFetch(401, { error: { code: "UNAUTHORIZED", message: "x" } });
    await request("/x").catch(() => undefined);
    expect(auth.state).toBe("out");
  });

  it("signIn posts the provider token and nonce, then marks the session active", async () => {
    const f = mockFetch(201, { data: { access_token: "ignored" } });
    await signIn("google", "id.jwt", "nonce1");
    expect(JSON.parse(f.mock.calls[0][1].body)).toEqual({ provider: "google", id_token: "id.jwt", nonce: "nonce1" });
    expect(auth.state).toBe("in");
    expect(JSON.stringify(sessionStorage) + JSON.stringify(localStorage)).not.toContain("ignored");
  });

  it("a failed sign-in does not mark the session active", async () => {
    mockFetch(401, { error: { code: "UNAUTHORIZED", message: "no" } });
    await expect(signIn("google", "bad", "n")).rejects.toBeInstanceOf(ApiError);
    expect(auth.state).not.toBe("in");
  });

  it("restoreSession reflects whether the cookie is valid", async () => {
    mockFetch(200, { data: { id: "u" } });
    await restoreSession();
    expect(auth.state).toBe("in");
    mockFetch(401, { error: { code: "UNAUTHORIZED", message: "x" } });
    await restoreSession();
    expect(auth.state).toBe("out");
  });

  it("signOut ends the session even if the request fails", async () => {
    auth.set("in");
    mockFetch(500, { error: { code: "X", message: "x" } });
    await signOut().catch(() => undefined);
    expect(auth.state).toBe("out");
  });

  it("refreshSession posts to the rotate endpoint with the client header", async () => {
    const f = mockFetch(200, { data: { access_token: "ignored" } });
    await refreshSession();
    expect(f.mock.calls[0][0]).toBe("/api/v1/auth/refresh");
    expect(f.mock.calls[0][1].method).toBe("POST");
    expect(f.mock.calls[0][1].headers["X-Cortex-Client"]).toBe("web");
  });

  it("lists and revokes device sessions", async () => {
    const f = mockFetch(200, { data: [{ id: "s1", device: "ios", current: false }] });
    expect(await listSessions()).toHaveLength(1);
    expect(f.mock.calls[0][0]).toBe("/api/v1/auth/sessions");
    await revokeSession("s1");
    expect(f.mock.calls[1][0]).toBe("/api/v1/auth/sessions/s1");
    expect(f.mock.calls[1][1].method).toBe("DELETE");
  });

  it("signOutEverywhere ends the session locally too", async () => {
    auth.set("in");
    const f = mockFetch(200, { data: { revoked: 2 } });
    await signOutEverywhere();
    expect(f.mock.calls[0][0]).toBe("/api/v1/auth/logout-all");
    expect(auth.state).toBe("out");
  });
});
