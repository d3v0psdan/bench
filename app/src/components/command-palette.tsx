import { useEffect, useState } from "react";
import { useTheme } from "next-themes";
import {
  ArrowSquareOutIcon,
  ArrowsLeftRightIcon,
  BookOpenIcon,
  BugIcon,
  CodeIcon,
  CubeIcon,
  DatabaseIcon,
  DownloadSimpleIcon,
  EyeIcon,
  EyeSlashIcon,
  FileCodeIcon,
  FolderSimplePlusIcon,
  GearSixIcon,
  InfoIcon,
  LinkSimpleIcon,
  LockSimpleIcon,
  MagnifyingGlassIcon,
  MonitorIcon,
  MoonIcon,
  PlusIcon,
  SparkleIcon,
  SquaresFourIcon,
  StethoscopeIcon,
  StopIcon,
  SunIcon,
  TerminalWindowIcon,
} from "@phosphor-icons/react";
import {
  Command,
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
  CommandShortcut,
} from "@/components/ui/command";
import { Kbd, KbdGroup } from "@/components/ui/kbd";
import { NAV_GROUPS, SETTINGS_ITEM, shortcutLabel } from "@/components/app-sidebar";
import { StopBenchAction } from "@/components/machine-actions";
import { openWithToast } from "@/components/open-actions";
import { SiteIcon } from "@/components/site-icon";
import { SERVICE_ICONS, SERVICE_LABELS } from "@/components/services/service-meta";
import { KindBadge } from "@/components/sites/site-badges";
import { useDaemonContext } from "@/hooks/daemon-provider";
import { useRoute, type Route, type SettingsPage } from "@/hooks/route";
import { LINKS } from "@/lib/links";
import { openExternal } from "@/lib/open";
import { setPrivacy, usePrivacy } from "@/lib/privacy";
import { IS_MAC } from "@/lib/utils";

const SETTINGS_PAGES: Array<{ page: SettingsPage; label: string; keywords: string[]; icon: typeof GearSixIcon }> = [
  { page: "general", label: "General", keywords: ["theme", "editor", "startup"], icon: GearSixIcon },
  {
    page: "privacy",
    label: "Privacy",
    keywords: ["privacy mode", "screen share", "recording", "streaming", "hide", "mask"],
    icon: EyeSlashIcon,
  },
  {
    page: "https",
    label: "HTTPS & DNS",
    keywords: ["https", "ssl", "certificate", "dns", "setup", "trust"],
    icon: LockSimpleIcon,
  },
  {
    page: "diagnostics",
    label: "Diagnostics",
    keywords: ["doctor", "checks", "health", "ports"],
    icon: StethoscopeIcon,
  },
  { page: "about", label: "About", keywords: ["version"], icon: InfoIcon },
];

/** Page keywords, so "php" finds Runtimes and "tasks" finds Activity. */
const PAGE_KEYWORDS: Record<string, string[]> = {
  php: ["php", "versions"],
  activity: ["tasks", "downloads", "progress"],
  mail: ["inbox", "email", "mailpit"],
};

const SITE_PREFIX = "site:";

/** CommandPalette: the header's search box. Jump to any page, site or
 *  service, run the main actions, or find help. Opens with ⌘K on macOS
 *  and Ctrl+K elsewhere; Mod+Enter on a site opens it in the browser. */
export function CommandPalette() {
  const [open, setOpen] = useState(false);
  const [highlighted, setHighlighted] = useState("");
  const [stopOpen, setStopOpen] = useState(false);
  const { conn, sites, services } = useDaemonContext();
  const { route, navigate } = useRoute();
  const { setTheme } = useTheme();
  const privacy = usePrivacy();

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key.toLowerCase() === "k" && (IS_MAC ? e.metaKey : e.ctrlKey)) {
        e.preventDefault();
        setOpen((o) => !o);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const run = (action: () => void) => {
    setOpen(false);
    action();
  };
  const go = (next: Route) => run(() => navigate(next));

  const site = route.kind === "site" ? sites?.find((s) => s.name === route.name) : undefined;
  const openPath = site ? `/api/sites/${encodeURIComponent(site.name)}/open` : "";

  // Mod+Enter on a highlighted site opens it in the browser instead.
  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key !== "Enter" || !(IS_MAC ? e.metaKey : e.ctrlKey) || !highlighted.startsWith(SITE_PREFIX)) return;
    const target = sites?.find((s) => `${SITE_PREFIX}${s.host}` === highlighted);
    if (!target) return;
    e.preventDefault();
    run(() => openExternal(target.url));
  };

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="mr-1 flex h-8 w-52 items-center gap-2 rounded-md bg-card pr-1.5 pl-2.5 text-[13px] text-muted-foreground shadow-button outline-none transition-colors duration-100 hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring"
      >
        <MagnifyingGlassIcon className="size-4 shrink-0" />
        Search
        <KbdGroup className="ml-auto">
          <Kbd>{IS_MAC ? "⌘" : "Ctrl"}</Kbd>
          <Kbd>K</Kbd>
        </KbdGroup>
      </button>

      <CommandDialog
        open={open}
        onOpenChange={setOpen}
        title="Search Bench"
        description="Jump to a page, site or service, or run an action"
      >
        <Command value={highlighted} onValueChange={setHighlighted} onKeyDown={onKeyDown}>
          <CommandInput placeholder="Search pages, sites, services and actions…" />
          <CommandList>
            <CommandEmpty>Nothing matches.</CommandEmpty>

            {site ? (
              <>
                <CommandGroup heading={privacy.host(site.host)}>
                  <CommandItem
                    value="this site overview"
                    onSelect={() => go({ kind: "site", name: site.name, tab: "overview" })}
                  >
                    <SquaresFourIcon />
                    Overview
                  </CommandItem>
                  <CommandItem
                    value="this site settings"
                    onSelect={() => go({ kind: "site", name: site.name, tab: "settings" })}
                  >
                    <GearSixIcon />
                    Settings
                  </CommandItem>
                  <CommandItem
                    value="this site php version"
                    onSelect={() => go({ kind: "site", name: site.name, tab: "settings" })}
                  >
                    <FileCodeIcon />
                    PHP version
                  </CommandItem>
                  <CommandItem value="this site open in browser" onSelect={() => run(() => openExternal(site.url))}>
                    <ArrowSquareOutIcon />
                    Open site
                  </CommandItem>
                  {site.path ? (
                    <>
                      <CommandItem
                        value="this site open in editor"
                        onSelect={() => run(() => openWithToast(conn, openPath, { in: "editor" }, site.host))}
                      >
                        <CodeIcon />
                        Open in editor
                      </CommandItem>
                      <CommandItem
                        value="this site open terminal here"
                        onSelect={() => run(() => openWithToast(conn, openPath, { in: "terminal" }, "a terminal"))}
                      >
                        <TerminalWindowIcon />
                        Open terminal here
                      </CommandItem>
                      <CommandItem
                        value="this site open database"
                        onSelect={() =>
                          run(() => openWithToast(conn, openPath, { in: "database" }, `${site.host}'s database`))
                        }
                      >
                        <DatabaseIcon />
                        Open database
                      </CommandItem>
                    </>
                  ) : null}
                </CommandGroup>
                <CommandSeparator />
              </>
            ) : null}

            <CommandGroup heading="Pages">
              {[...NAV_GROUPS.flatMap((g) => g.items), SETTINGS_ITEM].map((p) => (
                <CommandItem
                  key={p.section}
                  value={`page ${p.label}`}
                  keywords={PAGE_KEYWORDS[p.section]}
                  onSelect={() => go({ kind: "home", section: p.section })}
                >
                  <p.icon />
                  {p.label}
                  <CommandShortcut>{shortcutLabel(p)}</CommandShortcut>
                </CommandItem>
              ))}
              {SETTINGS_PAGES.map((p) => (
                <CommandItem
                  key={p.page}
                  value={`settings ${p.label}`}
                  keywords={p.keywords}
                  onSelect={() => go({ kind: "home", section: "settings", settings: p.page })}
                >
                  <p.icon />
                  Settings: {p.label}
                </CommandItem>
              ))}
            </CommandGroup>

            {sites && sites.length > 0 ? (
              <>
                <CommandSeparator />
                <CommandGroup heading="Sites">
                  {sites.map((s) => (
                    <CommandItem
                      key={s.name}
                      value={`${SITE_PREFIX}${s.host}`}
                      keywords={[s.path ?? s.proxy_to ?? "", s.kind]}
                      onSelect={() => go({ kind: "site", name: s.name, tab: "overview" })}
                    >
                      <SiteIcon name={s.name} />
                      {/* The host takes the free space, so every badge lines up
                          just before the check column CommandItem reserves. */}
                      <span className="min-w-0 flex-1 truncate">{privacy.host(s.host)}</span>
                      <KindBadge kind={s.kind} />
                    </CommandItem>
                  ))}
                </CommandGroup>
              </>
            ) : null}

            {services && services.length > 0 ? (
              <>
                <CommandSeparator />
                <CommandGroup heading="Services">
                  {services.map((s) => {
                    const ServiceIcon = SERVICE_ICONS[s.service] ?? CubeIcon;
                    return (
                      <CommandItem
                        key={s.name}
                        value={`service ${s.name} ${SERVICE_LABELS[s.service] ?? s.service}`}
                        onSelect={() =>
                          go(
                            s.service === "mailpit"
                              ? { kind: "home", section: "mail" }
                              : { kind: "home", section: "services", service: s.name },
                          )
                        }
                      >
                        <ServiceIcon />
                        <span className="truncate">{s.name}</span>
                        <CommandShortcut className="tracking-normal">
                          {SERVICE_LABELS[s.service] ?? s.service} :{s.port}
                        </CommandShortcut>
                      </CommandItem>
                    );
                  })}
                </CommandGroup>
              </>
            ) : null}

            <CommandSeparator />
            <CommandGroup heading="Actions">
              <CommandItem
                value="action new laravel app"
                keywords={["create", "starter kit", "installer", "react", "vue", "livewire"]}
                onSelect={() => go({ kind: "home", section: "sites", intent: "new" })}
              >
                <SparkleIcon />
                New Laravel app…
              </CommandItem>
              <CommandItem
                value="action link a project"
                onSelect={() => go({ kind: "home", section: "sites", intent: "link" })}
              >
                <LinkSimpleIcon />
                Link a project…
              </CommandItem>
              <CommandItem
                value="action park a folder"
                onSelect={() => go({ kind: "home", section: "sites", intent: "park" })}
              >
                <FolderSimplePlusIcon />
                Park a folder…
              </CommandItem>
              <CommandItem
                value="action proxy a local port"
                keywords={["vite", "node", "dev server"]}
                onSelect={() => go({ kind: "home", section: "sites", intent: "proxy" })}
              >
                <ArrowsLeftRightIcon />
                Proxy a local port…
              </CommandItem>
              <CommandItem
                value="action new service"
                keywords={["mysql", "postgres", "redis", "valkey", "database"]}
                onSelect={() => go({ kind: "home", section: "services", intent: "new-service" })}
              >
                <PlusIcon />
                New service…
              </CommandItem>
              <CommandItem value="action install a php version" onSelect={() => go({ kind: "home", section: "php" })}>
                <DownloadSimpleIcon />
                Install a PHP version
              </CommandItem>
              <CommandItem
                value="action set up https and dns"
                keywords={["setup", "certificate", "trust"]}
                onSelect={() => go({ kind: "home", section: "settings", settings: "https" })}
              >
                <LockSimpleIcon />
                Set up HTTPS and DNS
              </CommandItem>
              <CommandItem
                value="action privacy mode"
                keywords={["screen share", "recording", "streaming", "hide", "mask"]}
                onSelect={() => run(() => setPrivacy("on", !privacy.prefs.on))}
              >
                {privacy.prefs.on ? <EyeIcon /> : <EyeSlashIcon />}
                {privacy.prefs.on ? "Turn off privacy mode" : "Turn on privacy mode"}
              </CommandItem>
              <CommandItem value="action stop bench" onSelect={() => run(() => setStopOpen(true))}>
                <StopIcon />
                Stop Bench…
              </CommandItem>
            </CommandGroup>

            <CommandSeparator />
            <CommandGroup heading="Help">
              <CommandItem
                value="help documentation getting started"
                onSelect={() => run(() => openExternal(LINKS.docs))}
              >
                <BookOpenIcon />
                Documentation
              </CommandItem>
              <CommandItem value="help running beside herd" onSelect={() => run(() => openExternal(LINKS.herd))}>
                <ArrowsLeftRightIcon />
                Running beside Laravel Herd
              </CommandItem>
              <CommandItem value="help report an issue bug" onSelect={() => run(() => openExternal(LINKS.issues))}>
                <BugIcon />
                Report an issue
              </CommandItem>
            </CommandGroup>

            <CommandSeparator />
            <CommandGroup heading="Theme">
              <CommandItem value="theme light" onSelect={() => run(() => setTheme("light"))}>
                <SunIcon />
                Light theme
              </CommandItem>
              <CommandItem value="theme dark" onSelect={() => run(() => setTheme("dark"))}>
                <MoonIcon />
                Dark theme
              </CommandItem>
              <CommandItem value="theme system" onSelect={() => run(() => setTheme("system"))}>
                <MonitorIcon />
                System theme
              </CommandItem>
            </CommandGroup>
          </CommandList>
          <div className="flex items-center gap-4 border-t px-3 py-2 text-xs text-muted-foreground">
            <span className="flex items-center gap-1.5">
              <Kbd>↵</Kbd> Open
            </span>
            {highlighted.startsWith(SITE_PREFIX) ? (
              <span className="flex items-center gap-1.5">
                <KbdGroup>
                  <Kbd>{IS_MAC ? "⌘" : "Ctrl"}</Kbd>
                  <Kbd>↵</Kbd>
                </KbdGroup>
                Open site
              </span>
            ) : null}
          </div>
        </Command>
      </CommandDialog>
      <StopBenchAction open={stopOpen} onOpenChange={setStopOpen} />
    </>
  );
}
