import { useEffect, useState } from "react";
import { getVersion } from "@tauri-apps/api/app";
import {
  ArrowSquareOutIcon,
  ArrowsClockwiseIcon,
  BookOpenIcon,
  BugIcon,
  ClipboardTextIcon,
  CopyIcon,
  ScalesIcon,
  WarningIcon,
} from "@phosphor-icons/react";
import { toast } from "sonner";
import { Alert, AlertAction, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { Switch } from "@/components/ui/switch";
import { ConfirmAction } from "@/components/confirm-action";
import { InfoTip } from "@/components/info-tip";
import { SectionCard, SectionRow } from "@/components/page";
import { useDaemonContext } from "@/hooks/daemon-provider";
import { api, type Conn } from "@/lib/api";
import { DEFAULT_TIMEOUT_MS, errorMessage, withTimeout } from "@/lib/async";
import { LINKS } from "@/lib/links";
import { openExternal } from "@/lib/open";
import { notifyEnabled, setNotifyEnabled, type NotifyKind } from "@/lib/prefs";
import { usePrivacy } from "@/lib/privacy";
import { sameRelease } from "@/lib/versions";

const REPORT_LOG_LINES = 40;

/** useAppVersion is this desktop app's version ("…" until known). */
export function useAppVersion() {
  const [version, setVersion] = useState<string | null>(null);
  useEffect(() => {
    withTimeout(getVersion(), DEFAULT_TIMEOUT_MS, "The app version didn't load in time").then(setVersion, () =>
      setVersion("unknown"),
    );
  }, []);
  return version;
}

/** VersionSkewAlert warns when the app and the running Bench are different
 *  releases (audit UI-38): usually an update replaced the app while the old
 *  Bench kept running. Restarting Bench stops every site for a moment, so
 *  it is confirmed. */
export function VersionSkewAlert({ className }: { className?: string }) {
  const { status, connected, pending, start, stop } = useDaemonContext();
  const app = useAppVersion();
  const [restarting, setRestarting] = useState(false);

  // Restart = stop, then start once the old Bench is gone. A stop that
  // settles with Bench still connected failed (its error shows in the
  // shell), so the spinner ends there too.
  useEffect(() => {
    if (!restarting || pending !== null) return;
    setRestarting(false);
    if (!connected) start();
  }, [restarting, connected, pending, start]);

  if (!app || app === "unknown" || !status || sameRelease(app, status.version)) return null;
  return (
    <Alert variant="warning" className={className}>
      <WarningIcon />
      <AlertTitle>This app and Bench are different versions</AlertTitle>
      <AlertDescription>
        The app is {app}, Bench is {status.version}. Restart Bench so both match.
      </AlertDescription>
      <AlertAction>
        <ConfirmAction
          trigger={
            <Button size="sm" variant="outline" disabled={pending !== null || restarting}>
              {restarting ? <Spinner data-icon="inline-start" className="motion-reduce:animate-none" /> : null}
              Restart Bench…
            </Button>
          }
          title="Restart Bench?"
          description="Bench stops and starts again with the version that matches this app."
          details={[{ label: "Stops", value: "Every site and service, for a few seconds" }]}
          confirmLabel="Restart Bench"
          onConfirm={() => {
            setRestarting(true);
            stop();
          }}
        />
      </AlertAction>
    </Alert>
  );
}

interface UpdateInfo {
  enabled: boolean;
  current: string;
  latest?: string;
  url?: string;
  newer: boolean;
  checked_at?: string;
  error?: string;
}

/** UpdatesCard: the daily update check (on by default, one GitHub request a
 *  day, PLAN.md §4) with its switch and "Check now". Nothing installs by
 *  itself: a newer release links to its page. */
export function UpdatesCard() {
  const { conn } = useDaemonContext();
  const [info, setInfo] = useState<UpdateInfo | null>(null);
  const [checking, setChecking] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = async (force: boolean) => {
    if (!conn) return;
    setChecking(true);
    setError(null);
    try {
      setInfo(
        await withTimeout(
          api<UpdateInfo>(conn, "GET", `/api/updates${force ? "?force=1" : ""}`),
          DEFAULT_TIMEOUT_MS,
          "Checking took too long",
        ),
      );
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setChecking(false);
    }
  };
  // load is recreated each render and only reads conn, so conn is the key.
  useEffect(() => void load(false), [conn]);

  const setEnabled = async (on: boolean) => {
    if (!conn) return;
    try {
      await withTimeout(
        api(conn, "POST", "/api/settings", { key: "updates.check", value: on ? "on" : "off" }),
        DEFAULT_TIMEOUT_MS,
        "Saving took too long",
      );
      setInfo((i) => (i ? { ...i, enabled: on } : i));
    } catch (e) {
      setError(errorMessage(e));
    }
  };

  const status = error ?? info?.error;
  const summary = checking
    ? "Checking…"
    : status
      ? `Couldn't check: ${status}`
      : info?.newer
        ? `Bench ${info.latest} is available.`
        : info?.checked_at
          ? `Up to date. Checked ${new Date(info.checked_at).toLocaleString()}.`
          : "Not checked yet.";

  return (
    <SectionCard title="Updates">
      <SectionRow
        label="Latest version"
        description={
          <span role={status ? "alert" : undefined} className={status ? "text-destructive" : undefined}>
            {summary}
          </span>
        }
      >
        {info?.newer && info.url ? (
          <Button size="sm" variant="outline" onClick={() => openExternal(info.url!)}>
            <ArrowSquareOutIcon data-icon="inline-start" />
            View release
          </Button>
        ) : null}
        <Button size="sm" variant="outline" onClick={() => void load(true)} disabled={checking || !conn}>
          {checking ? (
            <Spinner data-icon="inline-start" className="motion-reduce:animate-none" />
          ) : (
            <ArrowsClockwiseIcon data-icon="inline-start" />
          )}
          Check now
        </Button>
      </SectionRow>
      <SectionRow
        label={
          <span className="inline-flex items-center gap-1">
            Check automatically
            <InfoTip label="About update checks">
              Bench asks GitHub once a day. When off, it never contacts GitHub on its own.
            </InfoTip>
          </span>
        }
      >
        <Switch
          checked={info?.enabled ?? true}
          onCheckedChange={(on) => void setEnabled(on)}
          disabled={info === null}
          aria-label="Check for updates automatically"
        />
      </SectionRow>
    </SectionCard>
  );
}

/** HelpCard: where to read more and where to report a problem (audit UI-39). */
export function HelpCard() {
  const links = [
    { label: "Documentation", url: LINKS.docs, icon: BookOpenIcon },
    {
      label: "Report an issue",
      description: "Attach a report from Diagnostics.",
      url: LINKS.issues,
      icon: BugIcon,
    },
    { label: "License", description: "MIT", url: LINKS.license, icon: ScalesIcon },
  ];
  return (
    <SectionCard title="Help">
      {links.map((l) => (
        <SectionRow key={l.label} label={l.label} description={l.description}>
          <Button size="sm" variant="outline" onClick={() => openExternal(l.url)}>
            <l.icon data-icon="inline-start" />
            Open
          </Button>
        </SectionRow>
      ))}
    </SectionCard>
  );
}

/** buildReport gathers what a bug report needs: versions, platform, every
 *  check, service states and the end of Bench's log. No .env values or
 *  tokens: services contribute their state only. */
async function buildReport(conn: Conn, app: string | null) {
  const get = <T,>(path: string) =>
    withTimeout(api<T>(conn, "GET", path), DEFAULT_TIMEOUT_MS, "Gathering the report took too long");
  const [status, checks, services, log] = await Promise.all([
    get<{ version: string; os: string; arch: string }>("/api/status"),
    get<Array<{ id: string; status: string; title: string; detail: string }>>("/api/doctor"),
    get<Array<{ name: string; service: string; version: string; state: string; error?: string }>>("/api/services"),
    get<{ text: string }>(`/api/daemon/log?tail=${REPORT_LOG_LINES}`),
  ]);
  return [
    `Bench app ${app ?? "unknown"}, daemon ${status.version}, ${status.os}/${status.arch}`,
    "",
    "Checks:",
    ...checks.map((c) => `- [${c.status}] ${c.title}: ${c.detail}`),
    "",
    "Services:",
    ...services.map((s) => `- ${s.name} (${s.service} ${s.version}): ${s.state}${s.error ? `, ${s.error}` : ""}`),
    "",
    `Last ${REPORT_LOG_LINES} lines of Bench's log:`,
    log.text.trimEnd(),
  ].join("\n");
}

/** CopyReportButton copies a diagnostics report for an issue (UI-39). */
export function CopyReportButton() {
  const { conn } = useDaemonContext();
  const app = useAppVersion();
  const [busy, setBusy] = useState(false);
  const copy = async () => {
    if (!conn || busy) return;
    setBusy(true);
    try {
      const report = await buildReport(conn, app);
      await navigator.clipboard.writeText(report);
      toast.success("Report copied", {
        action: { label: "Report an issue", onClick: () => openExternal(LINKS.issues) },
      });
    } catch (e) {
      toast.error("Couldn't copy the report", { description: errorMessage(e) });
    } finally {
      setBusy(false);
    }
  };
  return (
    <Button variant="outline" size="sm" onClick={() => void copy()} disabled={busy || !conn}>
      {busy ? (
        <Spinner data-icon="inline-start" className="motion-reduce:animate-none" />
      ) : (
        <ClipboardTextIcon data-icon="inline-start" />
      )}
      Copy report
    </Button>
  );
}

const NOTIFY_ROWS: Array<{ kind: NotifyKind; label: string }> = [
  { kind: "crash", label: "A service stops working" },
  { kind: "tasks", label: "A long task finishes" },
  { kind: "mail", label: "New mail arrives" },
];

/** NotificationsCard: which background events raise an OS notification
 *  (UI-40). They only appear while Bench's window isn't in front. */
export function NotificationsCard() {
  const [on, setOn] = useState(() => Object.fromEntries(NOTIFY_ROWS.map((r) => [r.kind, notifyEnabled(r.kind)])));
  return (
    <SectionCard
      title={
        <span className="inline-flex items-center gap-1">
          Notifications
          <InfoTip label="About notifications">Only shown while Bench's window isn't in front.</InfoTip>
        </span>
      }
    >
      {NOTIFY_ROWS.map((r) => (
        <SectionRow key={r.kind} label={r.label}>
          <Switch
            checked={on[r.kind]}
            onCheckedChange={(v) => {
              setNotifyEnabled(r.kind, v);
              setOn((o) => ({ ...o, [r.kind]: v }));
            }}
            aria-label={`Notify when ${r.label.toLowerCase()}`}
          />
        </SectionRow>
      ))}
    </SectionCard>
  );
}

/** CommandLineCard says whether the `bench` command works in a terminal
 *  (UI-51), and where the bundled one is if it isn't on PATH. */
export function CommandLineCard() {
  const { conn } = useDaemonContext();
  const [cli, setCli] = useState<{ on_path?: string; bundled?: string } | null>(null);
  const [error, setError] = useState<string | null>(null);
  const privacy = usePrivacy();

  useEffect(() => {
    if (!conn) return;
    let stale = false;
    withTimeout(
      api<{ on_path?: string; bundled?: string }>(conn, "GET", "/api/cli"),
      DEFAULT_TIMEOUT_MS,
      "Bench didn't answer in time",
    ).then(
      (c) => !stale && setCli(c),
      (e: unknown) => !stale && setError(errorMessage(e)),
    );
    return () => {
      stale = true;
    };
  }, [conn]);

  const folder = cli?.bundled?.replace(/[\\/][^\\/]+$/, "");
  const copyFolder = () =>
    folder &&
    navigator.clipboard.writeText(folder).then(
      () => toast.success("Folder copied", { description: "Add it to your PATH." }),
      (e: unknown) => toast.error("Couldn't copy", { description: errorMessage(e) }),
    );

  const description = error ? (
    <span role="alert" className="text-destructive">
      {error}
    </span>
  ) : cli === null ? (
    "Checking…"
  ) : cli.on_path ? (
    <>
      On your PATH: <span className="font-mono text-xs break-all">{privacy.path(cli.on_path)}</span>
    </>
  ) : cli.bundled ? (
    "Not on your PATH. Add its folder to use bench in any terminal."
  ) : (
    "Not on your PATH. Development builds don't include it."
  );

  return (
    <SectionCard title="Command line">
      <SectionRow label="bench" description={description}>
        {cli && !cli.on_path && folder ? (
          <Button size="sm" variant="outline" onClick={() => void copyFolder()}>
            <CopyIcon data-icon="inline-start" />
            Copy folder
          </Button>
        ) : null}
      </SectionRow>
    </SectionCard>
  );
}
