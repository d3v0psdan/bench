import { useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { listen } from "@tauri-apps/api/event";
import { ThemeProvider } from "next-themes";
import { FolderOpenIcon, PlayIcon } from "@phosphor-icons/react";
import { invoke } from "@tauri-apps/api/core";
import { enable as enableAutostart } from "@tauri-apps/plugin-autostart";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { Toaster } from "@/components/ui/sonner";
import { SidebarProvider } from "@/components/ui/sidebar";
import { TooltipProvider } from "@/components/ui/tooltip";
import { ActivityPane, ActivityTray } from "@/components/activity";
import { useNewAppLanding } from "@/components/add-site/add-site";
import { AppHeader, BenchMark } from "@/components/app-header";
import { AppSidebar } from "@/components/app-sidebar";
import { ErrorBoundary } from "@/components/error-boundary";
import { StopBenchAction } from "@/components/machine-actions";
import { MailPane } from "@/components/mail-pane";
import { OverviewPane } from "@/components/overview-pane";
import { PhpPane } from "@/components/php-pane";
import { ServicesPane } from "@/components/services-pane";
import { SettingsPane } from "@/components/settings-pane";
import { SitesPane } from "@/components/sites-pane";
import { SiteView } from "@/components/site-view";
import { DaemonProvider, useDaemonContext } from "@/hooks/daemon-provider";
import { DoctorProvider, useHealth } from "@/hooks/use-doctor";
import { useOsNotifications } from "@/hooks/use-os-notifications";
import { RouteContext, type Route, type Section } from "@/hooks/route";
import { errorMessage, withTimeout } from "@/lib/async";
import { revealPath } from "@/lib/open";
import { usePrivacy } from "@/lib/privacy";
import { hasConnectedBefore, markConnected, savedRoute, saveRoute, setSidebarExpanded, sidebarExpanded } from "@/lib/prefs";
import { cn } from "@/lib/utils";

const PANES: Record<Section, () => React.JSX.Element> = {
  overview: OverviewPane,
  sites: SitesPane,
  services: ServicesPane,
  php: PhpPane,
  mail: MailPane,
  settings: SettingsPane,
  activity: ActivityPane,
};

// The app answers where its logs live at once; this only bounds a hang.
const LOGS_DIR_TIMEOUT_MS = 5_000;

/** StartScreen: Bench isn't running. A first run (Bench never started
 *  on this machine) says what Bench is and what comes next; afterwards it
 *  just offers to start it. Failures offer the log folder (audit UI-25). */
function StartScreen() {
  const { pending, start, error, tokenError } = useDaemonContext();
  const firstRun = !hasConnectedBefore();
  const openLogs = () =>
    withTimeout(invoke<string>("logs_dir"), LOGS_DIR_TIMEOUT_MS, "Timed out").then(revealPath, (e: unknown) =>
      toast.error("Couldn't find the logs folder", { description: errorMessage(e) }),
    );

  return (
    <div className="flex h-full items-center justify-center p-8">
      <div className="flex w-full max-w-sm flex-col items-center gap-4 text-center">
        <BenchMark className="size-10" />
        {tokenError ? (
          <div>
            <h1 className="text-lg font-medium">Bench can't connect</h1>
            <p className="mt-1 text-[13px] text-muted-foreground">{tokenError}</p>
          </div>
        ) : firstRun ? (
          <div className="flex flex-col gap-3">
            <h1 className="text-lg font-medium">Welcome to Bench</h1>
            <ol className="flex flex-col gap-1.5 rounded-lg bg-well p-3 text-left text-[13px]">
              {["Start Bench", "Set up HTTPS and .test addresses", "Add your first site"].map((step, i) => (
                <li key={step} className="flex items-center gap-2.5">
                  <span className="flex size-5 shrink-0 items-center justify-center rounded-full bg-card text-xs font-medium tabular-nums ring-1 ring-border">
                    {i + 1}
                  </span>
                  {step}
                </li>
              ))}
            </ol>
          </div>
        ) : (
          <h1 className="text-lg font-medium">Bench is stopped</h1>
        )}
        {tokenError ? (
          <Button variant="outline" onClick={() => void openLogs()}>
            <FolderOpenIcon data-icon="inline-start" />
            Open logs folder
          </Button>
        ) : (
          <Button variant="brand" onClick={start} disabled={pending !== null}>
            {pending === "start" ? (
              <Spinner data-icon="inline-start" className="motion-reduce:animate-none" />
            ) : (
              <PlayIcon data-icon="inline-start" weight="fill" />
            )}
            {pending === "start" ? "Starting…" : "Start Bench"}
          </Button>
        )}
        {error ? (
          <div className="flex flex-col items-center gap-2">
            <p className="text-[13px] break-words text-destructive" role="alert">
              {error}
            </p>
            <div className="flex gap-2">
              <Button size="sm" variant="outline" onClick={() => void openLogs()}>
                <FolderOpenIcon data-icon="inline-start" />
                Open logs folder
              </Button>
              <Button size="sm" variant="outline" onClick={start} disabled={pending !== null}>
                Try again
              </Button>
            </div>
          </div>
        ) : null}
      </div>
    </div>
  );
}

/** ReconnectingBanner keeps the page up while a dropped connection is
 *  retried (audit UI-27); the sites don't depend on this window. */
function ReconnectingBanner() {
  return (
    <div
      role="status"
      className="flex items-center gap-2 border-b bg-warning-solid/10 px-4 py-2 text-[13px] text-warning"
    >
      <Spinner className="size-3.5 motion-reduce:animate-none" />
      Reconnecting to Bench… Your sites keep running; this page catches up when it's back.
    </div>
  );
}

function Shell() {
  const { connected, reconnecting, error } = useDaemonContext();
  useEffect(() => {
    if (!connected || hasConnectedBefore()) return;
    markConnected();
    // Sign-in start defaults to on (audit UI-08): turned on once, at the
    // first run, so turning it off later sticks.
    enableAutostart().catch((e: unknown) =>
      toast.error("Couldn't set Bench to open at sign-in", {
        description: `${errorMessage(e)}. Turn it on in Settings > General.`,
      }),
    );
  }, [connected]);
  // The page survives a reload (the dev server's, or the webview's own).
  const [route, setRoute] = useState<Route>(savedRoute);
  useEffect(() => saveRoute(route), [route]);

  const [sidebarOpen, setSidebarOpen] = useState(sidebarExpanded);

  const Pane = route.kind === "home" ? PANES[route.section] : null;
  // Settings pages keep their title and sub-nav in place and scroll only
  // their content, so the page fills the window instead of growing past it.
  // Mail does the same for its inbox, list and reading pane.
  const fillsWindow = route.kind === "home" && (route.section === "settings" || route.section === "mail");

  // The tray menu's Activity and Stop items (src-tauri/src/lib.rs).
  const [trayStop, setTrayStop] = useState(false);
  const connectedRef = useRef(connected);
  useEffect(() => {
    connectedRef.current = connected;
  }, [connected]);
  useEffect(() => {
    const unlisten = listen<string>("tray", (e) => {
      if (e.payload === "activity") setRoute({ kind: "home", section: "activity" });
      if (e.payload.startsWith("site:")) setRoute({ kind: "site", name: e.payload.slice(5), tab: "overview" });
      if (e.payload === "stop") {
        if (connectedRef.current) setTrayStop(true);
        else toast("Bench is already stopped");
      }
    });
    return () => void unlisten.then((fn) => fn());
  }, []);
  useTrayStatus(connected);
  useOsNotifications(setRoute);
  useNewAppLanding(setRoute);

  return (
    <RouteContext.Provider value={{ route, navigate: setRoute }}>
      <SidebarProvider
        open={sidebarOpen}
        onOpenChange={(open) => {
          setSidebarOpen(open);
          setSidebarExpanded(open);
        }}
        className="h-screen min-h-0 flex-col bg-background"
      >
        <AppHeader />
        <div className="flex min-h-0 flex-1">
          {connected ? <AppSidebar /> : null}
          <main
            className={cn(
              "mr-1.5 mb-1.5 flex",
              !connected && "ml-1.5",
              "min-w-0 flex-1 flex-col overflow-hidden rounded-lg border bg-sheet",
            )}
          >
            {reconnecting ? <ReconnectingBanner /> : null}
            <div className="min-h-0 flex-1 overflow-y-auto [scrollbar-gutter:stable]">
              {!connected ? (
                <StartScreen />
              ) : (
                <div
                  className={cn(
                    "mx-auto flex w-full max-w-7xl flex-col px-8 py-8",
                    fillsWindow ? "h-full" : "min-h-full",
                  )}
                >
                  {error ? (
                    <Alert variant="destructive" className="mb-6">
                      <AlertTitle>Bench</AlertTitle>
                      <AlertDescription className="break-words">{error}</AlertDescription>
                    </Alert>
                  ) : null}
                  {/* Keyed by page, not the whole route, so switching a site tab or
                      opening a detail sheet doesn't replay the entrance. */}
                  <div
                    key={route.kind === "site" ? `site:${route.name}` : route.section}
                    className="flex min-h-0 flex-1 flex-col animate-in duration-200 ease-out fade-in-0 slide-in-from-bottom-1 motion-reduce:animate-none"
                  >
                    <ErrorBoundary key={JSON.stringify(route)}>
                      {route.kind === "site" ? (
                        <SiteView name={route.name} tab={route.tab} />
                      ) : Pane ? (
                        <Pane />
                      ) : null}
                    </ErrorBoundary>
                  </div>
                </div>
              )}
            </div>
          </main>
        </div>
        {connected ? <ActivityTray /> : null}
        {connected ? <StopBenchAction open={trayStop} onOpenChange={setTrayStop} /> : null}
      </SidebarProvider>
    </RouteContext.Provider>
  );
}

/** useTrayStatus keeps the tray tooltip on the same truth as the pill, so
 *  Bench's state is visible without opening the window (ui-standard D1). */
function useTrayStatus(connected: boolean) {
  const { issues, loading, error } = useHealth();
  const { reconnecting } = useDaemonContext();
  const text = reconnecting
    ? "Bench: reconnecting"
    : !connected
    ? "Bench: stopped"
    : loading
      ? "Bench: running, checking"
      : error
        ? "Bench: running, checks unavailable"
        : issues.length > 0
          ? `Bench: running, ${issues.length} ${issues.length === 1 ? "issue" : "issues"}`
          : "Bench: running";
  // The tray menu lists sites too (audit UI-09): favorites first, capped,
  // so the menu stays short.
  const { sites } = useDaemonContext();
  const privacy = usePrivacy();
  const traySites = JSON.stringify(
    connected
      ? (sites ?? [])
          .slice()
          .sort((a, b) => Number(!!b.favorite) - Number(!!a.favorite))
          .slice(0, TRAY_SITES)
          .map((s) => ({ name: s.name, host: privacy.host(s.host) }))
      : [],
  );
  useEffect(() => {
    invoke("update_tray", { status: text, sites: JSON.parse(traySites) }).catch((e: unknown) =>
      console.error("updating the tray menu failed", e),
    );
  }, [text, traySites]);
}

/** How many sites the tray menu lists; the rest are a click away. */
const TRAY_SITES = 15;

function App() {
  return (
    <ThemeProvider attribute="class" defaultTheme="system" enableSystem disableTransitionOnChange>
      <TooltipProvider>
        <DaemonProvider>
          <DoctorProvider>
            <ErrorBoundary>
              <Shell />
            </ErrorBoundary>
          </DoctorProvider>
          <Toaster position="bottom-center" />
        </DaemonProvider>
      </TooltipProvider>
    </ThemeProvider>
  );
}

export default App;
