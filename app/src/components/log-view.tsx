import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { FolderOpenIcon, MagnifyingGlassIcon } from "@phosphor-icons/react";
import { Button } from "@/components/ui/button";
import { Field, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { LoadFailed } from "@/components/load-failed";
import { useDaemonContext } from "@/hooks/daemon-provider";
import { api } from "@/lib/api";
import { errorMessage, withTimeout } from "@/lib/async";
import { appendLog, type LogChunk, type LogTail } from "@/lib/log-text";
import { revealPath } from "@/lib/open";

const TAIL_TIMEOUT_MS = 10_000;

/** useLogStream reads a daemon log endpoint (a LogTail at `endpoint`, a
 *  follow socket at `endpoint/follow`): the tail, then appended text while
 *  `follow` is on. `retry` re-reads the tail and restarts following. */
export function useLogStream(endpoint: string, follow: boolean, tailLines?: number) {
  const { conn } = useDaemonContext();
  const [tail, setTail] = useState<LogTail | null>(null);
  const [text, setText] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [followError, setFollowError] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    if (!conn) return;
    let cancelled = false;
    setError(null);
    withTimeout(api<LogTail>(conn, "GET", tailLines ? `${endpoint}?tail=${tailLines}` : endpoint), TAIL_TIMEOUT_MS, "Bench didn't send the log in time")
      .then((t) => {
        if (cancelled) return;
        setTail(t);
        setText(t.text);
      })
      .catch((e: unknown) => !cancelled && setError(errorMessage(e)));
    return () => {
      cancelled = true;
    };
  }, [conn, endpoint, attempt, tailLines]);

  // Follow from where the tail ended, so no line is missed or repeated.
  // Every fresh tail starts a fresh socket; turning Follow back on
  // re-reads the tail rather than guessing what was missed.
  useEffect(() => {
    if (!conn || !follow || tail === null) return;
    setFollowError(null);
    const ws = new WebSocket(
      `ws://${conn.addr}${endpoint}/follow?from=${tail.size}&token=${encodeURIComponent(conn.token)}`,
    );
    let closedByUs = false;
    ws.onmessage = (e: MessageEvent<string>) => {
      if (closedByUs) return; // a frame already queued when we moved on
      let chunk: LogChunk;
      try {
        chunk = JSON.parse(e.data) as LogChunk; // the daemon's LogChunk shape
      } catch {
        return; // ignore malformed frames
      }
      setText((shown) => appendLog(shown, chunk));
    };
    ws.onclose = () => {
      if (!closedByUs) setFollowError("Live updates stopped.");
    };
    return () => {
      closedByUs = true;
      ws.close();
    };
  }, [conn, endpoint, follow, tail]);

  const retry = useCallback(() => setAttempt((n) => n + 1), []);
  return { tail, text, error, followError, retry };
}

/** LogView shows the end of a daemon-managed log, follows it live, and
 *  filters it. `endpoint` answers GET with a LogTail and has a `/follow`
 *  socket (e.g. /api/services/mysql/logs); `emptyText` says when output
 *  will appear. */
export function LogView({ endpoint, emptyText }: { endpoint: string; emptyText: string }) {
  const [follow, setFollow] = useState(true);
  const [query, setQuery] = useState("");
  const { tail, text, error, followError, retry } = useLogStream(endpoint, follow);
  const scroller = useRef<HTMLPreElement>(null);
  const pinned = useRef(true); // stay at the bottom unless the user scrolled up

  useLayoutEffect(() => {
    const el = scroller.current;
    if (el && pinned.current) el.scrollTop = el.scrollHeight;
  }, [text, query]);

  if (error) {
    return <LoadFailed title="Couldn't load the log" message={error} onRetry={retry} />;
  }
  if (tail === null) {
    return <Skeleton className="h-48 w-full rounded-lg" aria-busy />;
  }

  const needle = query.trim().toLowerCase();
  const shown = needle
    ? text
        .split("\n")
        .filter((line) => line.toLowerCase().includes(needle))
        .join("\n")
    : text;

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-2">
        <div className="relative flex-1">
          <MagnifyingGlassIcon className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Filter lines, e.g. error"
            aria-label="Filter log lines"
            className="h-7 pl-8 text-xs md:text-xs"
          />
        </div>
        <Field orientation="horizontal" className="w-auto shrink-0">
          <Switch
            id={`follow-${endpoint}`}
            size="sm"
            checked={follow}
            onCheckedChange={(on) => {
              setFollow(on);
              if (on) retry();
            }}
          />
          <FieldLabel htmlFor={`follow-${endpoint}`} className="text-[13px] font-normal text-muted-foreground">
            Follow
          </FieldLabel>
        </Field>
        <Button size="xs" variant="outline" onClick={() => revealPath(tail.path)}>
          <FolderOpenIcon data-icon="inline-start" />
          Show log file
        </Button>
      </div>
      {followError && follow ? (
        <p role="alert" className="flex items-center gap-2 text-xs text-destructive">
          {followError}
          <Button size="xs" variant="outline" onClick={retry}>
            Try again
          </Button>
        </p>
      ) : null}
      {text === "" ? (
        <p className="rounded-lg bg-well p-3 text-xs text-muted-foreground">{emptyText}</p>
      ) : (
        <pre
          ref={scroller}
          tabIndex={0}
          aria-label="Log output"
          onScroll={(e) => {
            const el = e.currentTarget;
            pinned.current = el.scrollHeight - el.scrollTop - el.clientHeight < 24;
          }}
          className="max-h-96 overflow-auto rounded-lg bg-well p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap break-all outline-none focus-visible:ring-3 focus-visible:ring-ring"
        >
          {shown || "No lines match the filter."}
        </pre>
      )}
    </div>
  );
}
