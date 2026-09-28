import { useSyncExternalStore } from "react";
import { privacyPrefs, setPrivacyPref, type PrivacyOption, type PrivacyPrefs } from "@/lib/prefs";

// Privacy mode masks what a screen share shouldn't show. It is display
// only: copy buttons, open and reveal actions and API calls keep using the
// real values.

/** MASK has one fixed length, so a masked value says nothing about its own. */
export const MASK = "••••••";

// A *.test host anywhere in a sentence ("Deleting demo.test"); "*.test"
// itself has no name to hide.
const TEST_HOST = /[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?\.test\b/gi;

/** maskHost keeps the TLD, so "demo.test" still reads as a site. */
export function maskHost(host: string): string {
  const dot = host.lastIndexOf(".");
  return dot > 0 ? MASK + host.slice(dot) : MASK;
}

/** maskText masks every *.test host in a sentence (toasts, task titles). */
export function maskText(text: string): string {
  return text.replace(TEST_HOST, `${MASK}.test`);
}

// A path in a sentence that names the user: any Windows drive path, or a
// home-folder path on macOS and Linux. Shortcut: other absolute paths
// (/opt, /var) stay, they rarely hold a user or project name.
//
// Folders before a separator may hold spaces ("C:\Users\John Doe\"); the
// last part may too, but only right before a clear end (": ", a quote, a
// bracket, a newline), so "…\shop was moved" keeps "was moved". A trailing
// "." or "," is the sentence's. /home and /Users count only at the start of
// a word, never inside a URL ("https://x.test/home").
const LAST_CHAR = String.raw`[^\s\\/"'<>|:)\]]`;
const LAST = String.raw`(?:${LAST_CHAR}+(?: +${LAST_CHAR}+)+(?=:\s|:$|["'\n)\]])|(?:${LAST_CHAR}*[^\s\\/"'<>|:)\].,;])?)`;
const WINDOWS_DIRS = String.raw`(?<!\w)[A-Za-z]:\\(?:[^\\\n"'<>|:*?]*\\)*`;
const HOME_DIRS = String.raw`(?<![\w:/.-])(?:~|/home|/Users)/(?:[^/\n"'<>|:~]*/)*`;
const USER_PATH = new RegExp(`(?:${WINDOWS_DIRS}|${HOME_DIRS})${LAST}`, "g");

/** maskPaths masks every user path in a sentence (errors, check details). */
export function maskPaths(text: string): string {
  return text.replace(USER_PATH, MASK);
}

// .env keys whose names say they're credentials.
const SECRET_KEY = /PASSWORD|SECRET|KEY|TOKEN/i;

/** isSecretEnv: a credential .env line; empty and "null" values have
 *  nothing to hide. */
export function isSecretEnv(key: string, value: string): boolean {
  return SECRET_KEY.test(key) && value !== "" && value !== "null";
}

/** Privacy is what a component renders through: which options are in effect
 *  (the master switch and the option's own) and maskers that honor them. */
export interface Privacy {
  prefs: PrivacyPrefs;
  names: boolean;
  folders: boolean;
  secrets: boolean;
  mail: boolean;
  host: (host: string) => string;
  /** Masks the hosts and user paths inside a sentence. */
  text: (text: string) => string;
  path: (path: string) => string;
}

export function privacyView(prefs: PrivacyPrefs): Privacy {
  const hides = (option: PrivacyOption) => prefs.on && prefs[option];
  const names = hides("names");
  const folders = hides("folders");
  return {
    prefs,
    names,
    folders,
    secrets: hides("secrets"),
    mail: hides("mail"),
    host: (host) => (names ? maskHost(host) : host),
    text: (text) => {
      const shown = names ? maskText(text) : text;
      return folders ? maskPaths(shown) : shown;
    },
    path: (path) => (folders ? MASK : path),
  };
}

// One store for the window, so a toggle in Settings applies everywhere at
// once. Read lazily: localStorage only exists in the webview.
let current: Privacy | null = null;
const listeners = new Set<() => void>();

const snapshot = () => (current ??= privacyView(privacyPrefs()));

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export function setPrivacy(key: keyof PrivacyPrefs, on: boolean) {
  setPrivacyPref(key, on);
  current = privacyView(privacyPrefs());
  listeners.forEach((l) => l());
}

/** currentPrivacy is for code outside React, such as OS notifications. */
export function currentPrivacy(): Privacy {
  return snapshot();
}

export function usePrivacy(): Privacy {
  return useSyncExternalStore(subscribe, snapshot);
}
