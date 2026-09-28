import { LinkBreakIcon } from "@phosphor-icons/react";
import { toast } from "sonner";
import { ConfirmAction } from "@/components/confirm-action";
import { useDaemonContext } from "@/hooks/daemon-provider";
import { useRoute } from "@/hooks/route";
import { api, type Site } from "@/lib/api";
import { withTimeout } from "@/lib/async";
import { usePrivacy } from "@/lib/privacy";

// Unlinking re-applies every route, which can wait on Caddy starting.
const UNLINK_TIMEOUT_MS = 60_000;

const KEPT = "Files, database and mail stay.";

const DESCRIPTION: Record<Site["kind"], string> = {
  linked: `Bench stops serving it and removes it from Sites. ${KEPT}`,
  // The parked folder still holds it, so say how it comes back.
  parked: `Bench stops serving it and removes it from Sites. ${KEPT} It stays hidden until you link its folder again.`,
  proxy: "Bench stops proxying this host and removes it from Sites.",
};

/** UnlinkSiteDialog removes a site from Bench without deleting anything
 *  (ui-standard C1): Bench stops serving it and forgets it, and a parked
 *  folder stays hidden until it is linked again. */
export function UnlinkSiteDialog({
  site,
  open,
  onOpenChange,
}: {
  site: Site;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { conn, setSites } = useDaemonContext();
  const { navigate } = useRoute();
  const privacy = usePrivacy();

  const unlink = async () => {
    if (!conn) throw new Error("Bench isn't connected");
    const sites = await withTimeout(
      api<Site[]>(conn, "POST", "/api/sites/unlink", { name: site.name }),
      UNLINK_TIMEOUT_MS,
      "Bench didn't answer in time",
    );
    setSites(sites);
    navigate({ kind: "home", section: "sites" });
    toast.success(`${privacy.host(site.host)} unlinked`);
  };

  return (
    <ConfirmAction
      open={open}
      onOpenChange={onOpenChange}
      title={`Unlink ${privacy.host(site.host)}?`}
      description={DESCRIPTION[site.kind]}
      details={[
        { label: "Site", value: privacy.text(site.url) },
        ...(site.path
          ? [{ label: "Folder", value: <span className="font-mono text-xs">{privacy.path(site.path)}</span> }]
          : []),
        ...(site.proxy_to ? [{ label: "Proxies to", value: <span className="font-mono text-xs">{site.proxy_to}</span> }] : []),
        ...(site.php_pinned ? [{ label: "Lost", value: `Its PHP ${site.php_pinned} pin` }] : []),
      ]}
      confirmLabel="Unlink site"
      confirmIcon={<LinkBreakIcon data-icon="inline-start" />}
      onConfirm={unlink}
    />
  );
}
