import { describe, expect, test } from "bun:test";
import { appendLog } from "./log-text";

describe("appendLog", () => {
  test("appends a chunk to what is shown", () => {
    expect(appendLog("a\n", { text: "b\n" })).toBe("a\nb\n");
  });

  test("a reset replaces what is shown", () => {
    expect(appendLog("old\n", { text: "new\n", reset: true })).toBe("new\n");
  });

  test("keeps only the last maxLines lines", () => {
    expect(appendLog("1\n2\n", { text: "3\n4\n" }, 3)).toBe("2\n3\n4\n");
    expect(appendLog("1\n2\n", { text: "3" }, 2)).toBe("2\n3");
    expect(appendLog("1\n", { text: "2\n" }, 5)).toBe("1\n2\n");
  });
});
