import { useEffect, useState, type ReactNode } from "react";
import {
  DotsThreeIcon,
  DownloadSimpleIcon,
  FileCodeIcon,
  FolderOpenIcon,
  StarIcon,
  TrashIcon,
  WarningCircleIcon,
} from "@phosphor-icons/react";
import { toast } from "sonner";
import { Alert, AlertAction, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { TaskProgress } from "@/components/activity";
import { ConfirmAction } from "@/components/confirm-action";
import { InfoTip } from "@/components/info-tip";
import { PageHeader, SectionCard, StatusDot } from "@/components/page";
import { CopyButton } from "@/components/copy-button";
import { LoadFailed } from "@/components/load-failed";
import { useDaemonContext } from "@/hooks/daemon-provider";
import { useRoute } from "@/hooks/route";
import { api, formatBytes, type BinaryInfo, type Site, type Task } from "@/lib/api";
import { DEFAULT_TIMEOUT_MS, errorMessage, withTimeout } from "@/lib/async";
import { revealPath } from "@/lib/open";
import { usePrivacy } from "@/lib/privacy";

// An install runs as a daemon task the user can watch and cancel in
// Activity; this only bounds how long the request itself is awaited.
const INSTALL_TIMEOUT_MS = 60 * 60_000;
const SETTING_TIMEOUT_MS = 30_000;
// Removing is a rename plus a delete: seconds, so the dialog waits for it.
const UNINSTALL_TIMEOUT_MS = 60_000;

/** PhpPane: the PHP catalog. Installing asks first and runs as a
 *  cancellable task; picking the global default is instant and reversible;
 *  each version opens a detail sheet (ui-standard N1). */
export function PhpPane() {
  const { binaries, defaultPhp, tasks, loadError, reloadData } = useDaemonContext();
  const [openChannel, setOpenChannel] = useState<string | undefined>();
  const php = (binaries ?? []).filter((b) => b.name === "php");
  const missingDefault = php.find((b) => (b.channel === defaultPhp || b.version === defaultPhp) && !b.installed);

  return (
    <>
      <PageHeader title="Runtimes" />
      {binaries === null && loadError ? (
        <LoadFailed title="Couldn't load PHP versions" message={loadError} onRetry={reloadData} />
      ) : binaries === null ? (
        <Skeleton className="h-56 w-full rounded-xl" aria-busy />
      ) : (
        <div className="flex flex-col gap-6">
          {missingDefault && !runningInstall(tasks, missingDefault) ? (
            <Alert variant="warning">
              <WarningCircleIcon />
              <AlertTitle>PHP {missingDefault.channel} isn't installed</AlertTitle>
              <AlertDescription className="text-foreground">
                It's the default, so sites without a pinned version can't run until it is.
              </AlertDescription>
              <AlertAction>
                <InstallButton build={missingDefault} size="sm" />
              </AlertAction>
            </Alert>
          ) : null}
          <SectionCard
            title={
              <span className="inline-flex items-center gap-1">
                Versions
                <InfoTip label="About PHP versions">
                  Sites run the default unless they pin a version in their settings.
                </InfoTip>
              </span>
            }
          >
            {php.map((b) => (
              <PhpRow key={b.channel} build={b} task={runningInstall(tasks, b)} onOpen={() => setOpenChannel(b.channel)} />
            ))}
          </SectionCard>
        </div>
      )}
      <PhpSheet channel={openChannel} onClose={() => setOpenChannel(undefined)} />
    </>
  );
}

/** runningInstall finds the in-flight install task for a build, if any. */
function runningInstall(tasks: Task[] | null, b: BinaryInfo): Task | undefined {
  return tasks?.find((t) => t.kind === "binary.install" && t.target === `${b.name}/${b.channel}` && t.state === "running");
}

/** usePhpActions holds one version's actions, shared by its row and sheet:
 *  make it the default (instant, C2) and uninstall it (confirmed, C1). */
function usePhpActions(build: BinaryInfo) {
  const { conn, sites, defaultPhp, setSites, setDefaultPhp, refreshBinaries } = useDaemonContext();
  const [settingDefault, setSettingDefault] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [uninstallOpen, setUninstallOpen] = useState(false);
  const label = `PHP ${build.channel}`;
  // Older settings can name a full version ("8.4.12") for the same build.
  const isThis = (v?: string) => v === build.channel || v === build.version;
  const isDefault = isThis(defaultPhp ?? undefined);
  const users = (sites ?? []).filter((s) => s.kind !== "proxy" && isThis(s.php));
  const pinnedBy = users.filter((s) => isThis(s.php_pinned));
  const privacy = usePrivacy();

  // Why it can't be uninstalled; the daemon refuses the same cases.
  const uninstallBlocked = isDefault
    ? "It's the default. Make another version the default first."
    : pinnedBy.length > 0
      ? `${pinnedBy.map((s) => privacy.host(s.host)).join(", ")} ${pinnedBy.length === 1 ? "uses" : "use"} it. Switch ${pinnedBy.length === 1 ? "that site" : "those sites"} to another version first.`
      : null;
  // The same reason in a few words, for the row menu.
  const uninstallBlockedShort = isDefault
    ? "It's the default"
    : pinnedBy.length > 0
      ? `${pinnedBy.length} ${pinnedBy.length === 1 ? "site uses" : "sites use"} it`
      : null;

  const makeDefault = async () => {
    if (!conn || settingDefault) return;
    setSettingDefault(true);
    setError(null);
    try {
      const next = await withTimeout(
        api<Site[]>(conn, "POST", "/api/settings", { key: "php.default", value: build.channel }),
        SETTING_TIMEOUT_MS,
        "Bench didn't answer in time",
      );
      setSites(next);
      setDefaultPhp(build.channel);
      toast.success(`${label} is now the default`);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setSettingDefault(false);
    }
  };

  const uninstall = async () => {
    if (!conn) throw new Error("Bench isn't connected");
    try {
      await withTimeout(
        api(conn, "DELETE", `/api/binaries/php/${encodeURIComponent(build.channel)}`),
        UNINSTALL_TIMEOUT_MS,
        "Bench didn't answer in time",
      );
      toast.success(`${label} uninstalled`);
    } finally {
      await refreshBinaries();
    }
  };

  const dialog = (
    <ConfirmAction
      open={uninstallOpen}
      onOpenChange={setUninstallOpen}
      title={`Uninstall ${label}?`}
      description="Its files are deleted. You can install it again at any time."
      details={[
        { label: "Version", value: <span className="font-mono">{build.version}</span> },
        { label: "Deletes", value: <span className="font-mono text-xs break-all">{privacy.path(build.dir)}</span> },
        ...(build.size ? [{ label: "Frees", value: formatBytes(build.size) }] : []),
        { label: "Sites affected", value: sites === null ? "Unknown, sites haven't loaded" : "None, no site uses it" },
      ]}
      confirmLabel={`Uninstall ${label}`}
      confirmIcon={<TrashIcon data-icon="inline-start" />}
      tone="destructive"
      onConfirm={uninstall}
    />
  );

  return {
    label,
    isDefault,
    users,
    makeDefault,
    settingDefault,
    error,
    uninstallBlocked,
    uninstallBlockedShort,
    openUninstall: () => setUninstallOpen(true),
    dialog,
  };
}

type PhpActions = ReturnType<typeof usePhpActions>;

function PhpMenu({ build, a }: { build: BinaryInfo; a: PhpActions }) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button size="icon-sm" variant="ghost" aria-label={`More actions for ${a.label}`} disabled={a.settingDefault}>
          <DotsThreeIcon weight="bold" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuGroup>
          {a.isDefault ? null : (
            <DropdownMenuItem onSelect={() => void a.makeDefault()}>
              <StarIcon />
              Make default
            </DropdownMenuItem>
          )}
          <DropdownMenuItem onSelect={() => revealPath(build.dir)}>
            <FolderOpenIcon />
            Show folder
          </DropdownMenuItem>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuGroup>
          <DropdownMenuItem variant="destructive" onSelect={a.openUninstall} disabled={!!a.uninstallBlocked}>
            <TrashIcon />
            <span className="flex flex-col">
              Uninstall…
              {a.uninstallBlockedShort ? (
                <span className="text-xs text-muted-foreground">{a.uninstallBlockedShort}</span>
              ) : null}
            </span>
          </DropdownMenuItem>
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/** PhpRow is one version; clicking it opens its detail sheet. The controls
 *  inside stop the click. */
function PhpRow({ build, task, onOpen }: { build: BinaryInfo; task?: Task; onOpen: () => void }) {
  const a = usePhpActions(build);
  const stop = (e: { stopPropagation: () => void }) => e.stopPropagation();

  return (
    <div
      className="flex cursor-pointer flex-col gap-3 px-4 py-3 transition-colors duration-100 first:rounded-t-lg last:rounded-b-lg hover:bg-well"
      onClick={onOpen}
    >
      <div className="flex items-center gap-4">
        <div className="flex min-w-0 flex-1 items-center gap-2">
          <button
            type="button"
            onClick={(e) => {
              stop(e);
              onOpen();
            }}
            className="rounded-sm text-left text-sm font-medium outline-none hover:underline focus-visible:ring-3 focus-visible:ring-ring"
          >
            {a.label}
          </button>
          {/* The row names the minor version; the exact build is a detail.
              Bench's catalog carries one build per minor. */}
          <span className="inline-flex" onClick={stop}>
            <InfoTip label={`About ${a.label}`}>
              {build.installed ? `PHP ${build.version}` : `Installs PHP ${build.version}`}
              {build.installed && build.size ? `, ${formatBytes(build.size)} on disk` : ""}
            </InfoTip>
          </span>
          {a.isDefault ? (
            <Badge variant="secondary">
              <StarIcon weight="fill" data-icon="inline-start" /> Default
            </Badge>
          ) : null}
        </div>
        {task ? null : (
          <div className="flex items-center gap-3" onClick={stop}>
            {build.installed ? (
              <>
                {a.settingDefault ? (
                  <StatusDot tone="neutral" busy>
                    Making default
                  </StatusDot>
                ) : (
                  <StatusDot tone="success">Installed</StatusDot>
                )}
                <PhpMenu build={build} a={a} />
              </>
            ) : (
              <>
                <StatusDot tone="neutral">Not installed</StatusDot>
                <InstallButton build={build} />
              </>
            )}
          </div>
        )}
      </div>
      {a.error ? (
        <p role="alert" className="text-xs break-words text-destructive">
          {a.error}
        </p>
      ) : null}
      {task ? (
        <div onClick={stop}>
          <TaskProgress task={task} />
        </div>
      ) : null}
      {/* contents: the dialog renders in a portal, so this wrapper only stops
          its clicks reaching the row; as a flex item it would add a gap. */}
      <div className="contents" onClick={stop}>
        {a.dialog}
      </div>
    </div>
  );
}

/** PhpSheet is a version's detail view: what's installed where, which
 *  sites run it, and its actions. */
function PhpSheet({ channel, onClose }: { channel: string | undefined; onClose: () => void }) {
  const { binaries } = useDaemonContext();
  const build = channel ? binaries?.find((b) => b.name === "php" && b.channel === channel) : undefined;
  return (
    <Sheet open={!!build} onOpenChange={(o) => !o && onClose()}>
      <SheetContent className="gap-0 p-0">{build ? <PhpSheetBody build={build} /> : null}</SheetContent>
    </Sheet>
  );
}

function PhpSheetBody({ build }: { build: BinaryInfo }) {
  const { tasks } = useDaemonContext();
  const { navigate } = useRoute();
  const a = usePhpActions(build);
  const task = runningInstall(tasks, build);
  const info = usePhpInfo(build);
  const privacy = usePrivacy();

  const details: Array<{ label: string; value: ReactNode; copy?: string }> = [
    { label: "Version", value: build.version, copy: build.version },
    {
      label: build.installed ? "Location" : "Installs to",
      value: <span className="break-all">{privacy.path(build.dir)}</span>,
      copy: build.dir,
    },
    ...(build.installed && build.size ? [{ label: "Size on disk", value: formatBytes(build.size) }] : []),
    ...(info.data ? [{ label: "Serves sites with", value: info.data.server }] : []),
    ...(info.data
      ? [
          {
            label: "php.ini",
            value: info.data.ini ? (
              <button
                type="button"
                onClick={() => revealPath(info.data!.ini!)}
                title="Show file"
                className="rounded-sm text-left break-all outline-none hover:underline focus-visible:ring-3 focus-visible:ring-ring"
              >
                {info.data.ini}
              </button>
            ) : (
              "Compiled into this build"
            ),
            copy: info.data.ini,
          },
        ]
      : []),
    { label: "Downloads from", value: build.sources?.join(", ") || "The vendor" },
  ];

  return (
    <>
      <SheetHeader className="border-b p-5">
        <div className="flex items-center gap-3 pr-8">
          <span className="flex size-9 shrink-0 items-center justify-center rounded-md bg-well text-muted-foreground ring-1 ring-border ring-inset">
            <FileCodeIcon className="size-4.5" />
          </span>
          <div className="min-w-0">
            <SheetTitle className="flex items-center gap-2 text-base">
              {a.label}
              {a.isDefault ? (
                <Badge variant="secondary">
                  <StarIcon weight="fill" data-icon="inline-start" /> Default
                </Badge>
              ) : null}
            </SheetTitle>
            <SheetDescription className="flex items-center gap-2">
              {task ? (
                <StatusDot tone="neutral" busy>
                  Installing
                </StatusDot>
              ) : build.installed ? (
                <StatusDot tone="success">Installed</StatusDot>
              ) : (
                <StatusDot tone="neutral">Not installed</StatusDot>
              )}
            </SheetDescription>
          </div>
        </div>
        {task ? (
          <div className="mt-4">
            <TaskProgress task={task} />
          </div>
        ) : (
          <div className="mt-4 flex items-center gap-2">
            {!build.installed ? (
              <InstallButton build={build} />
            ) : (
              <>
                {a.isDefault ? null : (
                  <Button size="sm" variant="outline" onClick={() => void a.makeDefault()} disabled={a.settingDefault}>
                    {a.settingDefault ? (
                      <Spinner data-icon="inline-start" className="motion-reduce:animate-none" />
                    ) : (
                      <StarIcon data-icon="inline-start" />
                    )}
                    Make default
                  </Button>
                )}
                <Button size="sm" variant="outline" onClick={() => revealPath(build.dir)}>
                  <FolderOpenIcon data-icon="inline-start" />
                  Show folder
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  className="ml-auto"
                  onClick={a.openUninstall}
                  disabled={!!a.uninstallBlocked}
                >
                  <TrashIcon data-icon="inline-start" />
                  Uninstall…
                </Button>
              </>
            )}
          </div>
        )}
        {build.installed && a.uninstallBlocked ? (
          <p className="mt-3 text-xs text-muted-foreground">Can't uninstall: {a.uninstallBlocked}</p>
        ) : null}
        {a.error ? (
          <p role="alert" className="mt-3 text-xs break-words text-destructive">
            {a.error}
          </p>
        ) : null}
      </SheetHeader>

      <div className="flex min-h-0 flex-1 flex-col gap-6 overflow-y-auto p-5">
        <section>
          <h3 className="mb-2 text-[13px] font-medium text-muted-foreground">Details</h3>
          <dl className="grid grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-x-4 gap-y-2.5 text-[13px]">
            {details.map((row) => (
              <div key={row.label} className="contents">
                <dt className="text-muted-foreground">{row.label}</dt>
                <dd className="min-w-0 font-mono text-xs">{row.value}</dd>
                <dd>{row.copy ? <CopyButton text={row.copy} label={row.label} /> : null}</dd>
              </div>
            ))}
          </dl>
        </section>

        {build.installed ? (
          <section>
            <h3 className="mb-2 text-[13px] font-medium text-muted-foreground">
              Extensions{info.data ? ` (${info.data.extensions.length})` : ""}
            </h3>
            {info.error ? (
              <p role="alert" className="flex items-center gap-2 text-xs text-destructive">
                Couldn't list them: {info.error}
                <Button size="xs" variant="outline" onClick={info.retry}>
                  Try again
                </Button>
              </p>
            ) : info.data === null ? (
              <Skeleton className="h-16 w-full rounded-lg" aria-busy />
            ) : (
              <div className="flex flex-wrap gap-1">
                {info.data.extensions.map((e) => (
                  <Badge key={e} variant="secondary" className="font-mono text-[11px] font-normal">
                    {e}
                  </Badge>
                ))}
              </div>
            )}
          </section>
        ) : null}

        <section>
          <h3 className="mb-2 text-[13px] font-medium text-muted-foreground">Sites running it</h3>
          {a.users.length === 0 ? (
            <p className="text-[13px] text-muted-foreground">
              {a.isDefault
                ? "No sites yet. New sites run the default."
                : "None. Pin it in a site's settings, or make it the default."}
            </p>
          ) : (
            <ul className="flex flex-col divide-y rounded-lg border">
              {a.users.map((s) => (
                <li key={s.name}>
                  <button
                    type="button"
                    onClick={() => navigate({ kind: "site", name: s.name, tab: "overview" })}
                    className="flex w-full items-center justify-between gap-3 px-3 py-2 text-left text-[13px] outline-none transition-colors duration-100 hover:bg-well focus-visible:ring-3 focus-visible:ring-ring"
                  >
                    <span className="truncate font-medium">{privacy.host(s.host)}</span>
                    <span className="shrink-0 text-xs text-muted-foreground">
                      {s.php_pinned ? "Pinned" : "Default"}
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </section>
      </div>
      {a.dialog}
    </>
  );
}

/** InstallButton asks before downloading (ui-standard C1), then starts the
 *  install as a daemon task and lets Activity take over (C3, C4). */
export function InstallButton({ build, size = "sm" }: { build: BinaryInfo; size?: "sm" | "default" }) {
  const { conn, refreshBinaries, defaultPhp, setDefaultPhp, setSites } = useDaemonContext();
  const { navigate } = useRoute();
  const privacy = usePrivacy();
  const label = `PHP ${build.channel}`;

  // Runs from the finished-install toast, so its progress and outcome show
  // as a toast too (ui-standard F2).
  const makeDefault = () => {
    if (!conn) return;
    const request = withTimeout(
      api<Site[]>(conn, "POST", "/api/settings", { key: "php.default", value: build.channel }),
      SETTING_TIMEOUT_MS,
      "Bench didn't answer in time",
    ).then((sites) => {
      setSites(sites);
      setDefaultPhp(build.channel);
    });
    toast.promise(request, {
      loading: `Making ${label} the default…`,
      success: `${label} is now the default`,
      error: (e: unknown) => `Couldn't make ${label} the default: ${errorMessage(e)}`,
    });
  };

  const start = () => {
    if (!conn) throw new Error("Bench isn't connected");
    withTimeout(
      api(conn, "POST", "/api/binaries/install", { name: build.name, channel: build.channel }),
      INSTALL_TIMEOUT_MS,
      `${label} is still installing; follow it in Activity`,
    )
      .then(async () => {
        await refreshBinaries();
        // The next step: make it the default, or put it to use (C4).
        toast.success(`${label} installed`, {
          action:
            defaultPhp !== build.channel
              ? { label: "Make default", onClick: makeDefault }
              : { label: "Add site", onClick: () => navigate({ kind: "home", section: "sites" }) },
        });
      })
      .catch((e: unknown) => {
        const message = errorMessage(e);
        if (message.trim() === "cancelled") {
          toast(`${label} install cancelled`);
          return;
        }
        toast.error(`Couldn't install ${label}`, { description: message });
      });
  };

  return (
    <ConfirmAction
      trigger={
        <Button size={size} variant="outline">
          <DownloadSimpleIcon data-icon="inline-start" />
          Install
        </Button>
      }
      title={`Install ${label}?`}
      description="You can cancel the download in Activity."
      details={[
        { label: "Version", value: <span className="font-mono">{build.version}</span> },
        { label: "Downloads from", value: build.sources?.join(", ") || "the vendor" },
        { label: "Verified with", value: "The checksum published in Bench's catalog" },
        { label: "Installs to", value: <span className="font-mono text-xs">{privacy.path(build.dir)}</span> },
      ]}
      confirmLabel={`Install ${label}`}
      confirmIcon={<DownloadSimpleIcon data-icon="inline-start" />}
      onConfirm={start}
    />
  );
}

interface PhpInfo {
  server: string;
  ini?: string;
  extensions: string[];
}

/** usePhpInfo loads an installed version's server, php.ini and loaded
 *  extensions (audit UI-32); the daemon runs its `php -m`. */
function usePhpInfo(build: BinaryInfo) {
  const { conn } = useDaemonContext();
  const [data, setData] = useState<PhpInfo | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    if (!conn || !build.installed) return;
    let stale = false;
    setError(null);
    setData(null);
    withTimeout(
      api<PhpInfo>(conn, "GET", `/api/php/${encodeURIComponent(build.channel)}`),
      DEFAULT_TIMEOUT_MS,
      "Bench didn't answer in time",
    ).then(
      (d) => !stale && setData(d),
      (e: unknown) => !stale && setError(errorMessage(e)),
    );
    return () => {
      stale = true;
    };
  }, [conn, build.channel, build.installed, attempt]);
  return { data, error, retry: () => setAttempt((n) => n + 1) };
}
