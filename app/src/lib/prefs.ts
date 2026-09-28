import { HOME, type Route } from "@/hooks/route";

// UI preferences kept on this machine (localStorage). These are choices
// about the app window, never app state; that lives in the daemon.

const START_DAEMON_KEY = "bench.startDaemonOnOpen";

/** startDaemonOnOpen: start Bench's daemon when the app opens (default on),
 *  so sites come back after a reboot without a click. */
export function startDaemonOnOpen(): boolean {
  return localStorage.getItem(START_DAEMON_KEY) !== "0";
}

export function setStartDaemonOnOpen(on: boolean) {
  localStorage.setItem(START_DAEMON_KEY, on ? "1" : "0");
}

const SIDEBAR_KEY = "bench.sidebarExpanded";

/** sidebarExpanded: the sidebar shows labels (default), or icons only. */
export function sidebarExpanded(): boolean {
  return localStorage.getItem(SIDEBAR_KEY) !== "0";
}

export function setSidebarExpanded(on: boolean) {
  localStorage.setItem(SIDEBAR_KEY, on ? "1" : "0");
}

const CONNECTED_KEY = "bench.hasConnected";

/** hasConnectedBefore: Bench has run on this machine before, so the start
 *  screen can skip the first-run welcome. */
export function hasConnectedBefore(): boolean {
  return localStorage.getItem(CONNECTED_KEY) === "1";
}

export function markConnected() {
  localStorage.setItem(CONNECTED_KEY, "1");
}

const ROUTE_KEY = "bench.route";

/** savedRoute is the page this window showed before a reload (session
 *  only: a fresh launch starts at Overview). Intents are one-shot and
 *  never restored. */
export function savedRoute(): Route {
  try {
    const raw = sessionStorage.getItem(ROUTE_KEY);
    // Written by saveRoute below, so the shape is ours.
    const route = raw ? (JSON.parse(raw) as Route) : null;
    if (route && (route.kind === "home" || route.kind === "site")) return route;
  } catch {
    // an unreadable entry just means starting at Overview
  }
  return HOME;
}

export function saveRoute(route: Route) {
  const kept = route.kind === "home" ? { ...route, intent: undefined } : route;
  sessionStorage.setItem(ROUTE_KEY, JSON.stringify(kept));
}

export type PrivacyOption = "names" | "folders" | "secrets" | "mail";

export type PrivacyPrefs = { on: boolean } & Record<PrivacyOption, boolean>;

// Off until asked for; once on, every option hides by default, so the one
// switch is enough before a screen share.
const PRIVACY_DEFAULTS: PrivacyPrefs = { on: false, names: true, folders: true, secrets: true, mail: true };

/** privacyPrefs: privacy mode (masking for screen sharing) and what it hides. */
export function privacyPrefs(): PrivacyPrefs {
  const read = (key: keyof PrivacyPrefs) => {
    const v = localStorage.getItem(`bench.privacy.${key}`);
    return v === null ? PRIVACY_DEFAULTS[key] : v === "1";
  };
  return { on: read("on"), names: read("names"), folders: read("folders"), secrets: read("secrets"), mail: read("mail") };
}

export function setPrivacyPref(key: keyof PrivacyPrefs, on: boolean) {
  localStorage.setItem(`bench.privacy.${key}`, on ? "1" : "0");
}

export type NotifyKind = "crash" | "tasks" | "mail";

// PLAN.md §4 (Phase 4 scope): crashes and finished installs on, new mail off.
const NOTIFY_DEFAULTS: Record<NotifyKind, boolean> = { crash: true, tasks: true, mail: false };

/** notifyEnabled: whether this kind of event raises an OS notification. */
export function notifyEnabled(kind: NotifyKind): boolean {
  const v = localStorage.getItem(`bench.notify.${kind}`);
  return v === null ? NOTIFY_DEFAULTS[kind] : v === "1";
}

export function setNotifyEnabled(kind: NotifyKind, on: boolean) {
  localStorage.setItem(`bench.notify.${kind}`, on ? "1" : "0");
}
