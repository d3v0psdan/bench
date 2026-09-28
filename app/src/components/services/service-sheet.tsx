import { useEffect, useState, type ReactNode } from "react";
import {
  ArrowSquareOutIcon,
  CubeIcon,
  DatabaseIcon,
  EyeIcon,
  EyeSlashIcon,
  FolderOpenIcon,
  ScrollIcon,
} from "@phosphor-icons/react";
import { Button } from "@/components/ui/button";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Spinner } from "@/components/ui/spinner";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { CopyButton } from "@/components/copy-button";
import { EnvBlock } from "@/components/env-block";
import { InfoTip } from "@/components/info-tip";
import { LogView } from "@/components/log-view";
import { ServiceMenu, StartStopButton, useServiceActions } from "@/components/services/service-actions";
import { SERVICE_ICONS, SERVICE_LABELS, ServiceStateDot, serviceAddress } from "@/components/services/service-meta";
import { useDaemonContext } from "@/hooks/daemon-provider";
import type { Service } from "@/lib/api";
import { revealPath } from "@/lib/open";
import { usePrivacy } from "@/lib/privacy";

/** ServiceSheet is an instance's detail view: everything the API knows,
 *  with copy buttons and the instance's actions (audit UI-10). */
export function ServiceSheet({
  name,
  showLogs,
  onClose,
}: {
  name: string | undefined;
  showLogs: boolean;
  onClose: () => void;
}) {
  const { services } = useDaemonContext();
  const s = name ? services?.find((x) => x.name === name) : undefined;
  return (
    <Sheet open={!!s} onOpenChange={(o) => !o && onClose()}>
      <SheetContent className="gap-0 p-0">{s ? <ServiceSheetBody s={s} showLogs={showLogs} /> : null}</SheetContent>
    </Sheet>
  );
}

// Port roles the daemon reports (services/drivers.go) that don't read
// right with only their first letter capitalized.
const PORT_ROLE_LABELS: Record<string, string> = { http: "HTTP" };

function ServiceSheetBody({ s, showLogs }: { s: Service; showLogs: boolean }) {
  const a = useServiceActions(s);
  const hasConnect = s.env.length > 0 || !!s.url || !!s.client || !!s.console_login?.length;
  // Opens on how to connect, the usual reason to come here; "View logs"
  // on a failed row lands on Logs instead.
  const [pickedTab, setPickedTab] = useState(showLogs ? "logs" : hasConnect ? "connect" : "details");
  useEffect(() => {
    if (showLogs) setPickedTab("logs");
  }, [showLogs]);
  const tab = pickedTab === "connect" && !hasConnect ? "details" : pickedTab;
  const ServiceIcon = SERVICE_ICONS[s.service] ?? CubeIcon;
  const label = SERVICE_LABELS[s.service] ?? s.service;
  const privacy = usePrivacy();

  const summary: Array<{ label: string; value: ReactNode; copy?: string }> = [
    { label: "Version", value: `${label} ${s.version || s.channel}` },
    { label: "Address", value: serviceAddress(s), copy: s.port ? `127.0.0.1:${s.port}` : undefined },
    ...Object.entries(s.ports ?? {}).map(([role, port]) => ({
      label: PORT_ROLE_LABELS[role] ?? role.charAt(0).toUpperCase() + role.slice(1),
      value: `127.0.0.1:${port}`,
      copy: `127.0.0.1:${port}`,
    })),
    ...(s.pid ? [{ label: "Process", value: `pid ${s.pid}` }] : []),
    { label: "Data", value: <span className="font-mono text-xs break-all">{privacy.path(s.data_dir)}</span> },
  ];

  return (
    <>
      <SheetHeader className="p-5 pb-4">
        <div className="flex items-center gap-3 pr-8">
          <span className="flex size-9 shrink-0 items-center justify-center rounded-md bg-well text-muted-foreground ring-1 ring-border ring-inset">
            <ServiceIcon className="size-4.5" />
          </span>
          <div className="min-w-0">
            <SheetTitle className="truncate text-base">{s.name}</SheetTitle>
            <SheetDescription className="flex items-center gap-2">
              {label} {s.version || s.channel}
              <ServiceStateDot state={s.state} />
            </SheetDescription>
          </div>
        </div>
        <div className="mt-4 flex flex-wrap items-center gap-2">
          {s.url ? (
            <Button size="sm" variant="brand" onClick={a.openClient} disabled={a.busy !== null}>
              {a.busy === "open" ? (
                <Spinner data-icon="inline-start" className="motion-reduce:animate-none" />
              ) : (
                <DatabaseIcon data-icon="inline-start" />
              )}
              Open in client
            </Button>
          ) : null}
          {s.console_url ? (
            <Button size="sm" variant="brand" onClick={a.openConsole}>
              <ArrowSquareOutIcon data-icon="inline-start" />
              Open console
            </Button>
          ) : null}
          <StartStopButton s={s} a={a} />
          <Button size="sm" variant="outline" onClick={() => revealPath(s.data_dir)}>
            <FolderOpenIcon data-icon="inline-start" />
            Show folder
          </Button>
          <div className="ml-auto">
            <ServiceMenu s={s} a={a} />
          </div>
        </div>
        {a.error || s.error ? (
          <div className="mt-3 flex items-center gap-3">
            <p
              role={a.error ? "alert" : "status"}
              className="min-w-0 flex-1 text-xs break-words whitespace-pre-wrap text-destructive"
            >
              {a.error ?? s.error}
            </p>
            <Button size="xs" variant="outline" className="shrink-0" onClick={() => setPickedTab("logs")}>
              <ScrollIcon data-icon="inline-start" />
              View logs
            </Button>
          </div>
        ) : null}
      </SheetHeader>

      {/* One tab per job instead of one long scroll: connect an app, look
          something up, read the logs. */}
      <Tabs value={tab} onValueChange={setPickedTab} className="min-h-0 flex-1 gap-0">
        <TabsList variant="line" className="h-9 w-full shrink-0 justify-start gap-4 border-b px-5">
          {hasConnect ? (
            <TabsTrigger value="connect" className="flex-none px-0">
              Environment
            </TabsTrigger>
          ) : null}
          <TabsTrigger value="details" className="flex-none px-0">
            Details
          </TabsTrigger>
          <TabsTrigger value="logs" className="flex-none px-0">
            Logs
          </TabsTrigger>
        </TabsList>
        {hasConnect ? (
          <TabsContent value="connect" className="min-h-0 overflow-y-auto p-5">
            <ConnectSection s={s} />
          </TabsContent>
        ) : null}
        <TabsContent value="details" className="min-h-0 overflow-y-auto p-5">
          <dl className="grid grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-x-4 gap-y-2.5 text-[13px]">
            {summary.map((row) => (
              <div key={row.label} className="contents">
                <dt className="text-muted-foreground">{row.label}</dt>
                <dd className="min-w-0 truncate font-mono text-xs">{row.value}</dd>
                <dd>{row.copy ? <CopyButton text={row.copy} label={row.label} /> : null}</dd>
              </div>
            ))}
            <div className="contents">
              <dt className="text-muted-foreground">Start with Bench</dt>
              <dd />
              <dd>
                <Switch
                  size="sm"
                  checked={s.autostart}
                  onCheckedChange={a.setAutostart}
                  disabled={a.busy !== null || a.transitional}
                  aria-label={`Start ${s.name} with Bench`}
                />
              </dd>
            </div>
          </dl>
          {/* The daemon's notice is a predicate ("listens on port ...") that
              reads after the instance's name. */}
          {s.notice ? (
            <p className="mt-4 rounded-lg bg-well p-3 text-xs text-muted-foreground">
              {s.name} {s.notice}
            </p>
          ) : null}
        </TabsContent>
        <TabsContent value="logs" className="min-h-0 overflow-y-auto p-5">
          <LogView
            endpoint={`/api/services/${encodeURIComponent(s.name)}/logs`}
            emptyText="Nothing logged yet."
          />
        </TabsContent>
      </Tabs>
      {a.dialogs}
    </>
  );
}

/** ConnectSection: how to reach the instance, as .env lines for a Laravel
 *  app, a connection URL and terminal commands (databases), and the login
 *  its web console asks for. */
function ConnectSection({ s }: { s: Service }) {
  const privacy = usePrivacy();
  const tabs = [
    ...(s.env.length > 0 ? [{ value: "env", label: ".env" }] : []),
    ...(s.url ? [{ value: "url", label: "URL" }] : []),
    ...(s.client ? [{ value: "cli", label: "CLI" }] : []),
  ];
  if (tabs.length === 0 && !s.console_login?.length) return null;

  return (
    <section className="flex flex-col gap-6">
      {tabs.length > 0 ? (
        <Tabs defaultValue={tabs[0].value}>
          {/* A picker for one option is noise: Mail only has .env. */}
          {tabs.length > 1 ? (
            <TabsList className="h-7">
              {tabs.map((t) => (
                <TabsTrigger key={t.value} value={t.value} className="px-2.5 text-xs">
                  {t.label}
                </TabsTrigger>
              ))}
            </TabsList>
          ) : null}
          <TabsContent value="env">
            <EnvBlock title="Paste into your app's .env" lines={s.env} copiedMessage={`.env lines for ${s.name} copied`} />
          </TabsContent>
          {s.url ? (
            <TabsContent value="url">
              <CopyLine label="Connection URL" text={s.url} />
            </TabsContent>
          ) : null}
          {s.client ? (
            <TabsContent value="cli" className="flex flex-col gap-4">
              {/* The client's path includes the user's home folder. */}
              <CopyLine label="Bundled client" text={s.client} shown={privacy.path(s.client)} />
              <CopyLine
                label="Bench command"
                text={`bench db ${s.name}`}
                hint="Opens TablePlus or DBeaver if installed, else the bundled client."
              />
            </TabsContent>
          ) : null}
        </Tabs>
      ) : null}
      {s.console_login?.length ? <ConsoleLogin login={s.console_login} /> : null}
    </section>
  );
}

/** CopyLine is one labelled mono value with a copy button; `shown` is what
 *  privacy mode displays instead of the text it copies. */
function CopyLine({ label, text, shown = text, hint }: { label: string; text: string; shown?: string; hint?: string }) {
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-center justify-between gap-2">
        <span className="inline-flex items-center gap-1 text-xs text-muted-foreground">
          {label}
          {hint ? <InfoTip label={`About ${label.toLowerCase()}`}>{hint}</InfoTip> : null}
        </span>
        <CopyButton text={text} label={label} />
      </div>
      <pre className="overflow-x-auto rounded-lg bg-well p-3 font-mono text-xs leading-relaxed">{shown}</pre>
    </div>
  );
}

/** ConsoleLogin lists what the web console asks for; secrets stay masked
 *  until revealed, and copy always copies the real value. */
function ConsoleLogin({ login }: { login: NonNullable<Service["console_login"]> }) {
  const [revealPicked, setReveal] = useState(false);
  const privacy = usePrivacy();
  const reveal = revealPicked && !privacy.secrets;
  return (
    <div>
      <div className="mb-2 flex items-center justify-between gap-2">
        <h3 className="text-[13px] font-medium text-muted-foreground">Console login</h3>
        {login.some((c) => c.secret) && !privacy.secrets ? (
          <Button size="xs" variant="ghost" aria-pressed={reveal} onClick={() => setReveal((r) => !r)}>
            {reveal ? <EyeSlashIcon data-icon="inline-start" /> : <EyeIcon data-icon="inline-start" />}
            {reveal ? "Hide secrets" : "Show secrets"}
          </Button>
        ) : null}
      </div>
      <dl className="grid grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-x-4 gap-y-2.5 rounded-lg bg-well p-3 text-[13px]">
        {login.map((c) => (
          <div key={c.label} className="contents">
            <dt className="text-muted-foreground">{c.label}</dt>
            <dd className="min-w-0 truncate font-mono text-xs">{c.secret && !reveal ? "•".repeat(12) : c.value}</dd>
            <dd>
              <CopyButton text={c.value} label={c.label} />
            </dd>
          </div>
        ))}
      </dl>
    </div>
  );
}
