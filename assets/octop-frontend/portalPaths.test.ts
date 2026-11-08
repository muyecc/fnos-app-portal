import { beforeEach, expect, test, vi } from "vitest";
import { portalUrl } from "./portalPaths";
import { getApiUrl, getWsUrl } from "./api/config";
import { workspaceLinkTarget } from "./portalWorkspaceLinks";

beforeEach(() => {
  vi.stubEnv("BASE_URL", "/app/octop-portal/");
  vi.stubGlobal("BASE_URL", "");
});

test("API and websocket paths retain one gateway prefix", () => {
  expect(getApiUrl("/agents")).toBe("/app/octop-portal/api/agents");
  expect(getApiUrl("/api/workspace/media?path=x")).toBe("/app/octop-portal/api/workspace/media?path=x");
  expect(getApiUrl("/app/octop-portal/api/workspace/media?path=x")).toBe("/app/octop-portal/api/workspace/media?path=x");
  expect(getWsUrl("/browser-stream/ws")).toMatch(/\/app\/octop-portal\/api\/browser-stream\/ws$/);
});

test("workspace links and media preserve portal and external boundaries", () => {
  expect(portalUrl("/api/workspace/media?path=x")).toBe("/app/octop-portal/api/workspace/media?path=x");
  expect(portalUrl("/app/octop-portal/api/workspace/media?path=x")).toBe("/app/octop-portal/api/workspace/media?path=x");
  expect(portalUrl("https://example.org/image.png")).toBe("https://example.org/image.png");
  expect(workspaceLinkTarget("/app/octop-portal/api/agents/demo/workspace/file?path=plans%2Ftest.md")).toEqual({ path: "plans/test.md", agentId: "demo" });
});
