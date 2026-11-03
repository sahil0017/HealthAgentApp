import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { App } from "./App";
import { auth } from "./api/client";
import { TrendChart } from "./components/TrendChart";
import { TABS } from "./components/Shell";

type Routes = Record<string, unknown | ((init?: RequestInit) => unknown)>;

function api(routes: Routes) {
  const fn = vi.fn(async (url: string, init?: RequestInit) => {
    const key = `${init?.method ?? "GET"} ${url.replace("/api/v1", "").split("?")[0]}`;
    if (!(key in routes)) return { status: 404, ok: false, statusText: "nf", json: async () => ({ error: { code: "NF", message: `no mock ${key}` } }) };
    const r = routes[key];
    const data = typeof r === "function" ? (r as (i?: RequestInit) => unknown)(init) : r;
    return { status: 200, ok: true, statusText: "ok", json: async () => ({ data }) };
  });
  vi.stubGlobal("fetch", fn);
  return fn;
}

const overview = {
  profile: { id: "p1", display_name: "Asha", age: null, sex: null, blood_group: null, height_cm: null },
  summary: { documents: 2, observations: 5, biomarkers_tracked: 3, active_goals: 1, open_follow_ups: 1, history_entries: 0 },
  attention: [{ kind: "review", severity: "action", title: "Review a report", detail: "lab.pdf is ready" }],
  recent_changes: [], goals: [], next_follow_up: null, suggested_follow_ups: [], reminders: [],
  completeness: { score: 60, items: [{ key: "consent", label: "Grant consent", done: false, detail: "Needed for AI" }] },
};

afterEach(() => vi.unstubAllGlobals());

function renderApp(path = "/") {
  return render(<MemoryRouter initialEntries={[path]}><App /></MemoryRouter>);
}

describe("App", () => {
  it("shows sign-in and stores the session token", async () => {
    api({ "POST /auth/session": { access_token: "acc" }, "GET /profiles": [{ id: "p1", display_name: "Asha" }], "GET /profiles/p1/overview": overview });
    renderApp();
    await userEvent.type(screen.getByLabelText("Access token"), "dev");
    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByText("Review a report")).toBeInTheDocument();
    expect(auth.token).toBe("acc");
  });

  it("surfaces sign-in errors", async () => {
    api({});
    renderApp();
    await userEvent.type(screen.getByLabelText("Access token"), "bad");
    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("no mock");
  });

  it("asks a new user to create a profile", async () => {
    auth.set("t");
    const f = api({ "GET /profiles": [], "POST /profiles": { id: "p2", display_name: "Ravi" } });
    renderApp();
    await userEvent.type(await screen.findByLabelText("Name"), "Ravi");
    await userEvent.click(screen.getByRole("button", { name: "Create profile" }));
    await waitFor(() => expect(f.mock.calls.some(([u, i]) => u.endsWith("/profiles") && i?.method === "POST")).toBe(true));
  });

  it("renders Today with attention, completeness and stats", async () => {
    auth.set("t");
    api({ "GET /profiles": [{ id: "p1", display_name: "Asha" }], "GET /profiles/p1/overview": overview });
    renderApp();
    expect(await screen.findByText("Needs your attention")).toBeInTheDocument();
    expect(screen.getByRole("progressbar")).toHaveAttribute("aria-valuenow", "60");
    expect(screen.getByText(/Grant consent/)).toBeInTheDocument();
  });

  it("offers every destination in the single navigation used on desktop and mobile", async () => {
    auth.set("t");
    api({ "GET /profiles": [{ id: "p1", display_name: "Asha" }], "GET /profiles/p1/overview": overview });
    renderApp();
    const nav = await screen.findByRole("navigation", { name: "Primary" });
    for (const t of TABS) expect(nav).toHaveTextContent(t.label);
  });

  it("shows an empty vault and the biomarker empty state", async () => {
    auth.set("t");
    api({
      "GET /profiles": [{ id: "p1", display_name: "Asha" }],
      "GET /profiles/p1/documents": [],
      "GET /profiles/p1/biomarkers/summary": [],
      "GET /profiles/p1/biomarkers/coverage": [],
    });
    const { unmount } = renderApp("/documents");
    expect(await screen.findByText("No documents yet.")).toBeInTheDocument();
    unmount();
    renderApp("/biomarkers");
    expect(await screen.findByText(/Upload and confirm a report/)).toBeInTheDocument();
  });

  it("sends the confirmed values when approving a document", async () => {
    auth.set("t");
    const doc = {
      id: "d1", file_name: "lab.pdf", status: "review_required", document_type: "LAB_REPORT", document_version: 1,
      extraction: { biomarkers: [{ name: "HbA1c", value: 5.4, unit: "%", observed_at: "2026-01-02", reference_range: "" }], text_preview: "", warnings: [] },
    };
    const f = api({ "GET /profiles": [{ id: "p1", display_name: "Asha" }], "GET /profiles/p1/documents": [doc], "POST /profiles/p1/documents/d1/approve": {} });
    renderApp("/documents");
    await userEvent.click(await screen.findByRole("button", { name: "Review" }));
    await userEvent.click(screen.getByRole("button", { name: "Confirm and save values" }));
    const call = f.mock.calls.find(([u]) => u.endsWith("/approve"))!;
    expect(JSON.parse(call[1]!.body as string).biomarkers[0]).toMatchObject({ name: "HbA1c", value: 5.4 });
  });

  it("shows an urgent banner and sources for an answer", async () => {
    auth.set("t");
    api({
      "GET /profiles": [{ id: "p1", display_name: "Asha" }],
      "GET /profiles/p1/questions": [],
      "POST /profiles/p1/questions": {
        id: "q1", answer: "Seek care now.", urgent: true, confidence: "low", uncertainty: "", limitations: [],
        sources: [{ type: "record", description: "HbA1c 5.4" }],
      },
    });
    renderApp("/ask");
    await userEvent.type(await screen.findByLabelText("Your question"), "chest pain");
    await userEvent.click(screen.getByRole("button", { name: "Ask" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(/urgent/i);
    expect(screen.getByText("HbA1c 5.4")).toBeInTheDocument();
  });

  it("requires typing DELETE before erasing a profile", async () => {
    auth.set("t");
    api({ "GET /profiles": [{ id: "p1", display_name: "Asha" }], "GET /profiles/p1/consents": [], "GET /profiles/p1/access-history": [] });
    renderApp("/privacy");
    const btn = await screen.findByRole("button", { name: "Delete profile" });
    expect(btn).toBeDisabled();
    await userEvent.type(screen.getByLabelText(/Type DELETE/), "DELETE");
    expect(btn).toBeEnabled();
  });

  it("toggles a consent", async () => {
    auth.set("t");
    const f = api({
      "GET /profiles": [{ id: "p1", display_name: "Asha" }],
      "GET /profiles/p1/consents": [{ purpose: "ai_processing", granted: false }],
      "GET /profiles/p1/access-history": [],
      "PUT /profiles/p1/consents/ai_processing": {},
    });
    renderApp("/privacy");
    await userEvent.click(await screen.findByRole("switch", { name: /Use AI/ }));
    const put = f.mock.calls.find(([, i]) => i?.method === "PUT")!;
    expect(JSON.parse(put[1]!.body as string)).toEqual({ granted: true, policy_version: "1.0" });
  });
});

describe("TrendChart", () => {
  const pt = (id: string, d: string, v: number) => ({ observation_id: id, observed_at: d, value: v, unit: "%", flag: "normal", range: null, verification_status: "VERIFIED", source_confidence: "high" });
  it("draws one marker per reading", () => {
    const { container } = render(<TrendChart points={[pt("a", "2026-01-01", 5), pt("b", "2026-03-01", 6)]} unit="%" low={4} high={5.6} />);
    expect(container.querySelectorAll("circle")).toHaveLength(2);
    expect(screen.getByRole("img")).toHaveAccessibleName(/2 readings/);
  });
  it("handles a single reading and no readings", () => {
    const { container, rerender } = render(<TrendChart points={[pt("a", "2026-01-01", 5)]} unit="%" />);
    expect(container.querySelectorAll("circle")).toHaveLength(1);
    rerender(<TrendChart points={[]} unit="%" />);
    expect(screen.getByText(/No verified readings/)).toBeInTheDocument();
  });
});
