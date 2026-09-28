import { useEffect, useRef, useState } from "react";
import {
  ArrowRightIcon,
  CaretDownIcon,
  CheckCircleIcon,
  ClockCounterClockwiseIcon,
  ProhibitIcon,
  WarningCircleIcon,
  XIcon,
} from "@phosphor-icons/react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Progress } from "@/components/ui/progress";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { ListCard, PageHeader, SectionTitle } from "@/components/page";
import { LoadFailed } from "@/components/load-failed";
import { TaskLog } from "@/components/task-log";
import { useDaemonContext } from "@/hooks/daemon-provider";
import { useRoute } from "@/hooks/route";
import { api, formatBytes, type Task } from "@/lib/api";
import { errorMessage, withTimeout } from "@/lib/async";
import { usePrivacy } from "@/lib/privacy";
import { cleanLog } from "@/lib/task-log";
import { cn } from "@/lib/utils";

const CANCEL_TIMEOUT_MS = 10_000;
// How long a finished task stays in the tray. Failures stay until dismissed.
const TRAY_LINGER_MS = 4_000;
const TRAY_ROWS = 3;

/** useCancelTask asks the daemon to cancel a task; failures show as toasts. */
export function useCancelTask() {
  const { conn } = useDaemonContext();
  const privacy = usePrivacy();
  return (task: Task) => {
    if (!conn) return;
    withTimeout(
      api(conn, "POST", `/api/tasks/${encodeURIComponent(task.id)}/cancel`),
      CANCEL_TIMEOUT_MS,
      "Bench didn't answer the cancel in time",
    ).catch((e: unknown) =>
      toast.error(`Couldn't cancel ${privacy.text(task.title).toLowerCase()}`, { description: errorMessage(e) }),
    );
  };
}

/** TaskProgress is one running or finished task: what it is, how far it
 *  got, and a cancel button while cancelling is safe (ui-standard C3). */
export function TaskProgress({
  task,
  onDismiss,
  details = false,
}: {
  task: Task;
  onDismiss?: () => void;
  /** Offer the task's step-by-step output (the Activity page); elsewhere a
   *  task with output links there. */
  details?: boolean;
}) {
  const cancel = useCancelTask();
  const { navigate } = useRoute();
  const privacy = usePrivacy();
  const title = privacy.text(task.title);
  // Output starts open while the task runs, so there's something to watch.
  const [showOutput, setShowOutput] = useState(task.state === "running");
  const d = task.download;
  const running = task.state === "running";
  const percent =
    d && d.phase === "extracting" ? 100 : d && d.total > 0 ? Math.min(100, (d.received / d.total) * 100) : null;

  return (
    <div className="flex items-start gap-3">
      <TaskIcon task={task} />
      <div className="min-w-0 flex-1">
        <div className="flex items-center justify-between gap-2">
          <p className="truncate text-[13px] font-medium">{title}</p>
          {running && task.cancellable ? (
            <Tooltip>
              <TooltipTrigger asChild>
                <Button variant="ghost" size="icon-xs" aria-label={`Cancel ${title}`} onClick={() => cancel(task)}>
                  <XIcon />
                </Button>
              </TooltipTrigger>
              <TooltipContent>Cancel</TooltipContent>
            </Tooltip>
          ) : !running && onDismiss ? (
            <Button variant="ghost" size="icon-xs" aria-label={`Dismiss ${title}`} onClick={onDismiss}>
              <XIcon />
            </Button>
          ) : null}
        </div>
        {running ? (
          <>
            {percent !== null ? (
              <Progress className="mt-2 h-1.5" value={percent} aria-label={`${title} progress`} />
            ) : null}
            <p className="mt-1.5 text-xs text-muted-foreground tabular-nums">{describeRunning(task)}</p>
          </>
        ) : (
          <p className={cn("mt-0.5 text-xs break-words", task.state === "failed" ? "text-destructive" : "text-muted-foreground")}>
            {privacy.text(describeFinished(task))}
          </p>
        )}
        {task.log_file ? (
          details ? (
            <div className="mt-2 flex flex-col gap-2">
              <Button
                size="xs"
                variant="outline"
                className="self-start"
                aria-expanded={showOutput}
                onClick={() => setShowOutput((s) => !s)}
              >
                <CaretDownIcon
                  data-icon="inline-start"
                  className={cn("transition-transform duration-150 motion-reduce:transition-none", !showOutput && "-rotate-90")}
                />
                {showOutput ? "Hide output" : "Show output"}
              </Button>
              {showOutput ? <TaskLog task={task} /> : null}
            </div>
          ) : (
            <Button
              size="xs"
              variant="ghost"
              className="mt-1 -ml-2"
              onClick={() => navigate({ kind: "home", section: "activity" })}
            >
              Details
              <ArrowRightIcon data-icon="inline-end" />
            </Button>
          )
        ) : null}
      </div>
    </div>
  );
}

function TaskIcon({ task }: { task: Task }) {
  const box = "mt-0.5 flex size-4 shrink-0 items-center justify-center";
  switch (task.state) {
    case "running":
      return (
        <span className={box}>
          <Spinner className="size-4 text-brand motion-reduce:animate-none" />
        </span>
      );
    case "done":
      return <CheckCircleIcon weight="fill" className={cn(box, "text-success-solid")} aria-label="Done" />;
    case "failed":
      return <WarningCircleIcon weight="fill" className={cn(box, "text-destructive-solid")} aria-label="Failed" />;
    default:
      return <ProhibitIcon className={cn(box, "text-muted-foreground")} aria-label="Cancelled" />;
  }
}

const PHASES: Record<string, string> = {
  downloading: "Downloading",
  extracting: "Extracting",
  initializing: "Setting up the data directory",
  starting: "Starting",
  copying: "Copying data",
  cancelling: "Cancelling",
  "waiting for approval": "Waiting for your approval",
};

function describeRunning(task: Task): string {
  const d = task.download;
  const phase = task.phase ? (PHASES[task.phase] ?? task.phase) : "Working";
  if (task.phase === "downloading" && d && d.phase === "downloading") {
    const amount = d.total > 0 ? `${formatBytes(d.received)} of ${formatBytes(d.total)}` : formatBytes(d.received);
    return `${phase} · ${amount}${d.files > 1 ? ` · file ${d.file} of ${d.files}` : ""}`;
  }
  if (task.phase === "downloading" && d?.phase === "extracting") return "Extracting";
  if (task.phase !== "cancelling" && !task.cancellable && task.kind !== "setup") return `${phase} · can't be cancelled now`;
  return phase;
}

function describeFinished(task: Task): string {
  const took = task.ended_at ? ` in ${formatDuration(new Date(task.ended_at).getTime() - new Date(task.started_at).getTime())}` : "";
  if (task.state === "done") return `Done${took}`;
  if (task.state === "cancelled") return "Cancelled";
  return task.error ? cleanLog(task.error) : "Failed";
}

function formatDuration(ms: number): string {
  const s = Math.max(1, Math.round(ms / 1000));
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  return m < 60 ? `${m}m ${s % 60}s` : `${Math.floor(m / 60)}h ${m % 60}m`;
}

/** ActivityTray floats bottom-right on every page while something runs, so
 *  a long download can always be watched and cancelled (ui-standard C4).
 *  Finished tasks linger briefly; failures stay until dismissed. */
export function ActivityTray() {
  const { tasks } = useDaemonContext();
  const { route, navigate } = useRoute();
  // Tasks this window saw running: only those get a finished row here
  // (history from before the window opened lives on the Activity page).
  const seenRunning = useRef(new Set<string>());
  const [hidden, setHidden] = useState<Set<string>>(new Set());

  useEffect(() => {
    const timers: number[] = [];
    for (const t of tasks ?? []) {
      if (t.state === "running") {
        seenRunning.current.add(t.id);
      } else if (t.state !== "failed" && seenRunning.current.has(t.id) && !hidden.has(t.id)) {
        timers.push(window.setTimeout(() => setHidden((h) => new Set(h).add(t.id)), TRAY_LINGER_MS));
      }
    }
    return () => timers.forEach((id) => window.clearTimeout(id));
  }, [tasks, hidden]);

  const visible = (tasks ?? []).filter(
    (t) => t.state === "running" || (seenRunning.current.has(t.id) && !hidden.has(t.id)),
  );
  if (visible.length === 0 || (route.kind === "home" && route.section === "activity")) return null;
  const running = visible.filter((t) => t.state === "running").length;

  return (
    <aside
      aria-label="Activity"
      className="fixed right-4 bottom-4 z-40 w-80 rounded-xl bg-popover shadow-popover duration-200 animate-in fade-in-0 slide-in-from-bottom-2 motion-reduce:animate-none"
    >
      <div className="flex items-center justify-between gap-2 border-b px-4 py-2.5">
        <p role="status" className="text-[13px] font-medium">
          {running > 0 ? `${running} running` : "Activity"}
        </p>
        <Button variant="ghost" size="xs" onClick={() => navigate({ kind: "home", section: "activity" })}>
          View all <ArrowRightIcon data-icon="inline-end" />
        </Button>
      </div>
      <div className="flex flex-col gap-4 px-4 py-3">
        {visible.slice(0, TRAY_ROWS).map((t) => (
          <TaskProgress key={t.id} task={t} onDismiss={() => setHidden((h) => new Set(h).add(t.id))} />
        ))}
        {visible.length > TRAY_ROWS ? (
          <p className="text-xs text-muted-foreground">{visible.length - TRAY_ROWS} more on the Activity page</p>
        ) : null}
      </div>
    </aside>
  );
}

/** ActivityPane lists running tasks and recent history. */
export function ActivityPane() {
  const { tasks, loadError, reloadData } = useDaemonContext();
  const running = (tasks ?? []).filter((t) => t.state === "running");
  const finished = (tasks ?? []).filter((t) => t.state !== "running");

  return (
    <>
      <PageHeader title="Activity" />
      {tasks === null && loadError ? (
        <LoadFailed title="Couldn't load activity" message={loadError} onRetry={reloadData} />
      ) : tasks === null ? (
        <Skeleton className="h-32 w-full rounded-lg" aria-busy />
      ) : tasks.length === 0 ? (
        <Empty className="border border-dashed bg-card">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <ClockCounterClockwiseIcon />
            </EmptyMedia>
            <EmptyTitle>Nothing has run yet</EmptyTitle>
            <EmptyDescription>Installs, new services and setup show up here.</EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <div className="flex flex-col gap-8">
          {running.length > 0 ? (
            <section>
              <SectionTitle>Running</SectionTitle>
              <ListCard>
                {running.map((t) => (
                  <div key={t.id} className="px-4 py-3.5">
                    <TaskProgress task={t} details />
                  </div>
                ))}
              </ListCard>
            </section>
          ) : null}
          {finished.length > 0 ? (
            <section>
              <SectionTitle>Recent</SectionTitle>
              <ListCard>
                {finished.map((t) => (
                  <div key={t.id} className="px-4 py-3">
                    <TaskProgress task={t} details />
                  </div>
                ))}
              </ListCard>
            </section>
          ) : null}
        </div>
      )}
    </>
  );
}
