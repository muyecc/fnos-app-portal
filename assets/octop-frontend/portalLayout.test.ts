import { expect, test } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

// Vitest intentionally disables CSS processing in the upstream config.
const source = (name: string) => readFileSync(resolve("src", name), "utf8");
const layout = source("styles/layout.css");
const header = source("layouts/Header.tsx");
const sidebar = source("layouts/Sidebar.tsx");

test("fnOS owns the top inset for both mobile navigation surfaces", () => {
  expect(layout).toContain("--safe-area-top: 0px;");
  for (const source of [header, sidebar]) {
    expect(source).toContain("var(--safe-area-top, 0px)");
    expect(source).not.toContain("env(safe-area-inset-top");
  }
});

test("bottom and horizontal safe areas remain device controlled", () => {
  for (const edge of ["bottom", "left", "right"]) {
    expect(layout).toContain(`--safe-area-${edge}: env(safe-area-inset-${edge}, 0px);`);
  }
});
