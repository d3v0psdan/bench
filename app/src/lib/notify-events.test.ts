import { describe, expect, test } from "bun:test";
import type { Service, Task } from "./api";
import { serviceNotes, taskNotes } from "./notify-events";

const svc = (name: string, state: string, error?: string) => ({ name, state, error }) as Service;
const task = (id: string, kind: string, state: Task["state"]) => ({ id, kind, state, title: `Task ${id}` }) as Task;

describe("serviceNotes", () => {
  test("a service turning failed is news, once", () => {
    expect(serviceNotes([svc("mysql", "running")], [svc("mysql", "failed", "port in use")])).toEqual([
      {
        kind: "crash",
        title: "mysql stopped working",
        body: "port in use",
        logs: { kind: "home", section: "services", service: "mysql", serviceLogs: true },
      },
    ]);
    expect(serviceNotes([svc("mysql", "failed")], [svc("mysql", "failed")])).toEqual([]);
  });

  test("the first list and new instances aren't news", () => {
    expect(serviceNotes(null, [svc("mysql", "failed")])).toEqual([]);
    expect(serviceNotes([], [svc("mysql", "failed")])).toEqual([]);
  });
});

describe("taskNotes", () => {
  test("long tasks that end are news; cancels and short ones aren't", () => {
    const before = [task("1", "site.new", "running"), task("2", "site.new", "running"), task("3", "other", "running")];
    const after = [task("1", "site.new", "done"), task("2", "site.new", "cancelled"), task("3", "other", "done")];
    expect(taskNotes(before, after).map((n) => n.title)).toEqual(["Task 1: done"]);
  });
});
