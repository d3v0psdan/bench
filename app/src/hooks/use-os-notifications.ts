import { useEffect, useRef } from "react";
import { isPermissionGranted, requestPermission, sendNotification } from "@tauri-apps/plugin-notification";
import { toast } from "sonner";
import { useDaemonContext } from "@/hooks/daemon-provider";
import type { Route } from "@/hooks/route";
import type { Service, Task } from "@/lib/api";
import { DEFAULT_TIMEOUT_MS, withTimeout } from "@/lib/async";
import { serviceNotes, taskNotes, type Note } from "@/lib/notify-events";
import { notifyEnabled } from "@/lib/prefs";
import { currentPrivacy } from "@/lib/privacy";

/** notify shows an OS notification when Bench isn't the focused window
 *  (in front of you, the app already shows the change) and its kind is on
 *  in Settings. Permission is asked the first time one is due. */
async function notify(note: Pick<Note, "title" | "body"> & { kind: Note["kind"] | "mail" }) {
  if (document.hasFocus() || !notifyEnabled(note.kind)) return;
  let granted = await withTimeout(
    isPermissionGranted(),
    DEFAULT_TIMEOUT_MS,
    "The notification permission didn't load in time",
  );
  // No timeout here: this waits on the user answering the OS prompt.
  if (!granted) granted = (await requestPermission()) === "granted";
  const privacy = currentPrivacy();
  if (granted) sendNotification({ title: privacy.text(note.title), body: privacy.text(note.body) });
}

function report(e: unknown) {
  console.error("sending a notification failed", e);
}

/** useOsNotifications turns daemon events into OS notifications (audit
 *  UI-40): a service that fails, a long task that ends, and new mail. A
 *  failed service also gets a toast with "View logs", at once when Bench is
 *  in front, or when the window next gets focus (the OS notification can't
 *  carry a link on desktop). */
export function useOsNotifications(navigate: (route: Route) => void) {
  const { services, tasks, newMail } = useDaemonContext();
  const lastServices = useRef<Service[] | null>(null);
  const lastTasks = useRef<Task[] | null>(null);
  const lastMail = useRef(newMail);
  const crashedAway = useRef<Note[]>([]);

  useEffect(() => {
    const crashToast = ({ title, body, logs }: Note) =>
      toast.error(title, {
        description: body,
        action: logs ? { label: "View logs", onClick: () => navigate(logs) } : undefined,
      });
    const onFocus = () => {
      crashedAway.current.forEach(crashToast);
      crashedAway.current = [];
    };
    if (services) {
      for (const n of serviceNotes(lastServices.current, services)) {
        if (document.hasFocus()) crashToast(n);
        else crashedAway.current.push(n);
        void notify(n).catch(report);
      }
      lastServices.current = services;
    }
    window.addEventListener("focus", onFocus);
    return () => window.removeEventListener("focus", onFocus);
  }, [services, navigate]);

  useEffect(() => {
    if (!tasks) return;
    taskNotes(lastTasks.current, tasks).forEach((n) => void notify(n).catch(report));
    lastTasks.current = tasks;
  }, [tasks]);

  useEffect(() => {
    if (newMail > lastMail.current) {
      const count = newMail - lastMail.current;
      void notify({
        kind: "mail",
        title: "New mail",
        body: `${count} new message${count === 1 ? "" : "s"} in Bench`,
      }).catch(report);
    }
    lastMail.current = newMail;
  }, [newMail]);
}
