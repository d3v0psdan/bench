import { createContext, useContext, useEffect, useRef, useState, type ReactNode } from "react";
import { useDaemonContext } from "@/hooks/daemon-provider";
import { api, type Check } from "@/lib/api";
import { errorMessage, withTimeout } from "@/lib/async";

// The DNS probe alone may take 3s; leave room for the port scans.
const DOCTOR_TIMEOUT_MS = 15_000;
// Service and site changes arrive in bursts; check once they settle.
const RERUN_DEBOUNCE_MS = 1_500;
// Coming back to the window re-checks (quitting Herd happens elsewhere),
// but not more often than this.
const FOCUS_RERUN_MIN_MS = 10_000;

export interface Doctor {
  checks: Check[] | null;
  error: string | null;
  running: boolean;
  rerun: () => void;
  byId: (id: string) => Check | undefined;
}

const DoctorContext = createContext<Doctor | null>(null);

/** DoctorProvider runs the daemon's diagnostics once for the whole app
 *  (docs/ui-standard.md F3): on connect, after setup, when services or
 *  sites change, when the window regains focus, and on demand. */
export function DoctorProvider({ children }: { children: ReactNode }) {
  const { conn, setupState, services, sites } = useDaemonContext();
  const [checks, setChecks] = useState<Check[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [running, setRunning] = useState(false);
  const [runKey, setRunKey] = useState(0);
  const lastRun = useRef(0);

  useEffect(() => {
    if (!conn) return;
    let stale = false;
    lastRun.current = Date.now();
    setRunning(true);
    withTimeout(api<Check[]>(conn, "GET", "/api/doctor"), DOCTOR_TIMEOUT_MS, "Diagnostics didn't finish in time")
      .then((c) => {
        if (stale) return;
        setChecks(c);
        setError(null);
      })
      .catch((e) => !stale && setError(errorMessage(e)))
      .finally(() => !stale && setRunning(false));
    return () => {
      stale = true;
    };
  }, [conn, runKey, setupState === "done"]);

  const servicesKey = services?.map((s) => `${s.name}:${s.state}:${s.port}`).join(",");
  const sitesKey = sites?.map((s) => `${s.name}:${s.error ?? ""}`).join(",");
  useEffect(() => {
    if (servicesKey === undefined && sitesKey === undefined) return;
    const timer = window.setTimeout(() => setRunKey((k) => k + 1), RERUN_DEBOUNCE_MS);
    return () => window.clearTimeout(timer);
  }, [servicesKey, sitesKey]);

  useEffect(() => {
    const onFocus = () => {
      if (Date.now() - lastRun.current >= FOCUS_RERUN_MIN_MS) setRunKey((k) => k + 1);
    };
    window.addEventListener("focus", onFocus);
    return () => window.removeEventListener("focus", onFocus);
  }, []);

  const value: Doctor = {
    checks,
    error,
    running,
    rerun: () => setRunKey((k) => k + 1),
    byId: (id) => checks?.find((c) => c.id === id),
  };
  return <DoctorContext.Provider value={value}>{children}</DoctorContext.Provider>;
}

/** useDoctor reads the shared diagnostics. */
export function useDoctor(): Doctor {
  const ctx = useContext(DoctorContext);
  if (!ctx) throw new Error("useDoctor outside DoctorProvider");
  return ctx;
}

export interface Health {
  /** Checks that stop Bench from doing its job: failures, and setup not done. */
  issues: Check[];
  /** The checks couldn't run; nothing is known, which is never "healthy". */
  error: string | null;
  /** Whether Bench can serve sites (the HTTPS port check); null while checking. */
  canServe: boolean | null;
  /** Whether the HTTPS and DNS setup still has to run; null while checking. */
  needsSetup: boolean | null;
  /** The Laravel Herd check when Herd is running, so it's explained once. */
  herd?: Check;
  loading: boolean;
}

const SETUP_CHECKS = new Set(["dns", "trust"]);

/** useHealth summarizes the checks into what the UI needs to tell the truth
 *  about the machine (ui-standard F4): the pill, banners, site actions. */
export function useHealth(): Health {
  const { checks, error } = useDoctor();
  return deriveHealth(checks, error);
}

/** deriveHealth is useHealth's pure core (tested in use-doctor.test.ts). */
export function deriveHealth(checks: Check[] | null, error: string | null): Health {
  const loading = checks === null && !error;
  const issues = (checks ?? []).filter((c) => c.status === "fail" || (c.status === "warn" && SETUP_CHECKS.has(c.id)));
  const https = checks?.find((c) => c.id === "https");
  const setup = checks?.filter((c) => SETUP_CHECKS.has(c.id)) ?? [];
  const herd = checks?.find((c) => c.id === "herd" && c.status !== "ok");
  return {
    issues,
    error: checks === null ? error : null,
    canServe: https ? https.status === "ok" : null,
    needsSetup: setup.length ? setup.some((c) => c.status !== "ok") : null,
    herd,
    loading,
  };
}
