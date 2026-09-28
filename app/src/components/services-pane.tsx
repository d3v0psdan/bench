import { useEffect, useState } from "react";
import { CubeIcon, DatabaseIcon, EnvelopeSimpleIcon, PlusIcon, ScrollIcon } from "@phosphor-icons/react";
import { Button } from "@/components/ui/button";
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Field, FieldLabel } from "@/components/ui/field";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { TaskProgress } from "@/components/activity";
import { LoadFailed } from "@/components/load-failed";
import { PageHeader, SectionCard, SectionRow } from "@/components/page";
import { CreateServiceDialog } from "@/components/service-dialogs";
import { ServiceMenu, StartStopButton, useServiceActions } from "@/components/services/service-actions";
import { SERVICE_ICONS, SERVICE_LABELS, ServiceStateDot, serviceAddress } from "@/components/services/service-meta";
import { ServiceSheet } from "@/components/services/service-sheet";
import { useDaemonContext } from "@/hooks/daemon-provider";
import { useRoute } from "@/hooks/route";
import type { Service } from "@/lib/api";

const CATEGORIES: Array<{ title: string; description?: string; kinds: string[] }> = [
  { title: "Databases", kinds: ["mysql", "mariadb", "postgresql"] },
  { title: "Cache", kinds: ["valkey"] },
  { title: "Search", kinds: ["meilisearch"] },
  { title: "Object storage", kinds: ["rustfs"] },
];
// Mail has one home, the Mail page (audit UI-21); here it only links there.
const KNOWN_KINDS = new Set([...CATEGORIES.flatMap((c) => c.kinds), "mailpit"]);

/** ServicesPane: managed service instances with live state, start/stop,
 *  autostart, clone, delete, and the .env block that connects an app. */
export function ServicesPane() {
  const { conn, services, loadError, reloadData } = useDaemonContext();
  const { route, navigate } = useRoute();
  // The open detail sheet is part of the route, so Overview, the site
  // canvas and the palette can link straight to it.
  const openName = route.kind === "home" ? route.service : undefined;
  const showLogs = route.kind === "home" && !!route.serviceLogs;
  const openService = (service?: string, serviceLogs?: boolean) =>
    navigate({ kind: "home", section: "services", service, serviceLogs });
  const [createOpen, setCreateOpen] = useState(false);
  // "New service…" from the palette arrives as an intent.
  const newServiceIntent = route.kind === "home" && route.intent === "new-service";
  useEffect(() => {
    if (!newServiceIntent) return;
    setCreateOpen(true);
    navigate({ kind: "home", section: "services" });
  }, [newServiceIntent, navigate]);

  const createButton = (
    <Button variant="brand" onClick={() => setCreateOpen(true)} disabled={!conn}>
      <PlusIcon data-icon="inline-start" />
      New service
    </Button>
  );

  const body = (() => {
    if (services === null && loadError) {
      return <LoadFailed message={loadError} onRetry={reloadData} />;
    }
    if (services === null) {
      return (
        <div className="flex flex-col gap-6" aria-busy>
          {[0, 1].map((i) => (
            <Skeleton key={i} className="h-36 w-full rounded-xl" />
          ))}
        </div>
      );
    }
    if (services.length === 0) {
      return (
        <Empty className="border border-dashed bg-card">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <DatabaseIcon />
            </EmptyMedia>
            <EmptyTitle>No services yet</EmptyTitle>
            <EmptyDescription>Create a database, cache, search or S3 service.</EmptyDescription>
          </EmptyHeader>
          <EmptyContent>{createButton}</EmptyContent>
        </Empty>
      );
    }
    const groups = [
      ...CATEGORIES,
      { title: "Other", description: "Unknown to this version of the app.", kinds: [] },
    ]
      .map((c) => ({
        ...c,
        rows: services.filter((s) => (c.kinds.length ? c.kinds.includes(s.service) : !KNOWN_KINDS.has(s.service))),
      }))
      .filter((c) => c.rows.length > 0);
    const mail = services.find((s) => s.service === "mailpit");
    return (
      <div className="flex flex-col gap-6">
        {groups.map((g) => (
          <SectionCard key={g.title} title={g.title} description={g.description}>
            {g.rows.map((s) => (
              <ServiceRow
                key={s.name}
                service={s}
                onOpen={() => openService(s.name)}
                onViewLogs={() => openService(s.name, true)}
              />
            ))}
          </SectionCard>
        ))}
        {mail ? <MailLinkCard mail={mail} /> : null}
      </div>
    );
  })();

  return (
    <>
      <PageHeader
        title="Services"
        actions={services !== null && services.length > 0 ? createButton : undefined}
      />
      {body}
      <CreateServiceDialog open={createOpen} onOpenChange={setCreateOpen} />
      <ServiceSheet name={openName} showLogs={showLogs} onClose={() => openService(undefined)} />
    </>
  );
}

/** MailLinkCard stands in for the mail catcher, which Mail manages. */
function MailLinkCard({ mail }: { mail: Service }) {
  const { navigate } = useRoute();
  return (
    <SectionCard title="Mail">
      <SectionRow
        label={
          <span className="flex items-center gap-3">
            {mail.name}
            <ServiceStateDot state={mail.state} />
          </span>
        }
        description={`SMTP ${serviceAddress(mail)}`}
      >
        <Button variant="outline" size="sm" onClick={() => navigate({ kind: "home", section: "mail" })}>
          <EnvelopeSimpleIcon data-icon="inline-start" />
          Open Mail
        </Button>
      </SectionRow>
    </SectionCard>
  );
}

/** ServiceRow is one instance in the list; clicking it opens its detail
 *  sheet (ui-standard N1). The controls inside stop the click. */
function ServiceRow({
  service: s,
  onOpen,
  onViewLogs,
}: {
  service: Service;
  onOpen: () => void;
  onViewLogs: () => void;
}) {
  const a = useServiceActions(s);
  const { tasks } = useDaemonContext();
  // A create or clone in flight shows its progress on the row (UI-12).
  const task = tasks?.find(
    (t) => t.state === "running" && t.target === s.name && (t.kind === "service.create" || t.kind === "service.clone"),
  );
  const ServiceIcon = SERVICE_ICONS[s.service] ?? CubeIcon;
  const extras = Object.entries(s.ports ?? {})
    .map(([role, port]) => `${role} :${port}`)
    .join(" · ");
  const stop = (e: { stopPropagation: () => void }) => e.stopPropagation();

  return (
    <div
      className="flex cursor-pointer flex-col gap-1.5 px-4 py-3 transition-colors duration-100 first:rounded-t-lg last:rounded-b-lg hover:bg-well"
      onClick={onOpen}
    >
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
        <span className="flex size-8 shrink-0 items-center justify-center rounded-md bg-well text-muted-foreground ring-1 ring-border ring-inset">
          <ServiceIcon className="size-4" />
        </span>
        <div className="min-w-0 flex-1 basis-40">
          <button
            type="button"
            onClick={(e) => {
              stop(e);
              onOpen();
            }}
            className="truncate rounded-sm text-left text-sm font-medium outline-none hover:underline focus-visible:ring-3 focus-visible:ring-ring"
          >
            {s.name}
          </button>
          <p className="truncate font-mono text-xs text-muted-foreground tabular-nums">
            {SERVICE_LABELS[s.service] ?? s.service} {s.version || s.channel} · {serviceAddress(s)}
            {extras ? ` · ${extras}` : ""}
          </p>
        </div>

        <div className="w-24">
          <ServiceStateDot state={s.state} />
        </div>

        <Field orientation="horizontal" className="w-auto shrink-0" onClick={stop}>
          <Switch
            id={`autostart-${s.name}`}
            size="sm"
            checked={s.autostart}
            onCheckedChange={a.setAutostart}
            disabled={a.busy !== null || a.transitional}
          />
          <FieldLabel htmlFor={`autostart-${s.name}`} className="text-[13px] font-normal text-muted-foreground">
            Start with Bench
          </FieldLabel>
        </Field>

        <div className="flex items-center gap-1" onClick={stop}>
          <StartStopButton s={s} a={a} />
          <ServiceMenu s={s} a={a} />
        </div>
      </div>

      {task ? (
        <div className="pl-12" onClick={stop}>
          <TaskProgress task={task} />
        </div>
      ) : null}
      {s.notice ? <p className="pl-12 text-xs text-muted-foreground">{s.notice}</p> : null}
      {a.error || s.error ? (
        <div className="flex items-center gap-3 pl-12">
          <p
            role={a.error ? "alert" : "status"}
            className="min-w-0 flex-1 text-xs break-words whitespace-pre-wrap text-destructive"
          >
            {a.error ?? s.error}
          </p>
          <Button
            size="xs"
            variant="outline"
            className="shrink-0"
            onClick={(e) => {
              stop(e);
              onViewLogs();
            }}
          >
            <ScrollIcon data-icon="inline-start" />
            View logs
          </Button>
        </div>
      ) : null}
      {/* contents: the dialogs render in a portal, so this wrapper only stops
          their clicks reaching the row; as a flex item it would add a gap. */}
      <div className="contents" onClick={stop}>
        {a.dialogs}
      </div>
    </div>
  );
}
