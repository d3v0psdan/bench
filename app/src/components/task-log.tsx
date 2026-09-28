import { useLayoutEffect, useRef, useState } from "react";
import {
  CaretRightIcon,
  CheckCircleIcon,
  FolderOpenIcon,
  ProhibitIcon,
  WarningCircleIcon,
} from "@phosphor-icons/react";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { useLogStream } from "@/components/log-view";
import type { Task } from "@/lib/api";
import { revealPath } from "@/lib/open";
import { splitSteps } from "@/lib/task-log";
import { cn } from "@/lib/utils";

/** How much of a task's log to read: a creation writes a few thousand
 *  lines at most (composer and npm progress). */
const TASK_LOG_LINES = 5000;

type StepState = "done" | "running" | "failed" | "cancelled";

/** TaskLog shows a task's output as one collapsible section per step. The
 *  step at work (or the one that failed) is open and follows live; the
 *  finished ones fold away. */
export function TaskLog({ task }: { task: Task }) {
  const running = task.state === "running";
  const { text, tail, error, retry } = useLogStream(`/api/tasks/${task.id}/log`, running, TASK_LOG_LINES);
  const [expanded, setExpanded] = useState<Record<number, boolean>>({});

  if (error) {
    return (
      <p role="alert" className="flex items-center gap-2 text-xs text-destructive">
        Couldn't load the output: {error}
        <Button size="xs" variant="outline" onClick={retry}>
          Try again
        </Button>
      </p>
    );
  }
  if (tail === null) return <Skeleton className="h-24 w-full rounded-lg" aria-busy />;

  const sections = splitSteps(text);
  const last = sections.length - 1;
  const stateOf = (i: number): StepState => {
    if (i < last) return "done";
    if (running) return "running";
    return task.state === "failed" ? "failed" : task.state === "cancelled" ? "cancelled" : "done";
  };

  return (
    <div className="flex flex-col gap-1.5">
      {sections.length === 0 ? (
        <p className="rounded-lg bg-well px-3 py-2 text-xs text-muted-foreground">Nothing has been logged yet.</p>
      ) : (
        sections.map((s, i) => {
          const state = stateOf(i);
          const open = expanded[i] ?? (i === last && state !== "done");
          return (
            <StepSection
              key={`${i}-${s.title}`}
              title={s.title}
              lines={s.lines}
              state={state}
              open={open}
              onToggle={(o) => setExpanded((e) => ({ ...e, [i]: o }))}
            />
          );
        })
      )}
      {task.log_file ? (
        <div>
          <Button size="xs" variant="ghost" onClick={() => revealPath(task.log_file!)}>
            <FolderOpenIcon data-icon="inline-start" />
            Show log file
          </Button>
        </div>
      ) : null}
    </div>
  );
}

function StepSection({
  title,
  lines,
  state,
  open,
  onToggle,
}: {
  title: string;
  lines: string[];
  state: StepState;
  open: boolean;
  onToggle: (open: boolean) => void;
}) {
  const body = useRef<HTMLPreElement>(null);
  // The live step keeps its newest line in view.
  useLayoutEffect(() => {
    if (state === "running" && body.current) body.current.scrollTop = body.current.scrollHeight;
  }, [lines.length, state]);

  return (
    <details
      open={open}
      onToggle={(e) => {
        if (e.currentTarget.open !== open) onToggle(e.currentTarget.open);
      }}
      className="group rounded-lg bg-well"
    >
      <summary className="flex cursor-pointer list-none items-center gap-2 rounded-lg px-3 py-2 text-[13px] outline-none select-none focus-visible:ring-3 focus-visible:ring-ring [&::-webkit-details-marker]:hidden">
        <CaretRightIcon className="size-3.5 shrink-0 text-muted-foreground transition-transform duration-150 group-open:rotate-90 motion-reduce:transition-none" />
        <StepIcon state={state} />
        <span className={cn("min-w-0 flex-1 truncate", state === "failed" && "text-destructive")}>{title}</span>
        <span className="shrink-0 text-xs text-muted-foreground tabular-nums">
          {lines.length === 0 ? "no output" : `${lines.length} line${lines.length === 1 ? "" : "s"}`}
        </span>
      </summary>
      {lines.length > 0 ? (
        <pre
          ref={body}
          tabIndex={0}
          aria-label={`${title} output`}
          className="mx-3 mb-3 max-h-64 overflow-auto rounded-md bg-background p-2.5 font-mono text-xs leading-relaxed break-all whitespace-pre-wrap text-muted-foreground outline-none focus-visible:ring-3 focus-visible:ring-ring"
        >
          {lines.map((line, i) =>
            line.startsWith("$ ") ? (
              <span key={i} className="block font-medium text-foreground">
                {line}
              </span>
            ) : (
              <span key={i} className="block">
                {line}
              </span>
            ),
          )}
        </pre>
      ) : null}
    </details>
  );
}

function StepIcon({ state }: { state: StepState }) {
  switch (state) {
    case "running":
      return <Spinner className="size-3.5 shrink-0 text-brand motion-reduce:animate-none" aria-label="Running" />;
    case "failed":
      return (
        <WarningCircleIcon weight="fill" className="size-3.5 shrink-0 text-destructive-solid" aria-label="Failed" />
      );
    case "cancelled":
      return <ProhibitIcon className="size-3.5 shrink-0 text-muted-foreground" aria-label="Cancelled" />;
    default:
      return <CheckCircleIcon weight="fill" className="size-3.5 shrink-0 text-success-solid" aria-label="Done" />;
  }
}
