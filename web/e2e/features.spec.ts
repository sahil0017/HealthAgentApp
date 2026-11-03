import { expect, test, type Page } from "@playwright/test";
import { seedProfile, signInAs } from "./session";

const day = (n: number) => new Date(Date.now() - n * 86400000).toISOString();

// Minimal one-page PDF the API's text extractor can read.
function pdf(text: string): Buffer {
  const stream = `BT /F1 12 Tf 72 720 Td (${text}) Tj ET`;
  const objs = [
    "<< /Type /Catalog /Pages 2 0 R >>",
    "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
    "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
    `<< /Length ${stream.length} >>\nstream\n${stream}\nendstream`,
    "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
  ];
  let out = "%PDF-1.4\n";
  const offs: number[] = [];
  objs.forEach((o, i) => { offs.push(out.length); out += `${i + 1} 0 obj\n${o}\nendobj\n`; });
  const x = out.length;
  out += `xref\n0 ${objs.length + 1}\n0000000000 65535 f \n${offs.map((o) => `${String(o).padStart(10, "0")} 00000 n \n`).join("")}`;
  out += `trailer\n<< /Size ${objs.length + 1} /Root 1 0 R >>\nstartxref\n${x}\n%%EOF\n`;
  return Buffer.from(out, "latin1");
}

async function noHorizontalScroll(page: Page) {
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(overflow, "page scrolls sideways").toBeLessThanOrEqual(1);
}

test.describe("signed out", () => {
  test("shows sign-in with no token field", async ({ page }) => {
    await page.goto("/");
    await expect(page.getByRole("heading", { name: /health records/i })).toBeVisible();
    await expect(page.getByText(/token/i)).toHaveCount(0);
    await noHorizontalScroll(page);
  });
});

test.describe("new user", () => {
  test("creates a profile and reaches Today", async ({ page, context, baseURL }) => {
    await signInAs(context, baseURL!);
    await page.goto("/");
    await page.getByLabel(/Whose records/).fill("Ravi Kumar");
    await page.getByRole("button", { name: "Continue" }).click();
    await expect(page.getByRole("heading", { name: /Good (morning|afternoon|evening), Ravi/ })).toBeVisible();
    await expect(page.getByRole("link", { name: "Do it now" }).first()).toBeVisible();
  });
});

test.describe("signed in", () => {
  let id = "";
  test.beforeEach(async ({ page, context, baseURL }) => {
    page.on("pageerror", (e) => { throw e; });
    await signInAs(context, baseURL!);
    const seeded = await seedProfile(context, baseURL!, "Asha Rao");
    id = seeded.id;
    const cat = await seeded.api.call("GET", `/profiles/${id}/biomarkers`);
    const a1c = cat.find((c: any) => /a1c/i.test(c.display_name));
    for (const [n, v] of [[200, 5.1], [100, 5.6], [10, 5.9]] as const) {
      const o = await seeded.api.call("POST", `/profiles/${id}/observations`, { biomarker_code: a1c.code, value_numeric: v, unit: "%", observed_at: day(n) });
      await seeded.api.call("POST", `/observations/${o.id}/verify`);
    }
  });

  test("Today: stats, follow-up add and complete", async ({ page }) => {
    await page.goto("/");
    await expect(page.getByText("Recent changes")).toBeVisible();
    await page.getByLabel("Follow-up title").fill("Repeat HbA1c");
    await page.getByRole("button", { name: "Add", exact: true }).click();
    await expect(page.getByText("Repeat HbA1c")).toBeVisible();
    await page.getByRole("button", { name: "Mark done" }).click();
    await expect(page.getByText("Nothing pending")).toBeVisible();
    await noHorizontalScroll(page);
  });

  test("Biomarkers: list, plain-language labels, trend detail", async ({ page }) => {
    await page.goto("/biomarkers");
    await expect(page.getByText(/Going up/)).toBeVisible();
    await expect(page.getByText(/unknown|~0 days|reading\(s\)/)).toHaveCount(0);
    await page.getByRole("button", { name: /Hemoglobin/i }).click();
    await expect(page.getByRole("img", { name: /3 readings/ })).toBeVisible();
    await page.getByRole("button", { name: /All biomarkers/ }).click();
    await noHorizontalScroll(page);
  });

  test("Vault: upload a report and see it processed", async ({ page }) => {
    await page.goto("/documents");
    await expect(page.getByText("No documents yet")).toBeVisible();
    await page.getByLabel("File").setInputFiles({ name: "labs.pdf", mimeType: "application/pdf", buffer: pdf("Hemoglobin A1c 5.4 % Reference 4.0-5.6 %") });
    await expect(page.getByText("labs.pdf")).toBeVisible();
    await expect(page.getByRole("button", { name: "Review" })).toBeVisible({ timeout: 25_000 });
    await expect(page.getByRole("button", { name: "Search" })).toBeDisabled();
    await noHorizontalScroll(page);
  });

  test("Ask: starter question gets a cited answer and appears in history", async ({ page }) => {
    await page.goto("/ask");
    await page.getByRole("button", { name: "What was my latest HbA1c?" }).click();
    await expect(page.getByText(/You asked/).first()).toBeVisible({ timeout: 30_000 });
    await expect(page.getByText("Earlier questions")).toBeVisible();
    await noHorizontalScroll(page);
  });

  test("Timeline lists records", async ({ page }) => {
    await page.goto("/timeline");
    await expect(page.getByText(/Hemoglobin A1C 5\.9/i)).toBeVisible();
  });

  test("Privacy: consent toggle, devices, and sign out", async ({ page }) => {
    await page.goto("/privacy");
    const toggle = page.getByRole("switch", { name: /remind/i });
    await expect(toggle).toBeChecked();
    await toggle.setChecked(false, { force: true });
    await expect(toggle).not.toBeChecked();
    await expect(page.getByText(/this device/i)).toBeVisible();
    await noHorizontalScroll(page);
    await page.getByRole("button", { name: "Sign out" }).click();
    await expect(page.getByRole("heading", { name: /health records/i })).toBeVisible();
  });

  test("navigation reaches every section", async ({ page }) => {
    await page.goto("/");
    for (const name of ["Vault", "Biomarkers", "Ask AI", "Timeline", "Privacy", "Today"]) {
      await page.getByRole("navigation", { name: "Primary" }).getByRole("link", { name }).click();
      await expect(page.locator("main h1")).toBeVisible();
    }
  });
});
