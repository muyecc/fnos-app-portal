import { routerBasename } from "./utils/publicUrl";

// The NAS gateway authenticates every renewal. No NAS token is read by Octop.
const base = routerBasename();
let pending = false;
async function renew() {
  if (pending) return;
  pending = true;
  try {
    const response = await fetch(`${base}/__portal/lease`, {
      method: "POST",
      credentials: "same-origin",
      cache: "no-store",
      headers: { "X-App-Portal": "lease" },
      signal: AbortSignal.timeout(4000),
    });
    if (!response.ok || !response.headers.get("content-type")?.includes("application/json")) throw new Error("NAS session expired");
    const result = await response.json();
    if (result.portal_session !== true) throw new Error("NAS session expired");
  } catch {
    // A reload must pass NAS authentication again; the server also expires all
    // active tunnels independently when renewal stops (including closed tabs).
    window.location.reload();
  } finally {
    pending = false;
  }
}
void renew();
window.setInterval(() => { if (document.visibilityState === "visible") void renew(); }, 5000);
document.addEventListener("visibilitychange", () => { if (document.visibilityState === "visible") void renew(); });
