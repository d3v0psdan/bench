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
