import { describe, expect, test } from "bun:test";
import { isReloadKey } from "./desktop-shell";

describe("isReloadKey", () => {
  test("matches the reload shortcuts", () => {
    expect(isReloadKey({ key: "F5", ctrlKey: false, metaKey: false })).toBe(true);
    expect(isReloadKey({ key: "r", ctrlKey: true, metaKey: false })).toBe(true);
    expect(isReloadKey({ key: "R", ctrlKey: false, metaKey: true })).toBe(true);
  });

  test("leaves typing alone", () => {
    expect(isReloadKey({ key: "r", ctrlKey: false, metaKey: false })).toBe(false);
    expect(isReloadKey({ key: "k", ctrlKey: true, metaKey: false })).toBe(false);
  });
});
