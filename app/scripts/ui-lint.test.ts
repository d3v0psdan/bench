import { afterAll, describe, expect, test } from "bun:test";
import { mkdirSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { lintFile } from "./ui-lint";

// Fixtures live under src/ so path-based rules apply as they do for real files.
const dir = join(import.meta.dir, "..", "src", "__lint_fixtures__");
mkdirSync(dir, { recursive: true });
afterAll(() => rmSync(dir, { recursive: true, force: true }));

function rules(name: string, source: string): string[] {
  const path = join(dir, name);
  writeFileSync(path, source);
  return lintFile(path).map((v) => v.rule);
}

describe("ui-lint", () => {
  test("clean code passes", () => {
    expect(rules("clean.tsx", `export const A = () => <div className="flex gap-2 bg-card" />;\n`)).toEqual([]);
  });

  test("C1: a consequential endpoint needs a ConfirmAction", () => {
    const call = `const f = () => withTimeout(api(conn, "POST", "/api/binaries/install"), 1, "");\n`;
    expect(rules("c1.tsx", call)).toContain("C1");
    expect(rules("c1-ok.tsx", call + `const B = () => <ConfirmAction />;\n`)).not.toContain("C1");
  });

  test("C1: stop and runSetup count as consequential", () => {
    expect(rules("c1-stop.tsx", `const { stop } = useDaemonContext();\n`)).toContain("C1");
  });

  test("F2: api() without a timeout, in .tsx and .ts alike", () => {
    expect(rules("f2.tsx", `api(conn, "GET", "/api/sites");\n`)).toContain("F2");
    expect(rules("f2.ts", `api(conn, "GET", "/api/sites");\n`)).toContain("F2");
    expect(rules("f2-ok.ts", `withTimeout(api(conn, "GET", "/api/sites"), 1, "");\n`)).not.toContain("F2");
  });

  test("F6, V1, V4, V5, W3, W4", () => {
    expect(rules("f6.tsx", `void openUrl(url);\n`)).toContain("F6");
    expect(rules("v1.tsx", `<div className="bg-red-500" />;\n`)).toContain("V1");
    expect(rules("v1-hex.tsx", `<div style={{ color: "#ff0000" }} />;\n`)).toContain("V1");
    expect(rules("v4.tsx", `<div className="space-y-2" />;\n`)).toContain("V4");
    expect(rules("v5.tsx", `import { X } from "lucide-react";\n`)).toContain("V5");
    expect(rules("v5-shield.tsx", `<ShieldCheckIcon />;\n`)).toContain("V5");
    expect(rules("w3.tsx", `const s = "a \u2014 b";\n`)).toContain("W3");
    expect(rules("w4.tsx", `alert("hi");\n`)).toContain("W4");
  });

  test("a waiver needs a reason", () => {
    expect(rules("waived.tsx", `// ui-standard-allow V4: third-party markup we can't restyle\n<div className="space-y-2" />;\n`)).toEqual([]);
    expect(rules("unwaived.tsx", `// ui-standard-allow V4: no\n<div className="space-y-2" />;\n`)).toContain("V4");
  });
});
