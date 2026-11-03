import { createPrivateKey, sign, randomUUID } from "node:crypto";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import type { BrowserContext } from "@playwright/test";

const b64 = (b: Buffer | string) => Buffer.from(b).toString("base64url");

// Signs an ID token with the throwaway test IdP key (scripts/testidp.py), the same way the e2e suite does.
function idToken(sub: string, nonce: string): string {
  const key = createPrivateKey(readFileSync(resolve(process.cwd(), "../.idp/private.pem")));
  const now = Math.floor(Date.now() / 1000);
  const head = b64(JSON.stringify({ alg: "RS256", typ: "JWT", kid: "ci-key-1" }));
  const body = b64(JSON.stringify({ iss: "http://idp", aud: "cortex-ci", sub, iat: now, exp: now + 600, nonce }));
  return `${head}.${body}.${b64(sign("sha256", Buffer.from(`${head}.${body}`), key))}`;
}

// Gives the browser context a real API session cookie, as if the user had just signed in.
export async function signInAs(context: BrowserContext, baseURL: string, sub = `ui-${randomUUID().slice(0, 8)}`) {
  const nonce = randomUUID();
  const res = await context.request.post(`${baseURL}/api/v1/auth/session`, {
    headers: { "X-Cortex-Client": "web" },
    data: { provider: "oidc", id_token: idToken(sub, nonce), nonce },
  });
  if (res.status() !== 201) throw new Error(`sign-in failed: ${res.status()} ${await res.text()}`);
  return sub;
}

const PURPOSES = ["health_data", "medical_record_storage", "document_processing", "ai_processing", "research", "notifications"];

// Thin API client sharing the browser context's cookie, for seeding data the way a user would have created it.
export function apiOf(context: BrowserContext, baseURL: string) {
  const call = async (method: string, path: string, data?: unknown) => {
    const res = await context.request.fetch(`${baseURL}/api/v1${path}`, { method, data, headers: { "X-Cortex-Client": "web" } });
    const text = await res.text();
    const body = text ? JSON.parse(text) : {};
    if (!res.ok()) throw new Error(`${method} ${path}: ${res.status()} ${text}`);
    return body.data;
  };
  return { call };
}

export async function seedProfile(context: BrowserContext, baseURL: string, name: string) {
  const api = apiOf(context, baseURL);
  const profile = await api.call("POST", "/profiles", { display_name: name });
  for (const purpose of PURPOSES) await api.call("PUT", `/profiles/${profile.id}/consents/${purpose}`, { granted: true, policy_version: "1.0" });
  return { api, id: profile.id as string };
}
