import { useEffect, useLayoutEffect, useRef, useState, type ReactNode, type RefObject } from "react";
import { createPortal } from "react-dom";
import {
  CaretRightIcon,
  InfoIcon,
  FolderOpenIcon,
  GlobeIcon,
  LinkBreakIcon,
  MagnifyingGlassIcon,
  StarIcon,
} from "@phosphor-icons/react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { AddSiteButton, useAddSite } from "@/components/add-site/add-site";
import { ConfirmAction } from "@/components/confirm-action";
import { HealthAlert } from "@/components/health";
import { infoButton } from "@/components/info-tip";
import { LoadFailed } from "@/components/load-failed";
import { PageHeader, SectionCard, SectionRow } from "@/components/page";
import { SiteIcon } from "@/components/site-icon";
import { KindBadge, SitePhpLabel } from "@/components/sites/site-badges";
import { useDaemonContext } from "@/hooks/daemon-provider";
import { useRoute } from "@/hooks/route";
import { api, type BinaryInfo, type Site } from "@/lib/api";
import { errorMessage, withTimeout } from "@/lib/async";
import { sitePhp } from "@/lib/php";
import { revealPath } from "@/lib/open";
import { groupSites, type SiteKindFilter } from "@/lib/site-groups";
import { folderName } from "@/lib/site-name";
import { createPeek, type PeekSource } from "@/lib/peek";
import { MASK, usePrivacy } from "@/lib/privacy";
import { cn } from "@/lib/utils";

const PARKED_TIMEOUT_MS = 60_000;
/** How many hosts the unpark confirm names before "and N more". */
const UNPARK_LISTED = 5;

/** SitesPane: every served host, filterable and grouped, plus the parked
 *  directories behind them. Each row opens the site. */
export function SitesPane() {
  const { conn, sites, defaultPhp, binaries, loadError, reloadData } = useDaemonContext();
  const { route, navigate } = useRoute();
  const add = useAddSite();

  // A palette action ("Link a project…") lands here with an intent: run it
  // once, then drop it from the route so a re-render can't repeat it.
  const intent = route.kind === "home" ? route.intent : undefined;
  const handledIntent = useRef<string | undefined>(undefined);
  useEffect(() => {
    if ((intent !== "new" && intent !== "link" && intent !== "park" && intent !== "proxy") || !conn || handledIntent.current === intent)
      return;
    handledIntent.current = intent;
    navigate({ kind: "home", section: "sites" });
    add.start(intent);
    // add.start is left out on purpose: it changes every render, and the
    // intent must run once, not on each new closure.
  }, [intent, conn, navigate]);
  useEffect(() => {
    if (!intent) handledIntent.current = undefined;
  }, [intent]);

  const [query, setQuery] = useState("");
  const [kind, setKind] = useState<SiteKindFilter>("all");
  const groups = sites ? groupSites(sites, query, kind) : [];
  const openSite = (name: string) => navigate({ kind: "site", name, tab: "overview" });
  const peek = useSitePeek();
  const listRef = useRef<HTMLDivElement>(null);
  const privacy = usePrivacy();
  // Looked up on every render, so the panel shows the daemon's latest data.
  const peeked = sites?.find((s) => s.name === peek.name);
  // A peeked site that was removed closes the peek.
  const peekGone = peek.name !== null && !peeked;
  const closePeek = peek.close;
  useEffect(() => {
    if (peekGone) closePeek();
  }, [peekGone, closePeek]);

  const actions = <AddSiteButton onClick={() => add.start()} />;

  return (
    <>
      <PageHeader
        title="Sites"
        actions={sites && sites.length > 0 ? actions : undefined}
      />
      <HealthAlert className="mb-4" />

      {sites === null && loadError ? (
        <LoadFailed title="Couldn't load sites" message={loadError} onRetry={reloadData} />
      ) : sites === null ? (
        <div className="flex flex-col gap-2" aria-busy>
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-12 w-full rounded-lg" />
          ))}
        </div>
      ) : sites.length === 0 ? (
        <Empty className="border border-dashed bg-card">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <GlobeIcon />
            </EmptyMedia>
            <EmptyTitle>No sites yet</EmptyTitle>
            <EmptyDescription>
              Link a project, park a folder or proxy a local port.
            </EmptyDescription>
          </EmptyHeader>
          <EmptyContent className="flex-row justify-center">{actions}</EmptyContent>
        </Empty>
      ) : (
        <div ref={listRef} className="flex flex-col gap-5">
          <div className="flex flex-wrap items-center gap-3">
            <div className="relative min-w-48 flex-1">
              <MagnifyingGlassIcon className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder="Filter by host or folder, e.g. shop"
                aria-label="Filter sites"
                className="pl-8"
              />
            </div>
            <ToggleGroup
              type="single"
              variant="outline"
              size="sm"
              value={kind}
              // The items below are exactly the SiteKindFilter values.
              onValueChange={(v) => v && setKind(v as SiteKindFilter)}
              aria-label="Site kind"
            >
              <ToggleGroupItem value="all">All</ToggleGroupItem>
              <ToggleGroupItem value="linked">Linked</ToggleGroupItem>
              <ToggleGroupItem value="parked">Parked</ToggleGroupItem>
              <ToggleGroupItem value="proxy">Proxy</ToggleGroupItem>
            </ToggleGroup>
          </div>
          {groups.length === 0 ? (
            <Empty className="border border-dashed bg-card">
              <EmptyHeader>
                <EmptyTitle>No sites match</EmptyTitle>
                <EmptyDescription>
                  Clear the filter to see all {sites.length} sites.
                </EmptyDescription>
              </EmptyHeader>
              <EmptyContent>
                <Button
                  variant="outline"
                  onClick={() => {
                    setQuery("");
                    setKind("all");
                  }}
                >
                  Clear filter
                </Button>
              </EmptyContent>
            </Empty>
          ) : (
            groups.map((g) => (
              <section key={g.key} aria-label={g.dir ? `Parked in ${privacy.path(g.dir)}` : g.title}>
                <h2 className="mb-2 flex min-w-0 items-baseline gap-2 text-[13px] font-medium text-muted-foreground">
                  {g.title}
                  {g.dir ? <span className="truncate font-mono text-xs font-normal">{privacy.path(g.dir)}</span> : null}
                </h2>
                <ul className="flex flex-col gap-1.5">
                  {g.sites.map((s) => (
                    <li key={s.name}>
                      <SiteRow
                        site={s}
                        defaultPhp={defaultPhp}
                        binaries={binaries}
                        showKind={g.key === "favorites"}
                        onOpen={() => openSite(s.name)}
                        isPeeking={peeked?.name === s.name}
                        onPeek={(source) => peek.show(s.name, source)}
                        onUnpeek={peek.hide}
                      />
                    </li>
                  ))}
                </ul>
              </section>
            ))
          )}
        </div>
      )}
      <ParkedDirectoriesCard />
      {peeked ? (
        <SitePeek
          site={peeked}
          anchor={listRef}
          onPointerEnter={peek.keepOpen}
          onPointerLeave={() => peek.hide("pointer")}
        />
      ) : null}
      {add.dialogs}
    </>
  );
}

function SiteRow({
  site: s,
  defaultPhp,
  binaries,
  showKind,
  onOpen,
  isPeeking,
  onPeek,
  onUnpeek,
}: {
  site: Site;
  /** Only where the group heading doesn't already name the kind. */
  showKind: boolean;
  defaultPhp: string | null;
  binaries: BinaryInfo[] | null;
  onOpen: () => void;
  /** This row's details are showing in the side peek. */
  isPeeking: boolean;
  onPeek: (source: PeekSource) => void;
  onUnpeek: (source: PeekSource) => void;
}) {
  const isProxy = s.kind === "proxy";
  // A PHP that isn't installed is a problem, so it stays in the row (F4);
  // a healthy PHP lives behind the info icon.
  const phpMissing = !isProxy && sitePhp(s, defaultPhp, binaries).installed === false;
  const privacy = usePrivacy();
  const problem = s.path_missing ? "Folder moved or deleted" : s.error && privacy.text(s.error);
  // The open button stretches over the row with an ::after overlay, so the
  // info button can sit beside it (a button inside a button is invalid).
  // The peek opens from the icon but closes only when the pointer leaves the
  // row: SitePeek pins itself over the list's right edge, so the row always
  // runs under the panel and the pointer can slide onto it (and back).
  return (
    <div
      onPointerEnter={() => {
        if (isPeeking) onPeek("pointer");
      }}
      onPointerLeave={() => onUnpeek("pointer")}
      className="group relative flex min-h-12 items-center gap-3 rounded-lg bg-card px-4 py-2 shadow-card transition-[background-color] duration-150 ease-out hover:bg-well motion-reduce:transition-none"
    >
      <SiteIcon name={s.name} />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={onOpen}
            className="truncate text-left text-sm font-medium outline-none after:absolute after:inset-0 after:rounded-lg focus-visible:after:ring-3 focus-visible:after:ring-ring"
          >
            {privacy.host(s.host)}
          </button>
          {showKind ? <KindBadge kind={s.kind} /> : null}
          {s.favorite ? (
            <StarIcon weight="fill" role="img" aria-label="Favorite" className="size-3.5 shrink-0 text-warning-solid" />
          ) : null}
          {/* Beside the host, not at the row's end: the peek slides in over
              the right side, and must not cover the next row's icon. */}
          <button
            type="button"
            aria-label={`Details for ${privacy.host(s.host)}`}
            aria-describedby={isPeeking ? SITE_PEEK_ID : undefined}
            onPointerEnter={() => onPeek("pointer")}
            onFocus={() => onPeek("focus")}
            onBlur={() => onUnpeek("focus")}
            className={infoButton(
              cn(
                // size-6: a 24px target (WCAG 2.5.8), with the row's open overlay right beside it.
                "relative z-10 size-6 opacity-0 transition-opacity duration-150 group-focus-within:opacity-100 group-hover:opacity-100 focus-visible:opacity-100 motion-reduce:transition-none",
                isPeeking && "text-foreground opacity-100",
              ),
            )}
          >
            <InfoIcon className="size-3.5" />
          </button>
        </div>
        {problem ? (
          <p role="status" className="mt-0.5 text-xs break-words text-destructive">
            {problem}
          </p>
        ) : null}
      </div>
      {phpMissing ? <SitePhpLabel site={s} defaultPhp={defaultPhp} binaries={binaries} /> : null}
      <CaretRightIcon className="size-4 shrink-0 text-muted-foreground transition-transform duration-150 ease-out group-hover:translate-x-0.5 motion-reduce:transition-none" />
    </div>
  );
}

const SITE_PEEK_ID = "site-peek";
const PEEK_OPEN_DELAY_MS = 150;
const PEEK_CLOSE_DELAY_MS = 200;
/** The panel's smallest gap to the window edge, as the sheets' right-2. */
const PEEK_EDGE_PX = 8;

/** useSitePeek holds which site the side peek shows (timing in lib/peek),
 *  and closes it on Esc. */
function useSitePeek() {
  const [name, setName] = useState<string | null>(null);
  const [peek] = useState(() =>
    createPeek({ openDelayMs: PEEK_OPEN_DELAY_MS, closeDelayMs: PEEK_CLOSE_DELAY_MS, onChange: setName }),
  );
  useEffect(() => peek.dispose, [peek]);
  useEffect(() => {
    if (name === null) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") peek.close();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [name, peek]);
  return { name, ...peek };
}

/** SitePeek shows a site's details in a panel that slides in from the side
 *  like a sheet, but isn't modal: no backdrop, no focus trap, the list stays
 *  usable. It behaves like a tooltip (hover or focus), so it holds no
 *  controls; the site's page has those. */
function SitePeek({
  site: s,
  anchor,
  onPointerEnter,
  onPointerLeave,
}: {
  site: Site;
  /** The list the panel overlaps. */
  anchor: RefObject<HTMLElement | null>;
  onPointerEnter: () => void;
  onPointerLeave: () => void;
}) {
  const { defaultPhp, binaries, services } = useDaemonContext();
  // Pinned to the list's right edge, not the window's: on a wide window the
  // centered list ends well before the window does, and a gap there would be
  // too wide to cross before the peek closes.
  const [right, setRight] = useState(PEEK_EDGE_PX);
  useLayoutEffect(() => {
    const place = () => {
      const box = anchor.current?.getBoundingClientRect();
      setRight(box ? Math.max(PEEK_EDGE_PX, window.innerWidth - box.right) : PEEK_EDGE_PX);
    };
    place();
    window.addEventListener("resize", place);
    return () => window.removeEventListener("resize", place);
  }, [anchor]);
  const connected = (services ?? [])
    .filter((service) => service.port && s.env_ports?.includes(service.port))
    .map((service) => service.name);
  const privacy = usePrivacy();
  const problem = s.path_missing ? "Folder moved or deleted" : s.error && privacy.text(s.error);
  const row = (label: string, value: ReactNode) => (
    <div className="contents">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0 break-all">{value}</dd>
    </div>
  );

  return createPortal(
    <div
      id={SITE_PEEK_ID}
      role="tooltip"
      onPointerEnter={onPointerEnter}
      onPointerLeave={onPointerLeave}
      // top-12: below the 48px title bar, so the window controls stay usable.
      style={{ right }}
      className="fixed top-12 bottom-2 z-50 flex w-80 max-w-[calc(100%-1rem)] animate-in flex-col gap-5 overflow-y-auto rounded-xl bg-popover p-5 text-sm text-popover-foreground shadow-dialog duration-200 ease-out fade-in-0 slide-in-from-right-10 motion-reduce:animate-none"
    >
      <div className="flex items-center gap-3">
        <SiteIcon name={s.name} size="lg" />
        <div className="min-w-0">
          <p className="flex items-center gap-2 text-base font-medium">
            <span className="truncate">{privacy.host(s.host)}</span>
            <KindBadge kind={s.kind} />
          </p>
          {problem ? <p className="text-[13px] text-destructive">{problem}</p> : null}
        </div>
      </div>
      <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-2.5 text-[13px]">
        {row("Address", <span className="font-mono text-xs">{privacy.text(s.url)}</span>)}
        {s.kind === "proxy"
          ? row("Proxies to", <span className="font-mono text-xs">{s.proxy_to}</span>)
          : row("Runtime", <SitePhpLabel site={s} defaultPhp={defaultPhp} binaries={binaries} />)}
        {s.kind !== "proxy" && s.path ? row("Folder", <span className="font-mono text-xs">{privacy.path(s.path)}</span>) : null}
        {s.app_name ? row("Mail inbox", privacy.names ? MASK : s.app_name) : null}
        {connected.length > 0 ? row("Services", connected.join(", ")) : null}
      </dl>
    </div>,
    document.body,
  );
}

/** ParkedDirectoriesCard lists the folders parked with Bench, each with its
 *  site count, and unparks them (audit UI-16). */
function ParkedDirectoriesCard() {
  const { conn, sites, setSites } = useDaemonContext();
  const [dirs, setDirs] = useState<string[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);
  const [unparking, setUnparking] = useState<string | null>(null);
  const privacy = usePrivacy();
  const folderLabel = (dir: string) => (privacy.folders ? MASK : folderName(dir));

  // Re-read whenever the site list changes: a park or unpark anywhere
  // (GUI, CLI) publishes a new list.
  useEffect(() => {
    if (!conn) return;
    let cancelled = false;
    withTimeout(
      api<string[]>(conn, "GET", "/api/parked"),
      PARKED_TIMEOUT_MS,
      "Bench didn't list parked folders in time",
    ).then(
      (d) => {
        if (cancelled) return;
        setDirs(d);
        setError(null);
      },
      (e: unknown) => !cancelled && setError(errorMessage(e)),
    );
    return () => {
      cancelled = true;
    };
  }, [conn, sites, attempt]);

  if (error && dirs === null) {
    return (
      <div className="mt-8">
        <LoadFailed title="Couldn't load parked folders" message={error} onRetry={() => setAttempt((n) => n + 1)} />
      </div>
    );
  }
  if (dirs === null) return <Skeleton className="mt-8 h-24 w-full rounded-xl" aria-busy />;
  if (dirs.length === 0) return null;

  const servedFrom = (dir: string) => (sites ?? []).filter((s) => s.kind === "parked" && s.parked_in === dir);
  const target = unparking ? servedFrom(unparking) : [];

  const unpark = async () => {
    if (!conn || !unparking) throw new Error("Bench isn't connected");
    const next = await withTimeout(
      api<Site[]>(conn, "POST", "/api/sites/unpark", { path: unparking }),
      PARKED_TIMEOUT_MS,
      "Bench didn't answer in time",
    );
    setSites(next);
    toast.success(`${folderLabel(unparking)} unparked`, {
      description: `${target.length} site${target.length === 1 ? "" : "s"} no longer served`,
    });
  };

  return (
    <SectionCard
      className="mt-8"
      title="Parked folders"
      action={
        error ? (
          <div className="flex items-center gap-2">
            <p role="alert" className="text-xs text-destructive">
              Couldn't refresh: {error}
            </p>
            <Button variant="outline" size="sm" onClick={() => setAttempt((n) => n + 1)}>
              Try again
            </Button>
          </div>
        ) : undefined
      }
    >
      {dirs.map((dir) => {
        const count = servedFrom(dir).length;
        return (
          <SectionRow
            key={dir}
            label={<span className="font-mono text-xs break-all">{privacy.path(dir)}</span>}
            description={`${count} site${count === 1 ? "" : "s"}`}
          >
            <Button variant="outline" size="sm" onClick={() => revealPath(dir)}>
              <FolderOpenIcon data-icon="inline-start" />
              Show folder
            </Button>
            <Button variant="outline" size="sm" onClick={() => setUnparking(dir)}>
              <LinkBreakIcon data-icon="inline-start" />
              Unpark…
            </Button>
          </SectionRow>
        );
      })}
      <ConfirmAction
        open={unparking !== null}
        onOpenChange={(o) => !o && setUnparking(null)}
        title={`Unpark ${unparking ? folderLabel(unparking) : ""}?`}
        description="The folders stay where they are. Park it again to bring them back."
        details={[
          {
            label: "Folder",
            value: <span className="font-mono text-xs break-all">{unparking ? privacy.path(unparking) : null}</span>,
          },
          {
            label: "Stops serving",
            value:
              target.length === 0
                ? "No sites"
                : target
                    .slice(0, UNPARK_LISTED)
                    .map((s) => privacy.host(s.host))
                    .join(", ") + (target.length > UNPARK_LISTED ? ` and ${target.length - UNPARK_LISTED} more` : ""),
          },
        ]}
        confirmLabel={`Unpark ${unparking ? folderLabel(unparking) : "folder"}`}
        confirmIcon={<LinkBreakIcon data-icon="inline-start" />}
        tone="destructive"
        timeoutMs={PARKED_TIMEOUT_MS}
        onConfirm={unpark}
      />
    </SectionCard>
  );
}
