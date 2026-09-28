import { describe, expect, test } from "bun:test";
import { DEFAULT_ANSWERS, stepsFor, toRequest, type NewAppAnswers } from "./new-app-steps";

const with_ = (a: Partial<NewAppAnswers>): NewAppAnswers => ({ ...DEFAULT_ANSWERS, name: "shop", ...a });

describe("stepsFor", () => {
  test("a React kit with Laravel auth asks about teams", () => {
    expect(stepsFor(with_({}))).toEqual([
      "name",
      "kit",
      "stack",
      "auth",
      "teams",
      "testing",
      "boost",
      "database",
      "javascript",
      "review",
    ]);
  });

  test("no starter kit skips auth and teams", () => {
    expect(stepsFor(with_({ starterKit: false, stack: "blade" }))).toEqual([
      "name",
      "kit",
      "stack",
      "testing",
      "boost",
      "database",
      "javascript",
      "review",
    ]);
  });

  test("Livewire asks about single-file components, and teams only with them", () => {
    expect(stepsFor(with_({ stack: "livewire" }))).toContain("livewire");
    expect(stepsFor(with_({ stack: "livewire" }))).toContain("teams");
    expect(stepsFor(with_({ stack: "livewire", singleFile: false }))).not.toContain("teams");
  });

  test("WorkOS skips the Laravel-auth questions", () => {
    const steps = stepsFor(with_({ stack: "livewire", auth: "workos" }));
    expect(steps).not.toContain("livewire");
    expect(steps).not.toContain("teams");
  });
});

describe("toRequest", () => {
  test("drops answers to questions that weren't asked", () => {
    const r = toRequest(with_({ starterKit: false, stack: "vue", teams: true, auth: "workos" }), "");
    expect(r.auth).toBeUndefined();
    expect(r.teams).toBe(false);
  });

  test("class components when single-file is declined", () => {
    expect(toRequest(with_({ stack: "livewire", singleFile: false }), "").class_components).toBe(true);
  });

  test("SQLite always migrates and never names a service", () => {
    const r = toRequest(with_({ database: "sqlite", migrate: false, service: "mysql" }), "");
    expect(r.migrate).toBe(true);
    expect(r.service).toBeUndefined();
  });
});
