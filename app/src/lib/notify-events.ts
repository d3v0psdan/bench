import type { Route } from "@/hooks/route";
import type { Service, Task } from "@/lib/api";

// Which daemon changes deserve an OS notification (audit UI-40). Pure, so
// the rules are testable apart from the notification plugin.

export interface Note {
  kind: "crash" | "tasks";
  title: string;
  body: string;
  /** Where a crashed service's logs are: its sheet, or the Mail page. */
  logs?: Route;
}

/** Tasks whose ending is worth a notification: the long ones. */
const NOTIFIED_TASKS = new Set(["binary.install", "service.create", "service.clone", "site.new", "setup"]);

/** serviceNotes: services that have just failed. */
export function serviceNotes(before: Service[] | null, after: Service[]): Note[] {
  if (!before) return []; // the first list isn't news
  const was = new Map(before.map((s) => [s.name, s.state]));
  return after
    .filter((s) => s.state === "failed" && was.has(s.name) && was.get(s.name) !== "failed")
    .map((s) => ({
      kind: "crash",
      title: `${s.name} stopped working`,
      body: s.error ?? "Its logs say why.",
      logs:
        s.service === "mailpit"
          ? { kind: "home", section: "mail", serviceLogs: true }
          : { kind: "home", section: "services", service: s.name, serviceLogs: true },
    }));
}

/** taskNotes: long tasks that have just finished, failed or not. */
export function taskNotes(before: Task[] | null, after: Task[]): Note[] {
  if (!before) return [];
  const running = new Set(before.filter((t) => t.state === "running").map((t) => t.id));
  return after
    .filter((t) => running.has(t.id) && t.state !== "running" && t.state !== "cancelled" && NOTIFIED_TASKS.has(t.kind))
    .map((t) => ({
      kind: "tasks",
      title: t.state === "done" ? `${t.title}: done` : `${t.title}: failed`,
      body: t.state === "done" ? "Finished in the background." : (t.error ?? "Open Bench to see what happened."),
    }));
}
