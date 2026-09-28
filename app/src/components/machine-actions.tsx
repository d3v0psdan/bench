import type { ReactNode } from "react";
import { LockSimpleIcon, StopIcon, TrashIcon } from "@phosphor-icons/react";
import { toast } from "sonner";
import { ConfirmAction } from "@/components/confirm-action";
import { useDaemonContext } from "@/hooks/daemon-provider";
import { useDoctor } from "@/hooks/use-doctor";
import { api } from "@/lib/api";
import { errorMessage, withTimeout } from "@/lib/async";

// Machine-wide actions, confirmed once here and reused by the header pill,
// Settings and the Get started guide (docs/ui-standard.md C1, C5).

/** StopBenchAction confirms stopping the daemon, naming what goes down. */
export function StopBenchAction({
  trigger,
  open,
  onOpenChange,
}: {
  trigger?: ReactNode;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
}) {
  const { stop, sites, services } = useDaemonContext();
  const running = (services ?? []).filter((s) => s.state === "running").map((s) => s.name);
  const siteCount = sites?.length ?? 0;

  return (
    <ConfirmAction
      trigger={trigger}
      open={open}
      onOpenChange={onOpenChange}
      title="Stop Bench?"
      description="Nothing is deleted."
      details={[
        { label: "Sites", value: siteCount === 0 ? "None yet" : `${siteCount} stop being served` },
        { label: "Services", value: running.length === 0 ? "None running" : `${running.join(", ")} stop` },
      ]}
      confirmLabel="Stop Bench"
      confirmIcon={<StopIcon data-icon="inline-start" />}
      tone="destructive"
      onConfirm={stop}
    />
  );
}

const DNS_CHANGE: Record<string, string> = {
  windows: "A Windows DNS rule (NRPT) that sends *.test names to Bench",
  darwin: "The file /etc/resolver/test, pointing *.test at Bench",
  linux: "A systemd-resolved rule in /etc/systemd/resolved.conf.d/ for *.test",
};

const TRUST_CHANGE: Record<string, string> = {
  windows: "Bench's local certificate authority, added to the Windows Root store",
  darwin: "Bench's local certificate authority, added to the System keychain",
  linux: "Bench's local certificate authority, added to the system CA bundle",
};

/** SetupAction confirms the elevated HTTPS and DNS setup, listing exactly
 *  what changes on this machine before the OS asks for permission. */
export function SetupAction({ trigger }: { trigger: ReactNode }) {
  const { runSetup, status } = useDaemonContext();
  const os = status?.os ?? "";

  return (
    <ConfirmAction
      trigger={trigger}
      title="Set up HTTPS and *.test?"
      description="Needs administrator permission once."
      details={[
        { label: "Trust", value: TRUST_CHANGE[os] ?? "Bench's local certificate authority, added to the system trust store" },
        { label: "DNS", value: DNS_CHANGE[os] ?? "A resolver rule that sends *.test names to Bench" },
      ]}
      confirmLabel="Set up HTTPS and DNS"
      confirmIcon={<LockSimpleIcon data-icon="inline-start" />}
      onConfirm={runSetup}
    />
  );
}

// The approval prompt can wait on the user; the task shows it in Activity.
const UNDO_TIMEOUT_MS = 10 * 60_000;

/** UndoSetupAction removes what SetupAction added (ui-standard C5), after
 *  listing it, with the same single administrator prompt. Like Run setup,
 *  the dialog closes once the removal starts (C4): the running setup task
 *  shows progress on the buttons, and onError puts a failure next to the
 *  row that started it. DNS removal is safe to repeat, so it always runs;
 *  the certificate is removed only when it's trusted. */
export function UndoSetupAction({
  trigger,
  trust,
  onStart,
  onSettled,
  onError,
}: {
  trigger: ReactNode;
  trust: boolean;
  /** Called as the removal request starts. */
  onStart?: () => void;
  /** Called once the request settles, success or failure. */
  onSettled?: () => void;
  onError: (message: string | null) => void;
}) {
  const { conn, status } = useDaemonContext();
  const { rerun } = useDoctor();
  const os = status?.os ?? "";

  const undo = () => {
    if (!conn) throw new Error("Bench isn't connected");
    onError(null);
    onStart?.();
    withTimeout(
      api<{ messages?: string[] }>(conn, "POST", "/api/setup/undo", { dns: true, trust }),
      UNDO_TIMEOUT_MS,
      "The administrator prompt wasn't answered in time",
    )
      .then((res) =>
        toast.success("Setup removed", { description: res?.messages?.join("; ") || undefined }),
      )
      .catch((e: unknown) => onError(errorMessage(e)))
      .finally(() => {
        onSettled?.();
        rerun();
      });
  };

  return (
    <ConfirmAction
      trigger={trigger}
      title="Remove the HTTPS and DNS setup?"
      description="Needs administrator permission once. Sites stop working over https://*.test until you run setup again."
      details={[
        ...(trust
          ? [{ label: "Trust", value: (TRUST_CHANGE[os] ?? "Bench's local certificate authority").replace(", added to", ", removed from") }]
          : []),
        { label: "DNS", value: `${DNS_CHANGE[os] ?? "The *.test resolver rule"}: removed` },
      ]}
      confirmLabel="Remove setup"
      confirmIcon={<TrashIcon data-icon="inline-start" />}
      tone="destructive"
      onConfirm={undo}
    />
  );
}
