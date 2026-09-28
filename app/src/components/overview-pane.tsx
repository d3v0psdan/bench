import { ArrowRightIcon, FileCodeIcon, FolderOpenIcon, StarIcon } from "@phosphor-icons/react";
import { Button } from "@/components/ui/button";
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/skeleton";
import { BenchMark } from "@/components/app-header";
import { AddSiteButton, useAddSite } from "@/components/add-site/add-site";
import { GetStarted } from "@/components/get-started";
import { HealthAlert } from "@/components/health";
import { VersionSkewAlert } from "@/components/lifecycle";
import { ListCard, PageHeader, SectionTitle, StatusDot } from "@/components/page";
import { useHealth } from "@/hooks/use-doctor";
import { SiteIcon } from "@/components/site-icon";
import { LoadFailed } from "@/components/load-failed";
import { SERVICE_LABELS, ServiceStateDot, serviceAddress } from "@/components/services/service-meta";
import { KindBadge, SitePhpLabel } from "@/components/sites/site-badges";
import { useDaemonContext } from "@/hooks/daemon-provider";
import { useRoute } from "@/hooks/route";
import { usePrivacy } from "@/lib/privacy";

const SITE_CARDS = 6;

/** HealthSummary leads the Overview with how this machine is doing
 *  (audit UI-31); the version lives in Settings > About. */
function HealthSummary() {
  const { sites, services } = useDaemonContext();
  const { issues, loading, error } = useHealth();
  const { navigate } = useRoute();
  const running = (services ?? []).filter((s) => s.state === "running").length;
  const counts = `${sites?.length ?? 0} site${sites?.length === 1 ? "" : "s"} · ${running} service${running === 1 ? "" : "s"} running`;
  if (loading) return <StatusDot tone="neutral" busy>Checking this machine</StatusDot>;
  if (error) return <StatusDot tone="warning">Checks unavailable · {counts}</StatusDot>;
  if (issues.length > 0) {
    return (
      <button
        type="button"
        onClick={() => navigate({ kind: "home", section: "settings", settings: "diagnostics" })}
        className="rounded-sm outline-none hover:underline focus-visible:ring-3 focus-visible:ring-ring"
      >
        <StatusDot tone={issues.some((c) => c.status === "fail") ? "danger" : "warning"}>
          {issues.length} {issues.length === 1 ? "issue needs" : "issues need"} attention · {counts}
        </StatusDot>
      </button>
    );
  }
  return <StatusDot tone="success">Healthy · {counts}</StatusDot>;
}

/** OverviewPane: the landing page. Sites as cards, services as a list. */
export function OverviewPane() {
  const { sites, defaultPhp, services, binaries, loadError, reloadData } = useDaemonContext();
  const { navigate } = useRoute();
  const add = useAddSite();
  const privacy = usePrivacy();

  const viewAll = (section: "sites" | "services") => (
    <Button variant="ghost" size="sm" onClick={() => navigate({ kind: "home", section })}>
      View all <ArrowRightIcon data-icon="inline-end" />
    </Button>
  );

  return (
    <>
      <PageHeader
        leading={<BenchMark className="size-10" />}
        title="This machine"
        description={<HealthSummary />}
      />

      <VersionSkewAlert className="mb-6" />
      <HealthAlert className="mb-6" />
      <GetStarted />

      <section className="mb-8">
        <SectionTitle action={sites && sites.length > SITE_CARDS ? viewAll("sites") : undefined}>Sites</SectionTitle>
        {sites === null && loadError ? (
          <LoadFailed title="Couldn't load sites" message={loadError} onRetry={reloadData} />
        ) : sites === null ? (
          <div className="grid gap-3 sm:grid-cols-2" aria-busy>
            {[0, 1].map((i) => (
              <Skeleton key={i} className="h-32 rounded-lg" />
            ))}
          </div>
        ) : sites.length === 0 ? (
          <Empty className="border border-dashed bg-card">
            <EmptyHeader>
              <EmptyTitle>No sites yet</EmptyTitle>
              <EmptyDescription>Link a project, park a folder or proxy a local port.</EmptyDescription>
            </EmptyHeader>
            <EmptyContent>
              <AddSiteButton onClick={() => add.start()} variant="outline" />
            </EmptyContent>
          </Empty>
        ) : (
          <div className="grid gap-3 sm:grid-cols-2">
            {/* Favorites first; the sort is stable, so the rest stay alphabetical. */}
            {sites
              .slice()
              .sort((a, b) => Number(!!b.favorite) - Number(!!a.favorite))
              .slice(0, SITE_CARDS)
              .map((s) => (
                <button
                  key={s.name}
                  type="button"
                  onClick={() => navigate({ kind: "site", name: s.name, tab: "overview" })}
                  className="flex flex-col gap-2.5 rounded-lg bg-card p-4 text-left shadow-card outline-none transition-[translate,box-shadow] duration-200 ease-out hover:-translate-y-0.5 hover:shadow-popover focus-visible:ring-3 focus-visible:ring-ring motion-reduce:transition-none motion-reduce:hover:translate-y-0"
                >
                  <span className="flex items-center gap-2">
                    <SiteIcon name={s.name} />
                    <span className="truncate text-sm font-medium">{privacy.host(s.host)}</span>
                    <KindBadge kind={s.kind} />
                    {s.favorite ? (
                      <StarIcon
                        weight="fill"
                        role="img"
                        aria-label="Favorite"
                        className="size-3.5 shrink-0 text-warning-solid"
                      />
                    ) : null}
                  </span>
                  <span className="flex flex-col gap-1.5 text-[13px] text-muted-foreground">
                    <span className="flex min-w-0 items-center gap-2">
                      <FolderOpenIcon className="size-3.5 shrink-0" />
                      <span className="truncate font-mono text-xs">
                        {s.kind === "proxy" ? `→ ${s.proxy_to}` : s.path && privacy.path(s.path)}
                      </span>
                    </span>
                    {s.kind !== "proxy" ? (
                      <span className="flex items-center gap-2">
                        <FileCodeIcon className="size-3.5 shrink-0" />
                        <SitePhpLabel site={s} defaultPhp={defaultPhp} binaries={binaries} />
                      </span>
                    ) : null}
                  </span>
                </button>
              ))}
          </div>
        )}
      </section>

      <section>
        <SectionTitle action={services && services.length > 0 ? viewAll("services") : undefined}>Services</SectionTitle>
        {services === null && loadError ? (
          <LoadFailed message={loadError} onRetry={reloadData} />
        ) : services === null ? (
          <Skeleton className="h-24 rounded-lg" aria-busy />
        ) : services.length === 0 ? (
          <Empty className="border border-dashed bg-card">
            <EmptyHeader>
              <EmptyTitle>No services yet</EmptyTitle>
              <EmptyDescription>Add a database, cache or search service.</EmptyDescription>
            </EmptyHeader>
            <EmptyContent>
              <Button variant="outline" onClick={() => navigate({ kind: "home", section: "services", intent: "new-service" })}>
                New service
              </Button>
            </EmptyContent>
          </Empty>
        ) : (
          <ListCard>
            {services.map((s) => (
              <button
                key={s.name}
                type="button"
                onClick={() =>
                  navigate({ kind: "home", section: "services", service: s.name, serviceLogs: s.state === "failed" })
                }
                className="flex w-full items-center gap-4 px-4 py-2.5 text-left text-[13px] outline-none transition-colors duration-100 hover:bg-well focus-visible:bg-well focus-visible:ring-3 focus-visible:ring-ring focus-visible:ring-inset"
              >
                <div className="w-24 shrink-0">
                  <ServiceStateDot state={s.state} />
                </div>
                <span className="w-32 truncate font-mono font-medium">{s.name}</span>
                <span className="min-w-0 flex-1 truncate text-muted-foreground">
                  {SERVICE_LABELS[s.service] ?? s.service} {s.version || s.channel}
                </span>
                <span className="font-mono text-muted-foreground tabular-nums">{serviceAddress(s)}</span>
              </button>
            ))}
          </ListCard>
        )}
      </section>
      {add.dialogs}
    </>
  );
}
