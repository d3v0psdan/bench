import { describe, expect, test } from "bun:test";
import type { BinaryInfo, Site } from "@/lib/api";
import { phpSetup, sitePhp, sitePhpProblem, type SitePhpFix } from "@/lib/php";

const site = (over: Partial<Site> = {}): Site => ({ name: "shop", host: "shop.test", url: "https://shop.test", kind: "linked", ...over });
const php = (channel: string, installed: boolean): BinaryInfo => ({
  name: "php",
  channel,
  version: `${channel}.0`,
  installed,
  dir: "",
  sources: [],
});

describe("sitePhp", () => {
  test("follows the default when the site has no version", () => {
    expect(sitePhp(site(), "8.4", [php("8.4", true)])).toMatchObject({ channel: "8.4", installed: true });
  });

  test("says when the effective version isn't installed", () => {
    expect(sitePhp(site({ php: "8.3" }), "8.4", [php("8.3", false), php("8.4", true)])).toMatchObject({
      channel: "8.3",
      installed: false,
    });
  });

  test("is unknown while the catalog loads, never healthy", () => {
    expect(sitePhp(site(), "8.4", null).installed).toBeNull();
  });

  test("no version at all is not installed", () => {
    expect(sitePhp(site(), null, [])).toEqual({ channel: null, installed: false });
  });
});

describe("phpSetup", () => {
  test("nothing installed offers the newest catalog version", () => {
    const setup = phpSetup([php("8.4", false), php("8.10", false), php("8.9", false)]);
    expect(setup.installed).toBe(false);
    expect(setup.latest?.channel).toBe("8.10");
  });

  test("any installed version counts, not only the newest", () => {
    expect(phpSetup([php("8.4", true), php("8.5", false)]).installed).toBe(true);
  });

  test("is unknown while the catalog loads", () => {
    expect(phpSetup(null)).toEqual({ installed: null });
  });
});

/** A machine as the daemon reports it: the catalog has PHP 8.4 and 8.5
 *  unless noCatalogPhp (a system Bench has no PHP build for); the first
 *  version installed becomes the default. */
interface Machine {
  installed: string[];
  pin?: string;
  proxy?: boolean;
  noCatalogPhp?: boolean;
}

function report(m: Machine) {
  const binaries = m.noCatalogPhp
    ? []
    : [php("8.4", m.installed.includes("8.4")), php("8.5", m.installed.includes("8.5"))];
  const defaultPhp = m.installed[0] ?? "";
  const s = m.proxy
    ? site({ kind: "proxy", proxy_to: "127.0.0.1:3000" })
    : site({ php_pinned: m.pin, php: m.pin ?? (defaultPhp || undefined) });
  return sitePhpProblem(s, defaultPhp, binaries);
}

/** What the daemon does when the user takes the action. */
function apply(m: Machine, fix: SitePhpFix): Machine {
  if (fix.kind === "unpin") return { ...m, pin: undefined };
  return { ...m, installed: m.installed.includes(fix.build.channel) ? m.installed : [...m.installed, fix.build.channel] };
}

describe("sitePhpProblem", () => {
  const notInstalled = "PHP 8.5 isn't installed, so this site can't run.";
  const notOffered = "This site is pinned to PHP 7.4, which Bench doesn't offer, so it can't run.";
  const noPhp = "PHP isn't installed, so this site can't run. The latest is PHP 8.5.";
  // "Pinned to 8.4, installed" needs something installed, so it has no
  // nothing-installed row.
  const cases: Array<{ name: string; machine: Machine; text: string | null; fix: string | null }> = [
    { name: "no pin, nothing installed", machine: { installed: [] }, text: noPhp, fix: "install 8.5" },
    { name: "no pin, something installed", machine: { installed: ["8.4"] }, text: null, fix: null },
    { name: "pin installed", machine: { installed: ["8.4"], pin: "8.4" }, text: null, fix: null },
    { name: "pin in catalog, nothing installed", machine: { installed: [], pin: "8.5" }, text: notInstalled, fix: "install 8.5" },
    { name: "pin in catalog, another installed", machine: { installed: ["8.4"], pin: "8.5" }, text: notInstalled, fix: "install 8.5" },
    { name: "pin not in catalog, nothing installed", machine: { installed: [], pin: "7.4" }, text: notOffered, fix: "unpin" },
    { name: "pin not in catalog, something installed", machine: { installed: ["8.4"], pin: "7.4" }, text: notOffered, fix: "unpin" },
    { name: "proxy, nothing installed", machine: { installed: [], proxy: true }, text: null, fix: null },
    { name: "proxy, something installed", machine: { installed: ["8.4"], proxy: true }, text: null, fix: null },
    // The one problem with no action: nothing in Bench can install PHP here.
    {
      name: "no pin, nothing installed, no PHP in the catalog for this system",
      machine: { installed: [], noCatalogPhp: true },
      text: "Bench has no PHP for this system, so this site can't run.",
      fix: null,
    },
  ];

  for (const c of cases) {
    test(`${c.name}: states the problem and the action that fixes it`, () => {
      const problem = report(c.machine);
      expect(problem?.text ?? null).toBe(c.text);
      const fix = problem?.fix
        ? problem.fix.kind === "unpin"
          ? "unpin"
          : `install ${problem.fix.build.channel}`
        : null;
      expect(fix).toBe(c.fix);

      // Taking the actions shown leads to a site that runs. Unpinning lands
      // in a no-pin case, which may ask for one install of its own.
      let machine = c.machine;
      let next = problem;
      for (let step = 0; next?.fix && step < 2; step++) {
        machine = apply(machine, next.fix);
        next = report(machine);
        if (next) expect(next.text).not.toBe(problem?.text);
      }
      // A problem without an action stays as stated; every other one is fixed.
      expect(next?.text ?? null).toBe(c.fix === null ? c.text : null);
    });
  }

  test("a default the catalog lacks is stated with no action, not thrown", () => {
    const problem = sitePhpProblem(site({ php: "7.4" }), "7.4", [php("8.4", true)]);
    expect(problem).toEqual({
      text: "This site runs the default, PHP 7.4, which Bench doesn't offer, so it can't run.",
    });
  });
});
