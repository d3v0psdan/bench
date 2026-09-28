import { useEffect, useState } from "react";
import {
  ArrowSquareOutIcon,
  ClipboardTextIcon,
  CopyIcon,
  DatabaseIcon,
  DotsThreeIcon,
  PlayIcon,
  StopIcon,
  TrashIcon,
} from "@phosphor-icons/react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Field, FieldContent, FieldLabel } from "@/components/ui/field";
import { Spinner } from "@/components/ui/spinner";
import { Switch } from "@/components/ui/switch";
import { ConfirmAction } from "@/components/confirm-action";
import { CloneServiceDialog } from "@/components/service-dialogs";
import { SERVICE_LABELS, TRANSITIONAL } from "@/components/services/service-meta";
import { useDaemonContext } from "@/hooks/daemon-provider";
import { api, type Service } from "@/lib/api";
import { DEFAULT_TIMEOUT_MS, errorMessage, withTimeout } from "@/lib/async";
import { openExternal } from "@/lib/open";
import { usePrivacy } from "@/lib/privacy";

// Matches the daemon's own bound on a start (which can reinstall a
// deleted build); a stop waits at most a minute for a clean shutdown.
const ACTION_TIMEOUT_MS = 11 * 60_000;

/** useServiceActions holds one instance's actions and their dialogs, shared
 *  by the list row and the detail sheet. Start, stop and autostart are
 *  instant and reversible (C2); clone and delete go through dialogs (C1). */
export function useServiceActions(s: Service) {
  const { conn } = useDaemonContext();
  const [busy, setBusy] = useState<"start" | "stop" | "autostart" | "open" | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [cloneOpen, setCloneOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [keepData, setKeepData] = useState(false);
  const privacy = usePrivacy();

  // A new state from the daemon supersedes an earlier action error (e.g. a
  // start the client gave up on that then succeeded).
  useEffect(() => setError(null), [s.state]);

  const running = s.state === "running" || s.state === "starting" || s.state === "backoff";
  const transitional = TRANSITIONAL.has(s.state);
  const path = `/api/services/${encodeURIComponent(s.name)}`;

  const run = async (kind: "start" | "stop" | "autostart", url: string, body?: unknown) => {
    if (!conn || busy) return;
    setBusy(kind);
    setError(null);
    try {
      await withTimeout(
        api(conn, "POST", url, body),
        ACTION_TIMEOUT_MS,
        `${kind} is taking too long; its logs may say why`,
      );
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(null);
    }
  };

  // Deleting is confirmed (C1); a failure shows inside the dialog.
  const remove = async () => {
    if (!conn) throw new Error("Bench isn't connected");
    await withTimeout(
      api(conn, "DELETE", keepData ? `${path}?keep_data=1` : path),
      ACTION_TIMEOUT_MS,
      "Deleting is taking too long; its logs may say why",
    );
    toast.success(`${s.name} deleted${keepData ? ", data kept" : ""}`);
  };

  // The database app appearing is the success state; a failure shows here.
  const openClient = async () => {
    if (!conn || busy) return;
    setBusy("open");
    setError(null);
    try {
      await withTimeout(api(conn, "POST", `${path}/open`), DEFAULT_TIMEOUT_MS, "The database app didn't open in time");
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(null);
    }
  };

  const openConsole = () => {
    if (!s.console_url) return;
    if (s.state !== "running") {
      setError(`${s.name} isn't running; start it, then open its console`);
      return;
    }
    openExternal(s.console_url);
  };

  const copyEnv = () =>
    navigator.clipboard.writeText(s.env.join("\n")).then(
      () => toast.success(`.env lines for ${s.name} copied`),
      (e: unknown) => setError(`Couldn't copy: ${errorMessage(e)}`),
    );

  const dialogs = (
    <>
      <CloneServiceDialog source={s} open={cloneOpen} onOpenChange={setCloneOpen} />
      <ConfirmAction
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        title={`Delete ${s.name}?`}
        description="The instance stops and is removed from Bench."
        details={[
          { label: "Instance", value: `${SERVICE_LABELS[s.service] ?? s.service} ${s.version || s.channel}` },
          { label: "Data", value: <span className="font-mono text-xs">{privacy.path(s.data_dir)}</span> },
          ...(running ? [{ label: "Stops", value: "Now; apps using it lose their connection" }] : []),
        ]}
        confirmLabel={keepData ? `Delete ${s.name}, keep data` : `Delete ${s.name} and its data`}
        confirmIcon={<TrashIcon data-icon="inline-start" />}
        tone="destructive"
        timeoutMs={ACTION_TIMEOUT_MS}
        onConfirm={remove}
      >
        <Field orientation="horizontal">
          <Switch id={`keep-${s.name}`} checked={keepData} onCheckedChange={setKeepData} />
          <FieldContent>
            <FieldLabel htmlFor={`keep-${s.name}`}>Keep data</FieldLabel>
          </FieldContent>
        </Field>
      </ConfirmAction>
    </>
  );

  return {
    busy,
    error,
    running,
    transitional,
    toggleRunning: () => void run(running ? "stop" : "start", `${path}/${running ? "stop" : "start"}`),
    setAutostart: (enabled: boolean) => void run("autostart", `${path}/autostart`, { enabled }),
    copyEnv,
    openClient: () => void openClient(),
    openConsole,
    openClone: () => setCloneOpen(true),
    openDelete: () => setDeleteOpen(true),
    dialogs,
  };
}

export type ServiceActions = ReturnType<typeof useServiceActions>;

export function StartStopButton({ s, a }: { s: Service; a: ServiceActions }) {
  return (
    <Button
      size="sm"
      variant="outline"
      className="w-20"
      onClick={a.toggleRunning}
      disabled={a.busy !== null || (a.transitional && s.state !== "backoff")}
    >
      {a.busy === "start" || a.busy === "stop" ? (
        <Spinner data-icon="inline-start" className="motion-reduce:animate-none" />
      ) : a.running ? (
        <StopIcon data-icon="inline-start" />
      ) : (
        <PlayIcon data-icon="inline-start" />
      )}
      {/* The in-flight action keeps its label until it resolves. */}
      {a.busy === "start" ? "Start" : a.busy === "stop" || a.running ? "Stop" : "Start"}
    </Button>
  );
}

export function ServiceMenu({ s, a }: { s: Service; a: ServiceActions }) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button size="icon-sm" variant="ghost" aria-label={`More actions for ${s.name}`} disabled={a.busy !== null}>
          <DotsThreeIcon weight="bold" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {s.url || s.console_url ? (
          <>
            <DropdownMenuGroup>
              {s.url ? (
                <DropdownMenuItem onSelect={a.openClient}>
                  <DatabaseIcon />
                  Open in client
                </DropdownMenuItem>
              ) : null}
              {s.console_url ? (
                <DropdownMenuItem onSelect={a.openConsole}>
                  <ArrowSquareOutIcon />
                  Open console
                </DropdownMenuItem>
              ) : null}
            </DropdownMenuGroup>
            <DropdownMenuSeparator />
          </>
        ) : null}
        <DropdownMenuGroup>
          <DropdownMenuItem onSelect={() => void a.copyEnv()} disabled={s.env.length === 0}>
            <ClipboardTextIcon />
            Copy .env
          </DropdownMenuItem>
          {s.service !== "mailpit" ? (
            <DropdownMenuItem onSelect={a.openClone} disabled={a.transitional}>
              <CopyIcon />
              Clone with data…
            </DropdownMenuItem>
          ) : null}
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuGroup>
          <DropdownMenuItem variant="destructive" onSelect={a.openDelete}>
            <TrashIcon />
            Delete…
          </DropdownMenuItem>
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
