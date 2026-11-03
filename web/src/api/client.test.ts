import { ApiError, auth, request, signIn } from "./client";

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

describe("api client", () => {
  it("unwraps the data envelope and sends the bearer token", async () => {
    auth.set("tok");
    const f = mockFetch(200, { data: { a: 1 }, meta: {} });
    expect(await request("/x")).toEqual({ a: 1 });
    expect(f.mock.calls[0][0]).toBe("/api/v1/x");
    expect(f.mock.calls[0][1].headers.Authorization).toBe("Bearer tok");
  });

  it("sends JSON bodies with a content type", async () => {
    mockFetch(200, { data: 1 });
    const f = vi.mocked(fetch);
    await request("/x", { method: "POST", body: { k: "v" } });
    expect(f.mock.calls[0][1]?.body).toBe('{"k":"v"}');
    expect((f.mock.calls[0][1]?.headers as Record<string, string>)["Content-Type"]).toBe("application/json");
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

  it("clears the session on 401", async () => {
    auth.set("tok");
    mockFetch(401, { error: { code: "UNAUTHENTICATED", message: "x" } });
    await request("/x").catch(() => undefined);
    expect(auth.token).toBeNull();
  });

  it("signIn exchanges the dev token for an access token", async () => {
    const f = mockFetch(200, { data: { access_token: "acc" } });
    await signIn("dev");
    expect(f.mock.calls[0][1].headers.Authorization).toBe("Bearer dev");
    expect(auth.token).toBe("acc");
  });
});
