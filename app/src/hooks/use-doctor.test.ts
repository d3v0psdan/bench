import { describe, expect, test } from "bun:test";
import type { Check } from "@/lib/api";
import { deriveHealth } from "@/hooks/use-doctor";

const check = (id: string, status: Check["status"]): Check => ({ id, title: id, status, detail: "" });

describe("deriveHealth", () => {
  test("nothing checked yet is loading, not healthy", () => {
    expect(deriveHealth(null, null)).toMatchObject({ loading: true, issues: [], canServe: null, needsSetup: null, error: null });
  });

  test("checks that couldn't run are an error, not a pass", () => {
    expect(deriveHealth(null, "timed out")).toMatchObject({ loading: false, error: "timed out", canServe: null });
  });

  test("a failing port blocks serving and counts as an issue", () => {
    const h = deriveHealth([check("https", "fail"), check("dns", "ok"), check("trust", "ok")], null);
    expect(h.canServe).toBe(false);
    expect(h.issues.map((c) => c.id)).toEqual(["https"]);
    expect(h.needsSetup).toBe(false);
  });

  test("setup warnings are issues; informational warnings are not", () => {
    const h = deriveHealth([check("https", "ok"), check("dns", "warn"), check("trust", "ok"), check("mail", "warn")], null);
    expect(h.issues.map((c) => c.id)).toEqual(["dns"]);
    expect(h.needsSetup).toBe(true);
  });

  test("Herd is reported only while it's in the way", () => {
    expect(deriveHealth([check("herd", "warn")], null).herd?.id).toBe("herd");
    expect(deriveHealth([check("herd", "ok")], null).herd).toBeUndefined();
  });
});
