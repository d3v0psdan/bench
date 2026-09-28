import { useEffect, useState } from "react";
import { getCurrentWindow } from "@tauri-apps/api/window";
import { useTheme } from "next-themes";
import {
  CaretUpDownIcon,
  ClockCounterClockwiseIcon,
  CopyIcon,
  MinusIcon,
  MonitorIcon,
  MoonIcon,
  SquareIcon,
  StopIcon,
  SunIcon,
  WarningCircleIcon,
  XIcon,
} from "@phosphor-icons/react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
} from "@/components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Spinner } from "@/components/ui/spinner";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { CommandPalette } from "@/components/command-palette";
import { StopBenchAction } from "@/components/machine-actions";
import { SiteIcon } from "@/components/site-icon";
import { KindBadge } from "@/components/sites/site-badges";
import { useDaemonContext } from "@/hooks/daemon-provider";
import { useHealth } from "@/hooks/use-doctor";
import { HOME, useRoute, type Route } from "@/hooks/route";
import { usePrivacy } from "@/lib/privacy";
import { cn, IS_MAC } from "@/lib/utils";

export function formatUptime(total: number) {
  const d = Math.floor(total / 86400);
  const h = Math.floor(total / 3600);
  const m = Math.floor(total / 60);
  if (d > 0) return `${d}d ${h % 24}h`;
  if (h > 0) return `${h}h ${m % 60}m`;
  if (m > 0) return `${m}m`;
  return `${total}s`;
}

/** AppHeader is the window's title bar: brand, site switcher, daemon
 *  status, search and window controls. Empty space drags the window. */
export function AppHeader() {
  const { connected } = useDaemonContext();

  return (
    <header className="shrink-0 bg-background select-none">
      <div data-tauri-drag-region className={cn("flex h-12 items-center gap-1", IS_MAC ? "pl-20" : "pl-4")}>
        <HomeCrumb />
        {connected ? <SiteSwitcher /> : null}
        <StatusPill />
        <div data-tauri-drag-region className="h-full flex-1" />
        {connected ? <CommandPalette /> : null}
        {connected ? <ActivityButton /> : null}
        <ThemeMenu />
        {IS_MAC ? <div className="w-3" /> : <WindowControls />}
      </div>
    </header>
  );
}

/** BenchMark: the app mark, a workbench seen side-on. */
export function BenchMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" aria-hidden className={cn("size-6 shrink-0", className)}>
      <rect width="24" height="24" rx="6" className="fill-primary" />
      <rect x="5" y="7.5" width="14" height="2.75" rx="1" className="fill-primary-foreground" />
      <rect x="7" y="11.5" width="2.25" height="5.5" rx="0.75" className="fill-primary-foreground" />
      <rect x="14.75" y="11.5" width="2.25" height="5.5" rx="0.75" className="fill-primary-foreground" />
    </svg>
  );
}

function HomeCrumb() {
  const { navigate } = useRoute();
  return (
    <button
      type="button"
      onClick={() => navigate(HOME)}
      className="flex h-8 items-center gap-2 rounded-md pr-2 pl-0.5 text-sm font-medium outline-none transition-colors duration-100 hover:bg-subtle focus-visible:ring-3 focus-visible:ring-ring"
    >
      <BenchMark />
      Bench
    </button>
  );
}

/** SiteSwitcher jumps between sites, filtering as you type. Outside a
 *  site it reads "Select site", so it never looks like a filter on the
 *  page below (audit UI-22). Switching keeps the site page you're on. */
function SiteSwitcher() {
  const { sites } = useDaemonContext();
  const { route, navigate } = useRoute();
  const [open, setOpen] = useState(false);
  const privacy = usePrivacy();
  const current = route.kind === "site" ? sites?.find((s) => s.name === route.name) : undefined;

  const go = (next: Route) => {
    setOpen(false);
    navigate(next);
  };
  const openSite = (name: string) =>
    go(
      route.kind === "site"
        ? { kind: "site", name, tab: route.tab }
        : { kind: "site", name, tab: "overview" },
    );

  return (
    <>
      <span aria-hidden className="px-0.5 text-muted-foreground/50">
        /
      </span>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <button
            type="button"
            aria-label={current ? `Site: ${privacy.host(current.host)}. Switch site` : "Select site"}
            className="flex h-8 max-w-64 items-center gap-2 rounded-md px-2 text-sm outline-none transition-colors duration-100 hover:bg-subtle focus-visible:ring-3 focus-visible:ring-ring aria-expanded:bg-subtle"
          >
            {current ? (
              <>
                <SiteIcon name={current.name} />
                <span className="truncate font-medium">{privacy.host(current.host)}</span>
              </>
            ) : (
              <span className="whitespace-nowrap text-muted-foreground">Select site</span>
            )}
            <CaretUpDownIcon className="size-3.5 shrink-0 text-muted-foreground" />
          </button>
        </PopoverTrigger>
        <PopoverContent align="start" className="w-72 p-0">
          <Command>
            <CommandInput placeholder="Find a site…" />
            <CommandList>
              {sites === null ? (
                <div className="px-3 py-6 text-center text-[13px] text-muted-foreground">Loading sites…</div>
              ) : (
                <CommandEmpty>{sites.length === 0 ? "No sites yet." : "No site matches."}</CommandEmpty>
              )}
              {sites && sites.length > 0 ? (
                <CommandGroup heading="Sites">
                  {sites.map((s) => (
                    <CommandItem
                      key={s.name}
                      value={`${s.host} ${s.path ?? s.proxy_to ?? ""}`}
                      onSelect={() => openSite(s.name)}
                      data-checked={current?.name === s.name}
                    >
                      <SiteIcon name={s.name} />
                      {/* The host takes the free space, so every badge lines up
                          just before the check column CommandItem reserves. */}
                      <span className="min-w-0 flex-1 truncate">{privacy.host(s.host)}</span>
                      <KindBadge kind={s.kind} />
                    </CommandItem>
                  ))}
                </CommandGroup>
              ) : null}
              <CommandSeparator />
              <CommandGroup>
                <CommandItem value="view all sites" onSelect={() => go({ kind: "home", section: "sites" })}>
                  View all sites
                </CommandItem>
              </CommandGroup>
            </CommandList>
          </Command>
        </PopoverContent>
      </Popover>
    </>
  );
}

/** StatusPill: benchd's state as a dark pill in the header.
 *  Running opens a menu with the stop action. */
const PILL_RING = {
  neutral: "bg-pill-muted/30",
  success: "bg-success-solid/30",
  warning: "bg-warning-solid/35",
  destructive: "bg-destructive-solid/35",
};
const PILL_DOT = {
  neutral: "bg-pill-muted",
  success: "bg-success-solid",
  warning: "bg-warning-solid",
  destructive: "bg-destructive-solid",
};
const PILL_TEXT = {
  neutral: "text-pill-muted",
  success: "text-success-solid",
  warning: "text-warning-solid",
  destructive: "text-destructive-solid",
};

function StatusPill() {
  const { connected, reconnecting, status, uptime, pending } = useDaemonContext();
  const { issues, loading: checking, error: checkError } = useHealth();
  const { navigate } = useRoute();
  const [stopOpen, setStopOpen] = useState(false);
  // Running only means the daemon answers; failing checks turn the pill
  // amber (setup to do) or red (something blocks serving sites).
  // Green needs a clean result: while checking the pill is neutral, and
  // checks that couldn't run are a warning, never a pass (ui-standard F4).
  const failing = issues.some((c) => c.status === "fail");
  const tone = checking
    ? "neutral"
    : checkError
      ? "warning"
      : issues.length === 0
        ? "success"
        : failing
          ? "destructive"
          : "warning";
  const summary = checking
    ? "Running · checking"
    : checkError
      ? "Running · checks unavailable"
      : issues.length > 0
        ? `Running · ${issues.length} ${issues.length === 1 ? "issue" : "issues"}`
        : "Running";
  const pillClass =
    "ml-2 inline-flex h-7 shrink-0 items-center gap-1.5 rounded-full bg-pill pr-2.5 pl-1 text-[13px] font-medium whitespace-nowrap text-pill-foreground shadow-xs";

  if (reconnecting) {
    return (
      <span role="status" className={pillClass}>
        <span className={cn("flex size-5 items-center justify-center rounded-full", PILL_RING.warning)}>
          <Spinner className={cn("size-3 motion-reduce:animate-none", PILL_TEXT.warning)} />
        </span>
        Reconnecting…
      </span>
    );
  }

  if (!connected) {
    return (
      <span role="status" className={pillClass}>
        <span className="flex size-5 items-center justify-center rounded-full bg-destructive-solid">
          <span className="size-2 rounded-[2px] bg-white" />
        </span>
        {pending === "start" ? "Starting" : "Stopped"}
      </span>
    );
  }

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <button
            type="button"
            className={cn(
              pillClass,
              "outline-none transition-opacity duration-100 hover:opacity-90 focus-visible:ring-3 focus-visible:ring-ring",
            )}
          >
            <span className={cn("flex size-5 items-center justify-center rounded-full", PILL_RING[tone])}>
              {pending === "stop" ? (
                <Spinner className={cn("size-3 motion-reduce:animate-none", PILL_TEXT[tone])} />
              ) : (
                <span className={cn("size-2 rounded-full", PILL_DOT[tone])} />
              )}
            </span>
            <span role="status">
              {pending === "stop" ? "Stopping" : summary}
            </span>
            {tone === "success" ? (
              <span className="font-normal text-pill-muted tabular-nums max-md:hidden">{formatUptime(uptime)}</span>
            ) : null}
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="w-60">
          <DropdownMenuGroup>
            <DropdownMenuLabel className="flex flex-col gap-0.5">
              <span className="text-foreground">Bench {status?.version}</span>
              <span className="font-normal">
                {status?.os}/{status?.arch} · pid {status?.pid}
              </span>
            </DropdownMenuLabel>
          </DropdownMenuGroup>
          {issues.length > 0 ? (
            <>
              <DropdownMenuSeparator />
              <DropdownMenuGroup>
                <DropdownMenuLabel>Needs attention</DropdownMenuLabel>
                {issues.map((c) => (
                  <DropdownMenuItem
                    key={c.id}
                    onSelect={() =>
                      navigate({ kind: "home", section: "settings", settings: c.action === "setup" ? "https" : "diagnostics" })
                    }
                  >
                    <WarningCircleIcon className={c.status === "fail" ? "text-destructive" : "text-warning"} />
                    <span className="flex flex-col">
                      <span>{c.title}</span>
                      <span className="text-xs text-muted-foreground">{c.status === "fail" ? "Failing" : "Warning"}</span>
                    </span>
                  </DropdownMenuItem>
                ))}
              </DropdownMenuGroup>
            </>
          ) : null}
          <DropdownMenuSeparator />
          <DropdownMenuGroup>
            <DropdownMenuItem variant="destructive" onSelect={() => setStopOpen(true)} disabled={pending !== null}>
              <StopIcon />
              Stop Bench…
            </DropdownMenuItem>
          </DropdownMenuGroup>
        </DropdownMenuContent>
      </DropdownMenu>
      <StopBenchAction open={stopOpen} onOpenChange={setStopOpen} />
    </>
  );
}

/** ActivityButton opens the Activity page; a badge counts running tasks. */
function ActivityButton() {
  const { tasks } = useDaemonContext();
  const { navigate } = useRoute();
  const running = (tasks ?? []).filter((t) => t.state === "running").length;
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          variant="ghost"
          size="icon-sm"
          className="relative"
          aria-label={running > 0 ? `Activity, ${running} running` : "Activity"}
          onClick={() => navigate({ kind: "home", section: "activity" })}
        >
          <ClockCounterClockwiseIcon />
          {running > 0 ? (
            <span className="absolute -top-0.5 -right-0.5 flex size-4 items-center justify-center rounded-full bg-brand text-[10px] font-semibold text-brand-foreground tabular-nums">
              {running}
            </span>
          ) : null}
        </Button>
      </TooltipTrigger>
      <TooltipContent>Activity</TooltipContent>
    </Tooltip>
  );
}

function ThemeMenu() {
  const { theme, resolvedTheme, setTheme } = useTheme();
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon-sm" aria-label="Theme">
          {resolvedTheme === "dark" ? <MoonIcon /> : <SunIcon />}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-40">
        <DropdownMenuRadioGroup value={theme} onValueChange={setTheme}>
          <DropdownMenuRadioItem value="light">
            <SunIcon /> Light
          </DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="dark">
            <MoonIcon /> Dark
          </DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="system">
            <MonitorIcon /> System
          </DropdownMenuRadioItem>
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function WindowControls() {
  const [maximized, setMaximized] = useState(false);

  useEffect(() => {
    const win = getCurrentWindow();
    // The window's rounded corners (index.css) square off when maximized.
    const sync = () =>
      void win.isMaximized().then((m) => {
        setMaximized(m);
        document.documentElement.toggleAttribute("data-maximized", m);
      }, () => {});
    sync();
    const unlisten = win.onResized(sync);
    return () => void unlisten.then((fn) => fn());
  }, []);

  const control =
    "flex h-12 w-11 items-center justify-center text-muted-foreground outline-none transition-colors duration-100 hover:bg-subtle hover:text-foreground focus-visible:bg-subtle focus-visible:text-foreground focus-visible:outline-2 focus-visible:-outline-offset-4 focus-visible:outline-ring [&_svg]:size-4";

  return (
    <div className="ml-2 flex self-start">
      <button type="button" aria-label="Minimize" className={control} onClick={() => void getCurrentWindow().minimize()}>
        <MinusIcon />
      </button>
      <button
        type="button"
        aria-label={maximized ? "Restore" : "Maximize"}
        className={control}
        onClick={() => void getCurrentWindow().toggleMaximize()}
      >
        {maximized ? <CopyIcon className="-scale-x-100" /> : <SquareIcon />}
      </button>
      <button
        type="button"
        aria-label="Close"
        className={cn(control, "hover:bg-destructive-solid hover:text-white focus-visible:bg-destructive-solid focus-visible:text-white")}
        onClick={() => void getCurrentWindow().close()}
      >
        <XIcon />
      </button>
    </div>
  );
}
