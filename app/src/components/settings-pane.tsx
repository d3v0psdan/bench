import { useEffect, useState } from "react";
import { disable, enable, isEnabled } from "@tauri-apps/plugin-autostart";
import { useTheme } from "next-themes";
import {
  ArrowsClockwiseIcon,
  CheckIcon,
  EyeSlashIcon,
  GearSixIcon,
  InfoIcon,
  MonitorIcon,
  MoonIcon,
  LockSimpleIcon,
  StethoscopeIcon,
  StopIcon,
  SunIcon,
  TrashIcon,
} from "@phosphor-icons/react";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { Switch } from "@/components/ui/switch";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { formatUptime } from "@/components/app-header";
import { SetupAction, StopBenchAction, UndoSetupAction } from "@/components/machine-actions";
import { CheckAction, CheckSteps, RecheckButton } from "@/components/health";
import { EditorCard } from "@/components/open-actions";
import { InfoTip } from "@/components/info-tip";
import {
  CommandLineCard,
  CopyReportButton,
  HelpCard,
  NotificationsCard,
  UpdatesCard,
  useAppVersion,
  VersionSkewAlert,
} from "@/components/lifecycle";
import {
  CHECK_LABELS,
  CHECK_TONES,
  SectionCard,
  SectionRow,
  SettingsLayout,
  StatusDot,
  type SettingsNavItem,
} from "@/components/page";
import { useDaemonContext } from "@/hooks/daemon-provider";
import { useRoute, type SettingsPage } from "@/hooks/route";
import { useDoctor, useHealth } from "@/hooks/use-doctor";
import { DEFAULT_TIMEOUT_MS, errorMessage, withTimeout } from "@/lib/async";
import { setStartDaemonOnOpen, startDaemonOnOpen, type PrivacyOption } from "@/lib/prefs";
import { MASK, setPrivacy, usePrivacy } from "@/lib/privacy";
import { cn } from "@/lib/utils";
import type { Check } from "@/lib/api";

// Each page's cards, once: the nav's sub-links and the rendered sections
// (with their ids) both come from this list. A card's title matches its label.
const PAGES: SettingsNavItem<SettingsPage>[] = [
  {
    value: "general",
    label: "General",
    icon: GearSixIcon,
    sections: [
      { id: "appearance", label: "Appearance", Content: AppearanceCard },
      { id: "editor", label: "Editor", Content: EditorCard },
      { id: "startup", label: "Startup", Content: StartupCard },
      { id: "notifications", label: "Notifications", Content: NotificationsCard },
      { id: "command-line", label: "Command line", Content: CommandLineCard },
      { id: "danger", label: "Danger zone", Content: StopBenchCard },
    ],
  },
  {
    value: "privacy",
    label: "Privacy",
    icon: EyeSlashIcon,
    sections: [{ id: "privacy-mode", label: "Privacy mode", Content: PrivacyCard }],
  },
  {
    value: "https",
    label: "HTTPS & DNS",
    icon: LockSimpleIcon,
    // One section: the setup and its removal share their pending state.
    sections: [{ id: "setup", label: "HTTPS & DNS", Content: HttpsPage }],
  },
  {
    value: "diagnostics",
    label: "Diagnostics",
    icon: StethoscopeIcon,
    sections: [{ id: "checks", label: "Diagnostics", Content: DiagnosticsPage }],
  },
  {
    value: "about",
    label: "About",
    icon: InfoIcon,
    sections: [
      { id: "system", label: "System", Content: SystemCard },
      { id: "updates", label: "Updates", Content: UpdatesCard },
      { id: "help", label: "Help", Content: HelpCard },
    ],
  },
];

/** SettingsPane: machine settings, a sub-nav on the left and
 *  section cards on the right. Machine-wide only; site settings live on the
 *  site's own Settings tab. */
export function SettingsPane() {
  const { route, navigate } = useRoute();
  const page = (route.kind === "home" && route.settings) || "general";

  return (
    <SettingsLayout
      items={PAGES}
      value={page}
      section={route.kind === "home" ? route.settingsSection : undefined}
      onChange={(settings, settingsSection) => navigate({ kind: "home", section: "settings", settings, settingsSection })}
    />
  );
}

function AppearanceCard() {
  const { theme, setTheme } = useTheme();

  return (
    <SectionCard title="Appearance">
      <SectionRow label="Theme">
        <ToggleGroup
          type="single"
          variant="outline"
          size="sm"
          value={theme}
          onValueChange={(v) => v && setTheme(v)}
          aria-label="Theme"
        >
          <ToggleGroupItem value="light">
            <SunIcon data-icon="inline-start" /> Light
          </ToggleGroupItem>
          <ToggleGroupItem value="dark">
            <MoonIcon data-icon="inline-start" /> Dark
          </ToggleGroupItem>
          <ToggleGroupItem value="system">
            <MonitorIcon data-icon="inline-start" /> System
          </ToggleGroupItem>
        </ToggleGroup>
      </SectionRow>
    </SectionCard>
  );
}

function StopBenchCard() {
  const { pending } = useDaemonContext();

  return (
    <SectionCard title="Danger zone" tone="danger">
      <SectionRow label="Stop Bench">
        <StopBenchAction
          trigger={
            <Button variant="destructive" disabled={pending !== null}>
              {pending === "stop" ? (
                <Spinner data-icon="inline-start" className="motion-reduce:animate-none" />
              ) : (
                <StopIcon data-icon="inline-start" />
              )}
              Stop Bench
            </Button>
          }
        />
      </SectionRow>
    </SectionCard>
  );
}

const PRIVACY_ROWS: Array<{ option: PrivacyOption; label: string; tip: string }> = [
  { option: "names", label: "Site names", tip: `demo.test shows as ${MASK}.test.` },
  { option: "folders", label: "Folders", tip: "Site folders and Bench's own paths, which include your user name." },
  { option: "secrets", label: "Passwords and keys", tip: "Stay masked, with no way to show them." },
  { option: "mail", label: "Mail", tip: "Senders, subjects and message contents." },
];

/** PrivacyCard: masking for screen sharing and recording. A window display
 *  preference; copy buttons and open actions keep using the real values. */
function PrivacyCard() {
  const { prefs } = usePrivacy();

  return (
    <SectionCard title="Privacy mode">
      <SectionRow
        label={
          <span className="inline-flex items-center gap-1">
            Hide private details on screen
            <InfoTip label="About privacy mode">
              For screen sharing and recording. Only the display changes: copy buttons still copy the real values.
            </InfoTip>
          </span>
        }
      >
        <Switch
          checked={prefs.on}
          onCheckedChange={(on) => setPrivacy("on", on)}
          aria-label="Hide private details on screen"
        />
      </SectionRow>
      {PRIVACY_ROWS.map((r) => (
        <SectionRow
          key={r.option}
          className="pl-10"
          label={
            <span className={cn("inline-flex items-center gap-1", !prefs.on && "text-muted-foreground")}>
              {r.label}
              <InfoTip label={`About hiding ${r.label.toLowerCase()}`}>{r.tip}</InfoTip>
            </span>
          }
        >
          <Switch
            checked={prefs[r.option]}
            disabled={!prefs.on}
            onCheckedChange={(on) => setPrivacy(r.option, on)}
            aria-label={`Hide ${r.label.toLowerCase()}`}
          />
        </SectionRow>
      ))}
    </SectionCard>
  );
}

/** StartupCard: whether Bench comes back by itself after a reboot. Both are
 *  instant, reversible toggles (ui-standard C2). */
function StartupCard() {
  const [atLogin, setAtLogin] = useState<boolean | null>(null);
  const [loginError, setLoginError] = useState<string | null>(null);
  const [onOpen, setOnOpen] = useState(startDaemonOnOpen);

  useEffect(() => {
    withTimeout(isEnabled(), DEFAULT_TIMEOUT_MS, "The sign-in setting didn't load in time").then(
      setAtLogin,
      (e: unknown) => setLoginError(errorMessage(e)),
    );
  }, []);

  const toggleLogin = (on: boolean) => {
    setLoginError(null);
    withTimeout(on ? enable() : disable(), DEFAULT_TIMEOUT_MS, "The sign-in setting didn't change in time").then(
      () => setAtLogin(on),
      (e: unknown) => setLoginError(errorMessage(e)),
    );
  };

  return (
    <SectionCard title="Startup">
      <SectionRow
        label={
          <span className="inline-flex items-center gap-1">
            Open Bench when I sign in
            <InfoTip label="About opening at sign-in">Opens in the tray, not on screen.</InfoTip>
          </span>
        }
        description={
          loginError ? (
            <span role="alert" className="text-destructive">
              {loginError}
            </span>
          ) : null
        }
      >
        {atLogin === null && !loginError ? (
          <Spinner className="text-muted-foreground motion-reduce:animate-none" />
        ) : (
          <Switch checked={!!atLogin} onCheckedChange={toggleLogin} aria-label="Open Bench when I sign in" />
        )}
      </SectionRow>
      <SectionRow label="Start sites when Bench opens">
        <Switch
          checked={onOpen}
          onCheckedChange={(on) => {
            setStartDaemonOnOpen(on);
            setOnOpen(on);
          }}
          aria-label="Start sites when Bench opens"
        />
      </SectionRow>
    </SectionCard>
  );
}

function HttpsPage() {
  const { setupState, tasks } = useDaemonContext();
  const doctor = useDoctor();
  const setupErr = typeof setupState === "object" ? setupState.error : null;
  const loading = doctor.checks === null && !doctor.error;
  // Whether setup is needed comes from the daemon's checks, not from
  // this window's memory, so it survives a relaunch (ui-standard F3).
  const { needsSetup } = useHealth();
  const setupDone = needsSetup === false;
  // What there is to remove, and whether a setup or removal is already
  // waiting on the administrator prompt (it runs as a daemon task).
  const trustSet = doctor.byId("trust")?.status === "ok";
  const dnsSet = doctor.byId("dns")?.status === "ok";
  const setupBusy = setupState === "running" || !!tasks?.some((t) => t.kind === "setup" && t.state === "running");
  const [undoErr, setUndoErr] = useState<string | null>(null);
  // True for as long as this window's removal request is open, so the
  // buttons show it even before (or without) the daemon's task event. The
  // error stays until the next attempt, which clears it.
  const [undoPending, setUndoPending] = useState(false);
  const removing = setupBusy || undoPending;
  // The newest setup task finishing cleanly (a later setup or removal, or
  // one the client stopped waiting for) makes an old removal error stale.
  const newestSetup = tasks?.find((t) => t.kind === "setup");
  useEffect(() => {
    if (newestSetup?.state === "done") setUndoErr(null);
  }, [newestSetup?.id, newestSetup?.state]);
  // What's set up is only known once the checks ran (ui-standard F4).
  const undoDescription = undoErr ? (
    <span role="alert" className="break-words text-destructive">
      Couldn't remove the setup: {undoErr}
    </span>
  ) : loading ? (
    "Checking what's set up on this machine…"
  ) : doctor.error ? (
    "Couldn't check what's set up on this machine. Check again first."
  ) : trustSet || dnsSet ? null : (
    "Nothing to remove."
  );

  return (
    <>
      <SectionCard title="HTTPS & DNS">
        <SectionRow
          label={
            <span className="inline-flex items-center gap-1">
              Trust HTTPS and route *.test
              <InfoTip label="About the HTTPS and DNS setup">
                Trusts Bench's local certificate authority and sends *.test names to this machine. Needs admin rights
                once.
              </InfoTip>
            </span>
          }
          description={
            setupErr ? (
              <span role="alert" className="break-words text-destructive">
                Setup failed: {setupErr}
              </span>
            ) : null
          }
        >
          <SetupAction
            trigger={
              <Button variant={setupDone ? "outline" : "brand"} disabled={removing || loading}>
                {removing ? (
                  <Spinner data-icon="inline-start" className="motion-reduce:animate-none" />
                ) : setupDone ? (
                  <CheckIcon data-icon="inline-start" />
                ) : (
                  <LockSimpleIcon data-icon="inline-start" />
                )}
                {removing ? "Waiting for approval…" : setupDone ? "Run again" : "Run setup"}
              </Button>
            }
          />
        </SectionRow>
        <CheckRow check={doctor.byId("trust")} loading={loading} fallback="HTTPS certificates" showAction={false} />
        <CheckRow check={doctor.byId("dns")} loading={loading} fallback="*.test DNS" showAction={false} />
        <CheckRow check={doctor.byId("https")} loading={loading} fallback="HTTPS port" showAction={false} />
      </SectionCard>

      <SectionCard title="Danger zone" tone="danger">
        <SectionRow
          label="Remove the HTTPS and DNS setup"
          description={undoDescription}
        >
          {doctor.error ? <RecheckButton /> : null}
          <UndoSetupAction
            trust={trustSet}
            onStart={() => setUndoPending(true)}
            onSettled={() => setUndoPending(false)}
            onError={setUndoErr}
            trigger={
              <Button variant="destructive" disabled={removing || loading || !!doctor.error || !(trustSet || dnsSet)}>
                {removing ? (
                  <Spinner data-icon="inline-start" className="motion-reduce:animate-none" />
                ) : (
                  <TrashIcon data-icon="inline-start" />
                )}
                Remove setup
              </Button>
            }
          />
        </SectionRow>
      </SectionCard>
    </>
  );
}

function CheckRow({
  check,
  loading,
  fallback,
  sameFixAs,
  showAction = true,
}: {
  check?: Check;
  loading: boolean;
  fallback: string;
  /** Title of an earlier row with the same steps (the Laravel Herd advice). */
  sameFixAs?: string;
  /** Off where the page already offers the action (HTTPS & DNS). */
  showAction?: boolean;
}) {
  const privacy = usePrivacy();
  return (
    <SectionRow
      label={check?.title ?? fallback}
      description={
        <>
          <span className="block">{check ? privacy.text(check.detail) : loading ? "Checking…" : "No result."}</span>
          {sameFixAs ? (
            <span className="mt-1.5 block text-foreground">Same fix as {sameFixAs}.</span>
          ) : (
            <CheckSteps steps={check?.steps} className="mt-1.5" />
          )}
        </>
      }
    >
      {check && showAction ? <CheckAction check={check} /> : null}
      {loading ? (
        <StatusDot tone="neutral" busy>
          Checking
        </StatusDot>
      ) : check ? (
        <StatusDot tone={CHECK_TONES[check.status]}>{CHECK_LABELS[check.status]}</StatusDot>
      ) : (
        <StatusDot tone="neutral">Unknown</StatusDot>
      )}
    </SectionRow>
  );
}

function DiagnosticsPage() {
  const doctor = useDoctor();

  return (
    <SectionCard
      title={
        <span className="inline-flex items-center gap-1">
          Diagnostics
          <InfoTip label="About diagnostics">
            The same checks as <code className="font-mono text-xs">bench doctor</code>.
          </InfoTip>
        </span>
      }
      action={
        <div className="flex items-center gap-2">
          <CopyReportButton />
          <Button variant="outline" size="sm" onClick={doctor.rerun} disabled={doctor.running}>
            {doctor.running ? (
              <Spinner data-icon="inline-start" className="motion-reduce:animate-none" />
            ) : (
              <ArrowsClockwiseIcon data-icon="inline-start" />
            )}
            Run again
          </Button>
        </div>
      }
    >
      {doctor.error ? (
        <p role="alert" className="px-4 py-4 text-[13px] break-words text-destructive">
          Couldn't run the checks: {doctor.error}
        </p>
      ) : doctor.checks === null ? (
        <div className="flex flex-col gap-3 p-4" aria-busy>
          {[0, 1, 2, 3].map((i) => (
            <Skeleton key={i} className="h-10 w-full" />
          ))}
        </div>
      ) : (
        doctor.checks.map((c, i, all) => (
          <CheckRow
            key={c.id}
            check={c}
            loading={false}
            fallback={c.id}
            sameFixAs={all.slice(0, i).find((prev) => !!c.steps?.length && prev.steps?.join("\n") === c.steps.join("\n"))?.title}
          />
        ))
      )}
    </SectionCard>
  );
}

function SystemCard() {
  const { status, uptime } = useDaemonContext();
  const appVersion = useAppVersion();

  return (
    <>
      <VersionSkewAlert />
      <SectionCard title="System">
        <SectionRow label="Bench">
          <span className="font-mono text-[13px] tabular-nums">{appVersion ?? "…"}</span>
        </SectionRow>
        <SectionRow label="benchd">
          <span className="font-mono text-[13px] tabular-nums">{status?.version}</span>
        </SectionRow>
        <SectionRow label="Platform">
          <span className="font-mono text-[13px]">
            {status?.os}/{status?.arch}
          </span>
        </SectionRow>
        <SectionRow label="Process" description={status ? `Running for ${formatUptime(uptime)}.` : undefined}>
          <span className="font-mono text-[13px] tabular-nums">pid {status?.pid}</span>
        </SectionRow>
      </SectionCard>
    </>
  );
}
