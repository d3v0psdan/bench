import { currentPrivacy } from "@/lib/privacy";

/** DEFAULT_TIMEOUT_MS bounds a normal request. Installs, creates and site
 *  operations that re-apply routes keep their own, longer timeouts. */
export const DEFAULT_TIMEOUT_MS = 15_000;

/** errorMessage renders any thrown value for display near its control,
 *  with what privacy mode hides (hosts, user paths) masked. */
export function errorMessage(e: unknown): string {
  return currentPrivacy().text(e instanceof Error ? e.message : String(e));
}

/** withTimeout rejects with message if p hasn't settled in ms, so a
 *  pending state can never spin forever (the request itself keeps going
 *  daemon-side; the WS stream reports its eventual outcome). */
export function withTimeout<T>(p: Promise<T>, ms: number, message: string): Promise<T> {
  let timer: number | undefined;
  const timeout = new Promise<never>((_, reject) => {
    timer = window.setTimeout(() => reject(new Error(message)), ms);
  });
  return Promise.race([p, timeout]).finally(() => window.clearTimeout(timer));
}
