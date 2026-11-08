import { routerBasename } from "./utils/publicUrl";

/** Only move this app's root API URLs. External and non-API URLs stay intact. */
export function portalUrl(raw: string): string {
  if (raw.startsWith("/api/")) return `${routerBasename() || ""}${raw}`;
  return raw;
}
