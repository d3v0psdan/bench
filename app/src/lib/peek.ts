/** What opened or is closing a peek. */
export type PeekSource = "pointer" | "focus";

export interface PeekTimers {
  set: (run: () => void, ms: number) => number;
  clear: (id: number | undefined) => void;
}

const windowTimers: PeekTimers = {
  set: (run, ms) => window.setTimeout(run, ms),
  clear: (id) => window.clearTimeout(id),
};

/** createPeek keeps which item a hover-or-focus peek shows, with the timing
 *  that keeps it calm: opening waits so a pointer sweeping past doesn't
 *  flicker it, closing waits so the pointer can reach the panel, a swap
 *  between items is instant, and a peek opened by keyboard focus stays until
 *  focus leaves (the pointer passing by can't close it). Timers are injected
 *  so tests can drive them. */
export function createPeek({
  openDelayMs,
  closeDelayMs,
  onChange,
  timers = windowTimers,
}: {
  openDelayMs: number;
  closeDelayMs: number;
  onChange: (name: string | null) => void;
  timers?: PeekTimers;
}) {
  let current: string | null = null;
  let focused: string | null = null;
  let hovered: string | null = null;
  let timer: number | undefined;

  const cancel = () => {
    timers.clear(timer);
    timer = undefined;
  };
  const commit = (next: string | null) => {
    if (next === current) return;
    current = next;
    onChange(next);
  };
  const schedule = (next: string | null, ms: number) => {
    cancel();
    if (ms <= 0) return commit(next);
    timer = timers.set(() => {
      timer = undefined;
      commit(next);
    }, ms);
  };

  return {
    show(name: string, source: PeekSource) {
      if (source === "focus") focused = name;
      else hovered = name;
      schedule(name, current === null ? openDelayMs : 0);
    },
    /** The pointer or focus left: close after the delay, or fall back to
     *  what the other one still holds open. */
    hide(source: PeekSource) {
      if (source === "focus") {
        focused = null;
        schedule(hovered, closeDelayMs);
      } else {
        hovered = null;
        schedule(focused, closeDelayMs);
      }
    },
    /** The pointer is over the panel: cancel a pending close or swap, and
     *  let the pointer hold what's showing. */
    keepOpen() {
      cancel();
      hovered = current;
    },
    /** Esc, or the item went away: close now. */
    close() {
      focused = null;
      hovered = null;
      schedule(null, 0);
    },
    dispose: cancel,
  };
}
