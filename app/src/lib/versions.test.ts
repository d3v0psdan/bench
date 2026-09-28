import { expect, test } from "bun:test";
import { sameRelease } from "./versions";

test("sameRelease ignores prerelease tags but not version numbers", () => {
  expect(sameRelease("0.1.0-dev", "0.1.0")).toBe(true);
  expect(sameRelease("v0.1.0", "0.1.0")).toBe(true);
  expect(sameRelease("0.2.0", "0.1.0")).toBe(false);
  expect(sameRelease("0.1.1", "0.1.0")).toBe(false);
});
