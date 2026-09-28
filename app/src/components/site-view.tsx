import { useState, type ReactNode } from "react";
import {
  ArrowSquareOutIcon,
  ArrowsLeftRightIcon,
  ClipboardTextIcon,
  DatabaseIcon,
  DotsThreeIcon,
  FolderOpenIcon,
  type Icon,
  LinkBreakIcon,
  StarIcon,
  TerminalWindowIcon,
  TrashIcon,
  WarningCircleIcon,
} from "@phosphor-icons/react";
import { open as pickDirectory } from "@tauri-apps/plugin-dialog";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { Alert, AlertAction, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { HealthAlert } from "@/components/health";
import { PageHeader } from "@/components/page";
import { SiteIcon } from "@/components/site-icon";
import { OpenInEditorButton, openWithToast } from "@/components/open-actions";
import { LoadFailed } from "@/components/load-failed";
import { KindBadge } from "@/components/sites/site-badges";
import { SiteCanvas } from "@/components/sites/site-canvas";
import { DeleteSiteDialog } from "@/components/sites/delete-site";
import { UnlinkSiteDialog } from "@/components/sites/unlink-site";
import { SiteSettings } from "@/components/sites/site-settings";
import { useDaemonContext } from "@/hooks/daemon-provider";
import { useHealth } from "@/hooks/use-doctor";
import { useRoute, type SiteTab } from "@/hooks/route";
import { api, type Site } from "@/lib/api";
import { errorMessage, withTimeout } from "@/lib/async";
import { openExternal, revealPath } from "@/lib/open";
import { usePrivacy } from "@/lib/privacy";

const SITE_TIMEOUT_MS = 60_000;
const FAVORITE_TIMEOUT_MS = 10_000;

/** SiteView: one site's page. */
export function SiteView({ name, tab }: { name: string; tab: SiteTab }) {
  const { sites, loadError, reloadData } = useDaemonContext();
  const { navigate } = useRoute();
  const site = sites?.find((s) => s.name === name);

  if (sites === null && loadError) {
    return <LoadFailed title="Couldn't load sites" message={loadError} onRetry={reloadData} />;
  }
  if (sites === null) {
    return (
      <div className="flex flex-col gap-6" aria-busy>
        <Skeleton className="h-12 w-72" />
        <Skeleton className="h-80 w-full rounded-xl" />
      </div>
    );
  }
  if (!site) {
    return (
      <Empty className="border border-dashed bg-card">
        <EmptyHeader>
          <EmptyTitle>{name}.test is gone</EmptyTitle>
          <EmptyDescription>It was removed, or its folder moved.</EmptyDescription>
        </EmptyHeader>
        <EmptyContent>
          <Button variant="outline" onClick={() => navigate({ kind: "home", section: "sites" })}>
            Back to sites
          </Button>
        </EmptyContent>
      </Empty>
    );
  }

  return (
    <>
      {tab === "overview" ? (
        <>
          <SiteHeader site={site} />
          <SiteCanvas site={site} />
        </>
      ) : (
        <SiteSettings site={site} />
      )}
    </>
  );
}

function SiteHeader({ site }: { site: Site }) {
  const [removeOpen, setRemoveOpen] = useState(false);
  const [unlinkOpen, setUnlinkOpen] = useState(false);
  const [relinking, setRelinking] = useState(false);
  const privacy = usePrivacy();
  // Relinking keeps the site's name and settings, at the folder's new place.
  const relink = async () => {
    if (!conn) return;
    const dir = await pickDirectory({ directory: true, title: `Where is ${site.host} now?` });
    if (typeof dir !== "string") return;
    setRelinking(true);
    try {
      setSites(
        await withTimeout(
          api<Site[]>(conn, "POST", "/api/sites/link", { path: dir, name: site.name }),
          SITE_TIMEOUT_MS,
          "Bench didn't answer in time",
        ),
      );
      toast.success(`${privacy.host(site.host)} now serves ${privacy.path(dir)}`);
    } catch (e) {
      toast.error(`Couldn't relink ${privacy.host(site.host)}`, { description: errorMessage(e) });
    } finally {
      setRelinking(false);
    }
  };
  const { canServe } = useHealth();
  const { conn, setSites } = useDaemonContext();
  const sitePath = site.path;
  // Instant and reversible (ui-standard C2): the same item undoes it.
  const toggleFavorite = async () => {
    if (!conn) return;
    const favorite = !site.favorite;
    try {
      setSites(
        await withTimeout(
          api<Site[]>(conn, "POST", "/api/sites/favorite", { name: site.name, favorite }),
          FAVORITE_TIMEOUT_MS,
          "Bench didn't answer in time",
        ),
      );
      const host = privacy.host(site.host);
      toast.success(favorite ? `${host} added to favorites` : `${host} removed from favorites`);
    } catch (e) {
      toast.error("Couldn't change favorites", { description: errorMessage(e) });
    }
  };
  const openPath = `/api/sites/${encodeURIComponent(site.name)}/open`;
  const copyUrl = () =>
    navigator.clipboard.writeText(site.url).then(
      () => toast.success("URL copied"),
      (e) => toast.error(`Couldn't copy: ${errorMessage(e)}`),
    );

  return (
    <>
      <PageHeader
        leading={<SiteIcon name={site.name} size="lg" />}
        title={
          <span className="flex items-center gap-2">
            <button
              type="button"
              onClick={() => openExternal(site.url)}
              title={`Open ${privacy.text(site.url)}`}
              className="rounded-sm outline-none decoration-muted-foreground/60 underline-offset-4 hover:underline focus-visible:ring-3 focus-visible:ring-ring"
            >
              {privacy.host(site.host)}
            </button>
            <KindBadge kind={site.kind} />
          </span>
        }
        description={
          <span className="flex flex-wrap items-center gap-x-5 gap-y-1">
            {site.kind === "proxy" ? (
              <MetaItem icon={ArrowsLeftRightIcon}>
                <span className="font-mono text-xs">{site.proxy_to}</span>
              </MetaItem>
            ) : (
              <button
                type="button"
                onClick={() => site.path && revealPath(site.path)}
                title="Show folder"
                className="inline-flex min-w-0 items-center gap-1.5 rounded-sm outline-none decoration-muted-foreground/60 underline-offset-4 hover:text-foreground hover:underline focus-visible:ring-3 focus-visible:ring-ring"
              >
                <FolderOpenIcon className="size-3.5 shrink-0" />
                <span className="truncate font-mono text-xs">{site.path ? privacy.path(site.path) : null}</span>
              </button>
            )}
          </span>
        }
        actions={
          <>
            {site.path ? <OpenInEditorButton site={site} /> : null}
            <Button variant={canServe === false ? "outline" : "brand"} onClick={() => openExternal(site.url)}>
              <ArrowSquareOutIcon data-icon="inline-start" />
              Open site
            </Button>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="icon" aria-label={`More actions for ${privacy.host(site.host)}`}>
                  <DotsThreeIcon weight="bold" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                {sitePath ? (
                  <>
                    <DropdownMenuGroup>
                      <DropdownMenuItem onSelect={() => revealPath(sitePath)}>
                        <FolderOpenIcon />
                        Show folder
                      </DropdownMenuItem>
                      <DropdownMenuItem onSelect={() => openWithToast(conn, openPath, { in: "terminal" }, "a terminal")}>
                        <TerminalWindowIcon />
                        Open terminal here
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        onSelect={() => openWithToast(conn, openPath, { in: "database" }, `${site.host}'s database`)}
                      >
                        <DatabaseIcon />
                        Open database
                      </DropdownMenuItem>
                    </DropdownMenuGroup>
                    <DropdownMenuSeparator />
                  </>
                ) : null}
                <DropdownMenuGroup>
                  <DropdownMenuItem onSelect={() => void toggleFavorite()}>
                    <StarIcon weight={site.favorite ? "fill" : "regular"} />
                    {site.favorite ? "Remove from favorites" : "Add to favorites"}
                  </DropdownMenuItem>
                  <DropdownMenuItem onSelect={() => void copyUrl()}>
                    <ClipboardTextIcon />
                    Copy URL
                  </DropdownMenuItem>
                </DropdownMenuGroup>
                <DropdownMenuSeparator />
                <DropdownMenuGroup>
                  {/* A proxy has nothing on disk to keep, so unlinking it is deleting it. */}
                  {site.kind !== "proxy" ? (
                    <DropdownMenuItem onSelect={() => setUnlinkOpen(true)}>
                      <LinkBreakIcon />
                      Unlink site…
                    </DropdownMenuItem>
                  ) : null}
                  <DropdownMenuItem variant="destructive" onSelect={() => setRemoveOpen(true)}>
                    <TrashIcon />
                    Delete site…
                  </DropdownMenuItem>
                </DropdownMenuGroup>
              </DropdownMenuContent>
            </DropdownMenu>
          </>
        }
      />
      <HealthAlert className="mb-4" />
      {site.path_missing ? (
        // The folder moved or went away (audit UI-49): point the site at
        // its new place, or remove it.
        <Alert variant="destructive" role="status" className="mb-4">
          <WarningCircleIcon />
          <AlertTitle>{privacy.host(site.host)}'s folder is gone</AlertTitle>
          <AlertDescription className="break-words">
            <span className="font-mono text-xs">{site.path ? privacy.path(site.path) : null}</span> was moved or deleted.
          </AlertDescription>
          <AlertAction className="flex gap-2">
            <Button size="sm" variant="outline" onClick={() => void relink()} disabled={relinking}>
              {relinking ? <Spinner data-icon="inline-start" className="motion-reduce:animate-none" /> : null}
              Relink…
            </Button>
            <Button size="sm" variant="outline" onClick={() => setRemoveOpen(true)}>
              <TrashIcon data-icon="inline-start" />
              Delete site…
            </Button>
          </AlertAction>
        </Alert>
      ) : site.error && canServe !== false ? (
        <Alert variant="destructive" role="status" className="mb-4">
          <WarningCircleIcon />
          <AlertTitle>{privacy.host(site.host)} has a problem</AlertTitle>
          <AlertDescription className="break-words">{privacy.text(site.error)}</AlertDescription>
        </Alert>
      ) : null}
      <UnlinkSiteDialog site={site} open={unlinkOpen} onOpenChange={setUnlinkOpen} />
      <DeleteSiteDialog site={site} open={removeOpen} onOpenChange={setRemoveOpen} />
    </>
  );
}

function MetaItem({ icon: Icon, children }: { icon: Icon; children: ReactNode }) {
  return (
    <span className="inline-flex min-w-0 items-center gap-1.5">
      <Icon className="size-3.5 shrink-0" />
      <span className="truncate">{children}</span>
    </span>
  );
}
