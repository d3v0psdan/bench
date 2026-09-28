import { useEffect, useRef, useState } from "react";
import { useDaemonContext } from "@/hooks/daemon-provider";
import { api } from "@/lib/api";
import { DEFAULT_TIMEOUT_MS, errorMessage, withTimeout } from "@/lib/async";
import { DEFAULT_ANSWERS, stepsFor, type NewAppAnswers, type NewAppStep } from "@/lib/new-app-steps";

// Link, park and proxy re-apply every route server-side (up to a pool
// start), so they get a generous timeout. Creating an app answers at once
// with its task.
const SITE_TIMEOUT_MS = 60_000;

export type AddSiteKind = "new" | "link" | "park" | "proxy";

export interface Tool {
  name: string;
  path?: string;
  version?: string;
}

export type Step = "choose" | NewAppStep | "link" | "park" | "proxy";

/** useAddSiteFlow is the Add site dialog's state: the step the user is on,
 *  their answers, and what the steps need from the daemon. */
export function useAddSiteFlow(open: boolean, initialKind: AddSiteKind | null) {
  const { conn, sites } = useDaemonContext();
  const [kind, setKind] = useState<AddSiteKind | null>(null);
  const [index, setIndex] = useState(0);
  const [back, setBack] = useState(false);
  const [answers, setAnswers] = useState<NewAppAnswers>(DEFAULT_ANSWERS);
  const [projectsDir, setProjectsDir] = useState("");
  const [dir, setDir] = useState("");
  const [tools, setTools] = useState<Tool[] | null>(null);
  const [toolsError, setToolsError] = useState<string | null>(null);
  const [toolsAttempt, setToolsAttempt] = useState(0);
  const [linkPath, setLinkPath] = useState<string | null>(null);
  const [linkName, setLinkName] = useState("");
  const [proxyName, setProxyName] = useState("");
  const [proxyTarget, setProxyTarget] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Each opening starts over, at the requested kind if there is one.
  const wasOpen = useRef(false);
  useEffect(() => {
    if (open && !wasOpen.current) {
      setKind(initialKind);
      setIndex(initialKind ? 1 : 0);
      setBack(false);
      setAnswers(DEFAULT_ANSWERS);
      setLinkPath(null);
      setLinkName("");
      setProxyName("");
      setProxyTarget("");
      setError(null);
    }
    wasOpen.current = open;
  }, [open, initialKind]);

  // What the new-app steps need from the daemon: the projects folder and
  // which JavaScript tools are installed.
  useEffect(() => {
    if (!open || !conn) return;
    let cancelled = false;
    withTimeout(
      api<Record<string, string>>(conn, "GET", "/api/settings"),
      DEFAULT_TIMEOUT_MS,
      "Bench didn't answer in time",
    )
      .then((s) => {
        if (cancelled) return;
        setProjectsDir(s["projects.dir"] ?? "");
        setDir(s["projects.dir"] ?? "");
      })
      .catch((e: unknown) => !cancelled && setError(errorMessage(e)));
    return () => {
      cancelled = true;
    };
  }, [open, conn]);

  useEffect(() => {
    if (!open || !conn) return;
    let cancelled = false;
    setToolsError(null);
    withTimeout(api<Tool[]>(conn, "GET", "/api/tools"), DEFAULT_TIMEOUT_MS, "Bench didn't check for Node in time")
      .then((t) => {
        if (cancelled) return;
        setTools(t);
        const has = (n: string) => t.some((x) => x.name === n && x.path);
        setAnswers((a) => ({ ...a, packageManager: has("npm") ? "npm" : has("bun") ? "bun" : "" }));
      })
      .catch((e: unknown) => !cancelled && setToolsError(errorMessage(e)));
    return () => {
      cancelled = true;
    };
  }, [open, conn, toolsAttempt]);

  const steps: Step[] =
    kind === null ? ["choose"] : kind === "new" ? ["choose", ...stepsFor(answers)] : ["choose", kind];
  const step = steps[Math.min(index, steps.length - 1)];
  const taken = (sites ?? []).map((s) => s.name);
  const set = (patch: Partial<NewAppAnswers>) => setAnswers((a) => ({ ...a, ...patch }));

  const go = (to: number) => {
    setBack(to < index);
    setError(null);
    setIndex(to);
  };
  const choose = (k: AddSiteKind) => {
    setKind(k);
    setBack(false);
    setError(null);
    setIndex(1);
  };

  const post = <T,>(path: string, body: unknown, ms = SITE_TIMEOUT_MS) => {
    if (!conn) throw new Error("Bench isn't connected");
    return withTimeout(api<T>(conn, "POST", path, body), ms, "Bench didn't answer in time");
  };

  const run = async (fn: () => Promise<void>) => {
    if (submitting) return;
    setSubmitting(true);
    setError(null);
    try {
      await fn();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setSubmitting(false);
    }
  };

  return {
    kind,
    index,
    back,
    answers,
    set,
    projectsDir,
    dir,
    setDir,
    tools,
    toolsError,
    retryTools: () => setToolsAttempt((n) => n + 1),
    linkPath,
    setLinkPath,
    linkName,
    setLinkName,
    proxyName,
    setProxyName,
    proxyTarget,
    setProxyTarget,
    submitting,
    error,
    steps,
    step,
    taken,
    go,
    choose,
    post,
    run,
  };
}
