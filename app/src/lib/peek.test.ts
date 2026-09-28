import { describe, expect, test } from "bun:test";
import { createPeek, type PeekTimers } from "./peek";

/** fakeTimers runs scheduled callbacks only when the test advances time. */
function fakeTimers() {
  let now = 0;
  let nextId = 1;
  const pending = new Map<number, { at: number; run: () => void }>();
  const timers: PeekTimers = {
    set: (run, ms) => {
      pending.set(nextId, { at: now + ms, run });
      return nextId++;
    },
    clear: (id) => {
      if (id !== undefined) pending.delete(id);
    },
  };
  const advance = (ms: number) => {
    now += ms;
    for (const [id, t] of [...pending]) {
      if (t.at > now) continue;
      pending.delete(id);
      t.run();
    }
  };
  return { timers, advance };
}

function setup() {
  const { timers, advance } = fakeTimers();
  const shown: Array<string | null> = [];
  const peek = createPeek({ openDelayMs: 150, closeDelayMs: 200, onChange: (n) => shown.push(n), timers });
  return { peek, advance, shown };
}

describe("createPeek", () => {
  test("opens after the open delay, not before", () => {
    const { peek, advance, shown } = setup();
    peek.show("a", "pointer");
    advance(149);
    expect(shown).toEqual([]);
    advance(1);
    expect(shown).toEqual(["a"]);
  });

  test("a pointer sweeping past never opens it", () => {
    const { peek, advance, shown } = setup();
    peek.show("a", "pointer");
    advance(50);
    peek.hide("pointer");
    advance(500);
    expect(shown).toEqual([]);
  });

  test("swaps to another item at once while open", () => {
    const { peek, advance, shown } = setup();
    peek.show("a", "pointer");
    advance(150);
    peek.show("b", "pointer");
    expect(shown).toEqual(["a", "b"]);
  });

  test("closes after the close delay, unless the pointer reaches the panel", () => {
    const { peek, advance, shown } = setup();
    peek.show("a", "pointer");
    advance(150);
    peek.hide("pointer");
    advance(100);
    peek.keepOpen();
    advance(500);
    expect(shown).toEqual(["a"]);
    peek.hide("pointer");
    advance(200);
    expect(shown).toEqual(["a", null]);
  });

  test("coming back to the open item cancels its pending close", () => {
    const { peek, advance, shown } = setup();
    peek.show("a", "pointer");
    advance(150);
    peek.hide("pointer");
    advance(100);
    peek.show("a", "pointer");
    advance(500);
    expect(shown).toEqual(["a"]);
  });

  test("a focus-opened peek survives the pointer passing another item", () => {
    const { peek, advance, shown } = setup();
    peek.show("a", "focus");
    advance(150);
    peek.show("b", "pointer");
    peek.hide("pointer");
    advance(200);
    expect(shown).toEqual(["a", "b", "a"]);
    peek.hide("focus");
    advance(200);
    expect(shown).toEqual(["a", "b", "a", null]);
  });

  test("focus leaving keeps the peek the pointer still holds", () => {
    const { peek, advance, shown } = setup();
    peek.show("a", "focus");
    advance(150);
    peek.show("b", "pointer");
    peek.hide("focus");
    advance(500);
    expect(shown).toEqual(["a", "b"]);
  });

  test("the pointer resting on the panel holds it when focus leaves", () => {
    const { peek, advance, shown } = setup();
    peek.show("a", "focus");
    advance(150);
    peek.keepOpen();
    peek.hide("focus");
    advance(500);
    expect(shown).toEqual(["a"]);
  });

  test("close is immediate and resets the open delay", () => {
    const { peek, advance, shown } = setup();
    peek.show("a", "focus");
    advance(150);
    peek.close();
    expect(shown).toEqual(["a", null]);
    peek.show("b", "pointer");
    advance(100);
    expect(shown).toEqual(["a", null]);
  });
});
