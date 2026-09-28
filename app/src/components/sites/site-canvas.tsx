import { useEffect, useState, type ReactNode } from "react";
import {
  ArrowsLeftRightIcon,
  CaretRightIcon,
  CpuIcon,
  DownloadSimpleIcon,
  EnvelopeSimpleIcon,
  FileCodeIcon,
  GlobeIcon,
  type Icon,
  PlugIcon,
  PlusIcon,
  PushPinIcon,
  LockSimpleIcon,
  TrayIcon,
} from "@phosphor-icons/react";
import { Skeleton } from "@/components/ui/skeleton";
import { CheckAction, CheckSteps } from "@/components/health";
import { InstallButton } from "@/components/php-pane";
import { CHECK_LABELS, CHECK_TONES, StatusDot } from "@/components/page";
import { CreateServiceDialog } from "@/components/service-dialogs";
import { LoadFailed } from "@/components/load-failed";
import { SERVICE_ICONS, SERVICE_LABELS, ServiceStateDot } from "@/components/services/service-meta";
import { useDaemonContext } from "@/hooks/daemon-provider";
import { useDoctor } from "@/hooks/use-doctor";
import { useRoute } from "@/hooks/route";
import { api, type Check, type Service, type Site } from "@/lib/api";
import { cn } from "@/lib/utils";
import { withTimeout } from "@/lib/async";
import { sitePhp } from "@/lib/php";
import { MASK, usePrivacy } from "@/lib/privacy";

const MAIL_COUNT_TIMEOUT_MS = 10_000;

/** SiteCanvas: the request path drawn as a canvas, network into runtime
 *  into the shared services. */
export function SiteCanvas({ site }: { site: Site }) {
  const { services, defaultPhp, binaries, status, loadError, reloadData } = useDaemonContext();
  const doctor = useDoctor();
  const { navigate } = useRoute();
  const [createOpen, setCreateOpen] = useState(false);
  const privacy = usePrivacy();
  const php = sitePhp(site, defaultPhp, binaries);
  const loading = doctor.checks === null && !doctor.error;
  const envPorts = site.env_ports ?? [];
  const used = (services ?? []).filter((s) => envPorts.includes(s.port));
  const others = (services ?? []).filter((s) => !envPorts.includes(s.port));
  const toHttps = () => navigate({ kind: "home", section: "settings", settings: "https" });

  return (
    <div className="@container rounded-xl border bg-dot-grid p-6">
      <div className="flex w-full flex-col items-stretch gap-3 @3xl:flex-row @3xl:items-start @3xl:gap-0">
        <CanvasGroup label="Network">
          <ResourceCard icon={LockSimpleIcon} title="HTTPS" onOpen={toHttps}>
            <ResourceRow icon={PlugIcon} label="Port 443">
              <CheckDot check={doctor.byId("https")} loading={loading} />
            </ResourceRow>
            <ResourceRow icon={LockSimpleIcon} label="Certificates">
              <CheckDot check={doctor.byId("trust")} loading={loading} />
            </ResourceRow>
            <CheckReason checks={[doctor.byId("https"), doctor.byId("trust")]} />
          </ResourceCard>
          <ResourceCard icon={GlobeIcon} title="Domain" onOpen={toHttps}>
            <ResourceRow icon={GlobeIcon} label="*.test DNS">
              <CheckDot check={doctor.byId("dns")} loading={loading} />
            </ResourceRow>
            <p className="truncate pl-5.5 text-[13px]">{privacy.host(site.host)}</p>
            <CheckReason checks={[doctor.byId("dns")]} />
          </ResourceCard>
        </CanvasGroup>

        <Connector />

        {site.kind === "proxy" ? (
          <CanvasGroup label="Proxy">
            <ResourceCard icon={ArrowsLeftRightIcon} title="Reverse proxy">
              <ResourceRow icon={ArrowsLeftRightIcon} label="Target">
                <span className="truncate text-[13px]">{site.proxy_to}</span>
              </ResourceRow>
            </ResourceCard>
          </CanvasGroup>
        ) : (
          <CanvasGroup label="Runtime">
            <ResourceCard
              icon={FileCodeIcon}
              title={php.channel ? `PHP ${php.channel}` : "PHP"}
              onOpen={() => navigate({ kind: "site", name: site.name, tab: "settings" })}
            >
              <ResourceRow icon={PushPinIcon} label="Version">
                <span className="text-[13px]">{site.php_pinned ? "Pinned" : "Default"}</span>
              </ResourceRow>
              <ResourceRow icon={DownloadSimpleIcon} label="Installed">
                {php.installed === null ? (
                  <StatusDot tone="neutral" busy>
                    Checking
                  </StatusDot>
                ) : php.installed ? (
                  <StatusDot tone="success">Yes</StatusDot>
                ) : (
                  <StatusDot tone="danger">No</StatusDot>
                )}
              </ResourceRow>
              <ResourceRow icon={CpuIcon} label="Server">
                <span className="text-[13px]">{status?.os === "windows" ? "php-cgi" : "php-fpm"}</span>
              </ResourceRow>
            </ResourceCard>
            {php.installed === false && php.build ? (
              <div className="rounded-lg bg-card p-3 shadow-card">
                <p className="mb-2 text-[13px] text-destructive">
                  PHP {php.channel} isn't installed, so this site can't run.
                </p>
                <InstallButton build={php.build} />
              </div>
            ) : null}
          </CanvasGroup>
        )}

        <Connector />

        <CanvasGroup label="Services">
          <MailCard site={site} />
          {services === null && loadError ? (
            <LoadFailed title="Couldn't load services" message={loadError} onRetry={reloadData} />
          ) : services === null ? (
            <Skeleton className="h-24 w-full rounded-lg" />
          ) : services.length > 0 ? (
            <>
              {/* Services the app's .env points at come first; the rest are
                  on this machine but unused by it (audit UI-11). */}
              {used.length > 0 ? (
                <ResourceCard icon={PlugIcon} title="Used by this app">
                  {used.map((s) => (
                    <CanvasServiceRow key={s.name} service={s} />
                  ))}
                </ResourceCard>
              ) : null}
              {others.length > 0 ? (
                <ResourceCard icon={PlugIcon} title={used.length > 0 ? "Other services" : "Services on this machine"}>
                  {used.length === 0 ? (
                    <p className="text-xs text-muted-foreground">
                      None are in this app's .env yet.
                    </p>
                  ) : null}
                  {others.map((s) => (
                    <CanvasServiceRow key={s.name} service={s} muted />
                  ))}
                </ResourceCard>
              ) : null}
            </>
          ) : null}
          <button
            type="button"
            onClick={() => setCreateOpen(true)}
            className="flex w-full items-start gap-2.5 rounded-lg bg-card px-3 py-2.5 text-left shadow-card outline-none transition-colors duration-150 ease-out hover:bg-well focus-visible:ring-3 focus-visible:ring-ring"
          >
            <span className="mt-1.5 size-1.5 shrink-0 rounded-full bg-brand" aria-hidden />
            <span>
              <span className="block text-sm font-medium">New service</span>
              <span className="block text-[13px] text-muted-foreground">Database, cache, search, storage</span>
            </span>
            <PlusIcon className="mt-0.5 ml-auto size-4 text-muted-foreground" />
          </button>
        </CanvasGroup>
      </div>
      <CreateServiceDialog open={createOpen} onOpenChange={setCreateOpen} />
    </div>
  );
}

/** MailCard is the site's inbox on the canvas (audit UI-21): how much mail
 *  its app has sent, opening Mail filtered to it. Apps file mail under
 *  their APP_NAME. */
function MailCard({ site }: { site: Site }) {
  const { conn, services, mailVersion } = useDaemonContext();
  const { navigate } = useRoute();
  const mail = services?.find((s) => s.service === "mailpit");
  const inbox = site.app_name;
  const privacy = usePrivacy();
  const [count, setCount] = useState<number | null>(null);
  const [countError, setCountError] = useState(false);

  useEffect(() => {
    if (!conn || !inbox || mail?.state !== "running") return;
    setCountError(false);
    let stale = false;
    const query = encodeURIComponent(`tag:"${inbox}"`);
    withTimeout(
      api<{ messages_count: number }>(conn, "GET", `/api/mail/v1/search?limit=1&query=${query}`),
      MAIL_COUNT_TIMEOUT_MS,
      "Mail didn't answer in time",
    )
      .then((r) => !stale && setCount(r.messages_count))
      .catch(() => !stale && setCountError(true)); // the card still opens Mail
    return () => {
      stale = true;
    };
  }, [conn, inbox, mail?.state, mailVersion]);

  if (!mail || site.kind === "proxy") return null;
  const running = mail.state === "running";
  return (
    <ResourceCard
      icon={EnvelopeSimpleIcon}
      title="Mail"
      onOpen={() => navigate({ kind: "home", section: "mail", inbox: running ? inbox : undefined })}
    >
      <ResourceRow icon={TrayIcon} label={inbox ? `Inbox ${privacy.names ? MASK : inbox}` : "Inbox"}>
        {!running ? (
          <StatusDot tone="neutral">Stopped</StatusDot>
        ) : !inbox ? null : countError ? (
          <span className="text-[13px] text-muted-foreground">Couldn't count</span>
        ) : count === null ? (
          <span className="text-[13px] text-muted-foreground">…</span>
        ) : (
          <span className="text-[13px] tabular-nums">
            {count} message{count === 1 ? "" : "s"}
          </span>
        )}
      </ResourceRow>
      {!inbox ? (
        <p className="text-xs text-muted-foreground">Set APP_NAME in .env to give it an inbox.</p>
      ) : null}
    </ResourceCard>
  );
}

/** CanvasServiceRow opens a service's detail (Mail has its own page). */
function CanvasServiceRow({ service: s, muted = false }: { service: Service; muted?: boolean }) {
  const { navigate } = useRoute();
  const ServiceIcon = SERVICE_ICONS[s.service] ?? PlugIcon;
  return (
    <button
      type="button"
      onClick={() =>
        navigate(
          s.service === "mailpit"
            ? { kind: "home", section: "mail" }
            : { kind: "home", section: "services", service: s.name, serviceLogs: s.state === "failed" },
        )
      }
      className={cn(
        "-mx-1.5 rounded-md px-1.5 py-0.5 text-left outline-none transition-colors duration-100 hover:bg-subtle focus-visible:ring-3 focus-visible:ring-ring",
        muted && "opacity-70 hover:opacity-100",
      )}
    >
      <ResourceRow icon={ServiceIcon} label={s.name} hint={SERVICE_LABELS[s.service]}>
        <ServiceStateDot state={s.state} />
      </ResourceRow>
    </button>
  );
}

/** CheckReason shows, as text, why the card's checks aren't OK (ui-standard
 *  A4: the reason must not live only in a tooltip). */
function CheckReason({ checks }: { checks: Array<Check | undefined> }) {
  const failing = checks.filter((c): c is Check => !!c && c.status !== "ok");
  const privacy = usePrivacy();
  if (failing.length === 0) return null;
  return (
    <div className="flex flex-col gap-2 border-t pt-2 text-xs text-muted-foreground">
      {failing.map((c) => (
        <div key={c.id} className="flex flex-col gap-1.5">
          <p className="break-words">{privacy.text(c.detail)}</p>
          {/* The fix sits where the problem shows (audit UI-11). */}
          <CheckSteps steps={c.steps} />
          <div>
            <CheckAction check={c} />
          </div>
        </div>
      ))}
    </div>
  );
}

function CanvasGroup({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="min-w-64 flex-1 rounded-xl bg-subtle/70 p-2 @3xl:max-w-sm">
      <p className="px-1.5 pt-0.5 pb-2 text-[13px] text-muted-foreground">{label}</p>
      <div className="flex flex-col gap-2">{children}</div>
    </div>
  );
}

function Connector() {
  return <div aria-hidden className="mt-14 hidden h-px min-w-8 flex-1 bg-input @3xl:block" />;
}

function ResourceCard({
  icon: Icon,
  title,
  onOpen,
  children,
}: {
  icon: Icon;
  title: string;
  /** Makes the card open its resource (ui-standard N1). */
  onOpen?: () => void;
  children: ReactNode;
}) {
  const header = (
    <div className="flex items-center gap-2 border-b px-3 py-2.5 text-sm font-medium">
      <Icon className="size-4 shrink-0" />
      <span className="truncate">{title}</span>
      {onOpen ? <CaretRightIcon className="ml-auto size-3.5 shrink-0 text-muted-foreground" /> : null}
    </div>
  );
  return (
    <div className="rounded-lg bg-card shadow-card">
      {onOpen ? (
        <button
          type="button"
          onClick={onOpen}
          aria-label={`Open ${title}`}
          className="block w-full rounded-t-lg text-left outline-none transition-colors duration-100 hover:bg-well focus-visible:ring-3 focus-visible:ring-ring"
        >
          {header}
        </button>
      ) : (
        header
      )}
      <div className="flex flex-col gap-2 px-3 py-2.5">{children}</div>
    </div>
  );
}

function ResourceRow({
  icon: Icon,
  label,
  hint,
  children,
}: {
  icon: Icon;
  label: string;
  hint?: string;
  children: ReactNode;
}) {
  return (
    <div className="flex items-center justify-between gap-3">
      <span className="flex min-w-0 items-center gap-2 text-[13px] text-muted-foreground">
        <Icon className="size-3.5 shrink-0" />
        <span className="truncate" title={hint}>
          {label}
        </span>
      </span>
      {children}
    </div>
  );
}

/** CheckDot shows one doctor check in the shared vocabulary; CheckReason
 *  under the card carries the detail as text. */
function CheckDot({ check, loading }: { check?: Check; loading: boolean }) {
  if (loading) {
    return (
      <StatusDot tone="neutral" busy>
        Checking
      </StatusDot>
    );
  }
  if (!check) return <StatusDot tone="neutral">Unknown</StatusDot>;
  return <StatusDot tone={CHECK_TONES[check.status]}>{CHECK_LABELS[check.status]}</StatusDot>;
}
