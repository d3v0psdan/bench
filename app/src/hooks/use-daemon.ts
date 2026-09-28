import { useEffect, useRef, useState } from "react";
import { invoke } from "@tauri-apps/api/core";
import { fetch } from "@tauri-apps/plugin-http";
import {
  api,
  type BinaryInfo,
  type Conn,
  type DaemonEvent,
  type DaemonStatus,
  type Service,
  type Site,
  type Task,
} from "@/lib/api";
import { toast } from "sonner";
import { DEFAULT_TIMEOUT_MS, errorMessage, withTimeout } from "@/lib/async";
import { startDaemonOnOpen } from "@/lib/prefs";

export type { DaemonStatus } from "@/lib/api";

interface DaemonConfig {
  addr: string | null;
  token: string | null;
  defaultAddr: string;
}

const RETRY_MS = 2000;
// How many retries a dropped connection gets before the UI says Bench is
// stopped (about 16 seconds: a daemon restart or a resume from sleep).
const RECONNECT_ATTEMPTS = 8;

const PENDING_TIMEOUT_MS = 15_000;
// A shutdown request that hasn't answered by now is treated as failed.
const STOP_TIMEOUT_MS = 10_000;

// Long enough for a running daemon to answer the first connect.
const AUTO_START_DELAY_MS = 2_500;

export function useDaemon() {
  const [connected, setConnected] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // In-flight start/stop: drives the button spinner until the WS confirms
  // the outcome (or the timeout/an error resolves it).
  const [pending, setPending] = useState<"start" | "stop" | null>(null);
  const pendingTimer = useRef<number | undefined>(undefined);
  // Mirrors `connected` for callbacks that must not close over stale state.
  const connectedRef = useRef(false);
  // A live connection dropped and is being retried: the UI stays up
  // (audit UI-27) until RECONNECT_ATTEMPTS fail, then shows the start
  // screen. A stop the user asked for skips straight to stopped.
  const [reconnecting, setReconnecting] = useState(false);
  const failedRetries = useRef(0);
  const liveRef = useRef(false); // the socket is up right now
  const stoppingRef = useRef(false);
  // The daemon's address is known but its token can't be read: not the
  // same as stopped, and starting again won't fix it.
  const [tokenError, setTokenError] = useState<string | null>(null);

  const resolvePending = () => {
    window.clearTimeout(pendingTimer.current);
    setPending(null);
  };

  const beginPending = (kind: "start" | "stop") => {
    setError(null);
    setPending(kind);
    stoppingRef.current = kind === "stop";
    window.clearTimeout(pendingTimer.current);
    pendingTimer.current = window.setTimeout(() => {
      setPending(null);
      setError(
        `Bench didn't ${kind} in time. Its log may say why.`,
      );
    }, PENDING_TIMEOUT_MS);
  };
  const [heartbeat, setHeartbeat] = useState<{
    status: DaemonStatus;
    at: number;
  } | null>(null);
  const [uptime, setUptime] = useState(0);
  // Live data pushed over the WS (null = not loaded yet → skeletons).
  const [sites, setSites] = useState<Site[] | null>(null);
  const [binaries, setBinaries] = useState<BinaryInfo[] | null>(null);
  const [defaultPhp, setDefaultPhp] = useState<string | null>(null);
  const [services, setServices] = useState<Service[] | null>(null);
  // Running and recent long operations, straight from the daemon.
  const [tasks, setTasks] = useState<Task[] | null>(null);
  // Initial-load failure (null = fine); reloadData retries it.
  const [loadError, setLoadError] = useState<string | null>(null);
  const [reloadKey, setReloadKey] = useState(0);
  // Bumped by each WS sites/services event: an initial REST snapshot that
  // was requested before the newest event is older, so it's discarded.
  const sitesEpoch = useRef(0);
  const servicesEpoch = useRef(0);
  const tasksEpoch = useRef(0);
  // Task ids already seen finished, so each one refreshes binaries once.
  const doneTasks = useRef(new Set<string>());
  // Bumped on every relayed Mailpit event; the Mail pane refetches on it.
  const [mailVersion, setMailVersion] = useState(0);
  // Counts messages that arrived while connected (OS notifications).
  const [newMail, setNewMail] = useState(0);
  // Connection details for API calls, as state so panes re-render with it.
  const [conn, setConn] = useState<Conn | null>(null);
  // Last addr/token that produced a live connection (used by stop()).
  const connRef = useRef<{ addr: string; token: string } | null>(null);
  // Freshest heartbeat for the uptime ticker, without re-arming its interval.
  const heartbeatRef = useRef<{ status: DaemonStatus; at: number } | null>(
    null,
  );

  useEffect(() => {
    let ws: WebSocket | null = null;
    let timer: number | undefined;
    let disposed = false;

    const retry = () => {
      liveRef.current = false;
      if (connectedRef.current && !stoppingRef.current && failedRetries.current < RECONNECT_ATTEMPTS) {
        failedRetries.current++;
        setReconnecting(true);
      } else {
        connectedRef.current = false;
        failedRetries.current = 0;
        setConnected(false);
        setReconnecting(false);
      }
      timer = window.setTimeout(connect, RETRY_MS);
    };

    // Always re-read config before connecting: the addr can change
    // between daemon runs.
    const connect = async () => {
      let cfg: DaemonConfig | null = null;
      try {
        cfg = await invoke<DaemonConfig>("daemon_config");
      } catch {
        // config unreadable: treat as stopped, retry below
      }
      if (disposed) return;
      setTokenError(
        cfg?.addr && !cfg.token
          ? "Bench is running, but this app can't read its access token (the token file in .bench, in your home folder). Check that the file exists and that your user can read it."
          : null,
      );
      if (!cfg?.addr || !cfg.token) {
        retry();
        return;
      }
      connRef.current = { addr: cfg.addr, token: cfg.token };
      ws = new WebSocket(
        `ws://${cfg.addr}/api/events?token=${encodeURIComponent(cfg.token)}`,
      );
      ws.onmessage = (e) => {
        let msg: DaemonEvent;
        try {
          msg = JSON.parse(e.data);
        } catch {
          return; // ignore malformed frames
        }
        switch (msg.type) {
          case "status": {
            // A new live socket (first connect or a reconnect, possibly to
            // a restarted daemon on a new port) gets a fresh conn, which
            // reloads the data.
            if (!liveRef.current) {
              setConn(connRef.current);
            }
            liveRef.current = true;
            failedRetries.current = 0;
            stoppingRef.current = false;
            setReconnecting(false);
            connectedRef.current = true;
            setConnected(true);
            setError(null); // a live heartbeat supersedes any stale error
            const hb = { status: msg.status!, at: Date.now() };
            heartbeatRef.current = hb;
            setHeartbeat(hb);
            break;
          }
          case "sites":
            sitesEpoch.current++;
            setSites(msg.sites ?? []);
            break;
          case "services":
            servicesEpoch.current++;
            setServices(msg.services ?? []);
            break;
          case "tasks": {
            tasksEpoch.current++;
            const list = msg.tasks ?? [];
            setTasks(list);
            // A finished install or create changes what's installed.
            const finished = list.filter((t) => t.state === "done" && !doneTasks.current.has(t.id));
            finished.forEach((t) => doneTasks.current.add(t.id));
            if (finished.length > 0) void refreshBinaries();
            break;
          }
          case "mail":
            setMailVersion((v) => v + 1);
            if (msg.mail?.Type === "new") setNewMail((n) => n + 1);
            break;
        }
      };
      // A failed connection attempt also fires close, so this covers
      // both error and clean-shutdown paths.
      ws.onclose = () => {
        if (!disposed) retry();
      };
    };

    connect();
    return () => {
      disposed = true;
      window.clearTimeout(timer);
      if (ws) {
        ws.onclose = null;
        ws.close();
      }
    };
  }, []);

  // The WS is the source of truth for start/stop outcomes: a heartbeat
  // resolves a pending start, losing the connection resolves a pending stop.
  useEffect(() => {
    if ((pending === "start" && connected) || (pending === "stop" && !connected)) {
      resolvePending();
    }
  }, [connected, pending]);

  // Initial data load per connection: sites, catalog, settings, services.
  // Live updates come over the WS afterwards. Each result applies on its
  // own; a failure is kept for the panes to show with a retry.
  useEffect(() => {
    if (!connected || !conn) return;
    let stale = false;
    const epochs = { sites: sitesEpoch.current, services: servicesEpoch.current, tasks: tasksEpoch.current };
    setLoadError(null);
    const load = <T,>(path: string) => withTimeout(api<T>(conn, "GET", path), DEFAULT_TIMEOUT_MS, "Bench didn't answer in time");
    void Promise.allSettled([
      load<Site[]>("/api/sites"),
      load<BinaryInfo[]>("/api/binaries"),
      load<Record<string, string>>("/api/settings"),
      load<Service[]>("/api/services"),
      load<Task[]>("/api/tasks"),
    ]).then(([s, b, settings, svcs, tsks]) => {
      if (stale) return;
      if (s.status === "fulfilled" && sitesEpoch.current === epochs.sites) setSites(s.value);
      if (b.status === "fulfilled") setBinaries(b.value);
      if (settings.status === "fulfilled") setDefaultPhp(settings.value["php.default"] ?? null);
      if (svcs.status === "fulfilled" && servicesEpoch.current === epochs.services) setServices(svcs.value);
      if (tsks.status === "fulfilled" && tasksEpoch.current === epochs.tasks) setTasks(tsks.value);
      const failed = [s, b, settings, svcs, tsks].find((r) => r.status === "rejected");
      if (failed?.status === "rejected") {
        setLoadError(errorMessage(failed.reason));
      }
    });
    return () => {
      stale = true;
    };
  }, [connected, conn, reloadKey]);

  /** reloadData retries the initial load after a failure. */
  const reloadData = () => setReloadKey((k) => k + 1);

  // One-time HTTPS-trust + .test-DNS setup (elevation prompt happens in
  // the daemon-spawned helper). idle → running → done, or an error string.
  const [setupState, setSetupState] = useState<
    "idle" | "running" | "done" | { error: string }
  >("idle");

  const runSetup = () => {
    const c = connRef.current;
    if (!c || setupState === "running") return;
    setSetupState("running");
    // No client timeout: the user may sit on the OS elevation prompt.
    fetch(`http://${c.addr}/api/setup`, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${c.token}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ dns: true, trust: true }),
    })
      .then(async (r) => {
        if (!r.ok) throw new Error((await r.text()).trim() || `HTTP ${r.status}`);
        setSetupState("done");
      })
      .catch((e) => setSetupState({ error: e instanceof Error ? e.message : String(e) }));
  };

  /** refreshBinaries re-reads the catalog (after an install completes). */
  const refreshBinaries = () => {
    const c = connRef.current;
    if (!c) return Promise.resolve();
    return withTimeout(api<BinaryInfo[]>(c, "GET", "/api/binaries"), DEFAULT_TIMEOUT_MS, "Bench didn't answer in time")
      .then(setBinaries)
      .catch((e: unknown) =>
        toast.error("Couldn't refresh the installed versions", { description: errorMessage(e) }),
      );
  };

  useEffect(() => () => window.clearTimeout(pendingTimer.current), []);

  // Opening the app brings sites back: if no daemon answers shortly after
  // launch, start one (unless the user turned that off in Settings).
  const autoStarted = useRef(false);
  useEffect(() => {
    const timer = window.setTimeout(() => {
      if (connectedRef.current || autoStarted.current || !startDaemonOnOpen()) return;
      autoStarted.current = true;
      startRef.current();
    }, AUTO_START_DELAY_MS);
    return () => window.clearTimeout(timer);
  }, []);

  // Tick uptime locally every second instead of waiting for the next 2s
  // event. One steady interval keyed on `connected` (re-arming it per
  // heartbeat makes the seconds visibly skip/repeat); the freshest
  // heartbeat comes in via ref.
  useEffect(() => {
    if (!connected) return;
    const calc = () => {
      const hb = heartbeatRef.current;
      if (!hb) return;
      setUptime(
        hb.status.uptime_seconds + Math.floor((Date.now() - hb.at) / 1000),
      );
    };
    calc();
    const id = window.setInterval(calc, 1000);
    return () => window.clearInterval(id);
  }, [connected]);

  const start = () => {
    beginPending("start");
    // the reconnect loop picks the new daemon up on success; beginPending's
    // timer already bounds a hung invoke, so it takes no withTimeout too
    invoke("start_daemon").catch((e) => {
      resolvePending();
      setError(`${e}. Bench's log may say why.`);
    });
  };

  const startRef = useRef(start);
  startRef.current = start;

  const stop = () => {
    const c = connRef.current;
    if (!c) return;
    beginPending("stop");
    fetch(`http://${c.addr}/api/shutdown`, {
      method: "POST",
      headers: { Authorization: `Bearer ${c.token}` },
      signal: AbortSignal.timeout(STOP_TIMEOUT_MS),
    }).catch((e) => {
      // A rejected shutdown request is expected collateral of a successful
      // shutdown (the daemon can drop the connection before the response
      // flushes). Only report it if we're still connected shortly after.
      window.setTimeout(() => {
        if (connectedRef.current) {
          resolvePending();
          setError(`${e}. Bench may still be running.`);
        }
      }, 2000);
    });
  };

  return {
    connected,
    reconnecting,
    tokenError,
    status: heartbeat?.status ?? null,
    uptime,
    error,
    pending,
    start,
    stop,
    conn,
    sites,
    setSites,
    binaries,
    refreshBinaries,
    defaultPhp,
    setDefaultPhp,
    services,
    tasks,
    loadError,
    reloadData,
    mailVersion,
    newMail,
    setupState,
    runSetup,
  };
}

export type Daemon = ReturnType<typeof useDaemon>;
