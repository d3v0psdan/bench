import type { BinaryInfo, Site } from "@/lib/api";

/** sitePhp is the PHP a site actually runs and whether it's installed
 *  (ui-standard F4: never show a runtime as healthy without checking).
 *  installed is null while the catalog is loading. */
export function sitePhp(
  site: Site,
  defaultPhp: string | null,
  binaries: BinaryInfo[] | null,
): { channel: string | null; build?: BinaryInfo; installed: boolean | null } {
  const channel = site.php || defaultPhp || null;
  if (!channel) return { channel, installed: false };
  if (binaries === null) return { channel, installed: null };
  const build = binaries.find((b) => b.name === "php" && b.channel === channel);
  return { channel, build, installed: !!build?.installed };
}

/** phpSetup says whether any PHP is installed, and which version to offer
 *  when none is: the newest in the catalog. The daemon makes the first
 *  version installed the default, so any installed version means sites
 *  have a default to run. installed is null while the catalog loads. */
export function phpSetup(binaries: BinaryInfo[] | null): { installed: boolean | null; latest?: BinaryInfo } {
  if (binaries === null) return { installed: null };
  const php = binaries.filter((b) => b.name === "php");
  const latest = php.reduce<BinaryInfo | undefined>(
    (newest, b) => (!newest || b.channel.localeCompare(newest.channel, undefined, { numeric: true }) > 0 ? b : newest),
    undefined,
  );
  return { installed: php.some((b) => b.installed), latest };
}

/** The one action that fixes a site's PHP problem: install a build, or
 *  drop the site's pin so it runs the default PHP. */
export type SitePhpFix = { kind: "install"; build: BinaryInfo } | { kind: "unpin" };

/** A site's PHP problem, and the action that fixes it when there is one. */
export interface SitePhpProblem {
  text: string;
  /** Absent when nothing in Bench can fix it (no PHP for this system). */
  fix?: SitePhpFix;
}

/** sitePhpProblem is why a site can't run its PHP, and the action that
 *  fixes exactly that (ui-standard F4), or null when there's nothing to
 *  fix: a proxy site, PHP that's installed, or a catalog still loading.
 *  It never offers an action that doesn't fix the stated problem; when
 *  none exists the problem comes without one. */
export function sitePhpProblem(
  site: Site,
  defaultPhp: string | null,
  binaries: BinaryInfo[] | null,
): SitePhpProblem | null {
  if (site.kind === "proxy" || binaries === null) return null;
  const php = sitePhp(site, defaultPhp, binaries);
  if (php.installed) return null;
  if (site.php_pinned && !php.build) {
    // Nothing installs a version the catalog lacks; the default is the way back.
    return {
      text: `This site is pinned to PHP ${php.channel}, which Bench doesn't offer, so it can't run.`,
      fix: { kind: "unpin" },
    };
  }
  if (php.build) {
    return { text: `PHP ${php.channel} isn't installed, so this site can't run.`, fix: { kind: "install", build: php.build } };
  }
  if (php.channel) {
    // A default the catalog lacks. The daemon only reports installed
    // defaults, so this is a stale view; there's nothing here to install.
    return { text: `This site runs the default, PHP ${php.channel}, which Bench doesn't offer, so it can't run.` };
  }
  // No pin and no default: no PHP is installed yet.
  const { latest } = phpSetup(binaries);
  if (!latest) {
    // The catalog only lists builds for this system, and may have no PHP.
    return { text: "Bench has no PHP for this system, so this site can't run." };
  }
  return {
    text: `PHP isn't installed, so this site can't run. The latest is PHP ${latest.channel}.`,
    fix: { kind: "install", build: latest },
  };
}
