import { useEffect, useState } from "react";
import { fetch } from "@tauri-apps/plugin-http";
import { LetterTile } from "@/components/page";
import { useDaemonContext } from "@/hooks/daemon-provider";
import { DEFAULT_TIMEOUT_MS } from "@/lib/async";
import { MASK, usePrivacy } from "@/lib/privacy";
import { cn } from "@/lib/utils";

/** SiteIcon is the site's own favicon (from its public folder, served by
 *  the daemon), or its letter tile when it has none. The icon is fetched
 *  with the token in a header and shown from a blob URL, so the daemon
 *  token never sits in the page as part of an image URL. */
export function SiteIcon({ name, size = "sm" }: { name: string; size?: "sm" | "lg" }) {
  const { conn } = useDaemonContext();
  const [src, setSrc] = useState<string | null>(null);
  const privacy = usePrivacy();

  useEffect(() => {
    setSrc(null);
    if (!conn) return;
    let url: string | null = null;
    let cancelled = false;
    fetch(`http://${conn.addr}/api/sites/${encodeURIComponent(name)}/icon`, {
      headers: { Authorization: `Bearer ${conn.token}` },
      signal: AbortSignal.timeout(DEFAULT_TIMEOUT_MS),
    })
      // plugin-http's Response carries no content type into blob(), and an
      // <img> won't render an SVG blob without one, so it is set here.
      .then(async (resp) =>
        resp.ok ? new Blob([await resp.arrayBuffer()], { type: resp.headers.get("Content-Type") ?? "" }) : null,
      )
      .then((blob) => {
        if (!blob || cancelled) return;
        url = URL.createObjectURL(blob);
        setSrc(url);
      })
      // No icon, or the daemon is restarting: the letter tile stays, and
      // the next connection (a new conn) tries again.
      .catch(() => {});
    return () => {
      cancelled = true;
      if (url) URL.revokeObjectURL(url);
    };
  }, [conn, name]);

  // A favicon or an initial can give a hidden site name away.
  if (privacy.names) return <LetterTile name={MASK} size={size} />;
  if (!src) return <LetterTile name={name} size={size} />;
  return (
    <img
      src={src}
      alt=""
      aria-hidden
      draggable={false}
      // An icon the webview can't decode falls back to the letter tile.
      onError={() => setSrc(null)}
      className={cn(
        "shrink-0 rounded-md bg-card object-contain ring-1 ring-border ring-inset",
        size === "lg" ? "size-10 p-1.5" : "size-5 p-0.5",
      )}
    />
  );
}
