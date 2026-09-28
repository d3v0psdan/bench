// A desktop app, not a web page (ui-standard D3): no browser context menu
// outside text fields, and no page reload from the keyboard. Development
// builds keep reload for working on the UI.

const RELOAD_KEYS = new Set(["F5", "r", "R"]);

/** isEditable is true where the native menu is useful (copy, paste). */
function isEditable(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  return target.isContentEditable || target.closest("input, textarea, [contenteditable=true]") !== null;
}

/** isReloadKey matches F5, Ctrl+R and Cmd+R, with or without Shift. */
export function isReloadKey(e: Pick<KeyboardEvent, "key" | "ctrlKey" | "metaKey">): boolean {
  if (!RELOAD_KEYS.has(e.key)) return false;
  return e.key === "F5" || e.ctrlKey || e.metaKey;
}

export function installDesktopShell() {
  document.addEventListener("contextmenu", (e) => {
    if (!isEditable(e.target)) e.preventDefault();
  });
  if (import.meta.env.DEV) return;
  document.addEventListener("keydown", (e) => {
    if (isReloadKey(e)) e.preventDefault();
  });
}
