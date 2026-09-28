import { Badge } from "@/components/ui/badge";
import type { BinaryInfo, Site } from "@/lib/api";
import { sitePhp } from "@/lib/php";
import { cn } from "@/lib/utils";

/** KindBadge is how a site's kind shows next to its host everywhere
 *  (audit UI-29: the host names a site; the kind is secondary). Parked is
 *  the usual kind, so only the exceptions (linked, proxy) get a chip. */
export function KindBadge({ kind }: { kind: Site["kind"] }) {
  if (kind === "parked") return null;
  return (
    <Badge variant="secondary" className="capitalize">
      {kind}
    </Badge>
  );
}

/** SitePhpLabel names the PHP a site runs, and says so when it isn't
 *  installed (ui-standard F4). */
export function SitePhpLabel({
  site,
  defaultPhp,
  binaries,
  className,
}: {
  site: Site;
  defaultPhp: string | null;
  binaries: BinaryInfo[] | null;
  className?: string;
}) {
  const php = sitePhp(site, defaultPhp, binaries);
  const missing = php.installed === false;
  return (
    <span
      className={cn(
        "shrink-0 text-[13px] tabular-nums",
        missing ? "font-medium text-destructive" : "text-muted-foreground",
        className,
      )}
    >
      PHP {php.channel ?? "none"}
      {missing ? " · not installed" : site.php_pinned ? " · pinned" : ""}
    </span>
  );
}
