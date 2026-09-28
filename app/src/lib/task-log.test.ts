import { describe, expect, test } from "bun:test";
import { cleanLog, splitSteps } from "./task-log";

const ESC = String.fromCharCode(27);

describe("task logs", () => {
  test("cleanLog drops colours and keeps a spinner's last frame", () => {
    expect(cleanLog(`${ESC}[41;1m Pest ${ESC}[49;22m`)).toBe(" Pest");
    expect(cleanLog("⠶ Generating\r⠦ Generating\r✓ Generated\n")).toBe("✓ Generated\n");
  });

  test("splitSteps groups lines under their step", () => {
    const log =
      "\n== Creating the project\n$ composer create-project laravel/laravel shop\nDone.\n\n== Serving shop.test\n";
    expect(splitSteps(log)).toEqual([
      { title: "Creating the project", lines: ["$ composer create-project laravel/laravel shop", "Done."] },
      { title: "Serving shop.test", lines: [] },
    ]);
  });
});
