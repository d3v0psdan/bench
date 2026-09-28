import { fetch } from "@tauri-apps/plugin-http";

// Wire types mirroring daemon/internal/api (api.go).

export interface Conn {
  addr: string;
  token: string;
}

export interface Site {
  name: string;
  host: string;
  url: string;
  path?: string;
  kind: "linked" | "parked" | "proxy";
  php?: string;
  php_pinned?: string;
  proxy_to?: string;
  error?: string;
  favorite?: boolean;
  /** The parked directory a parked site comes from. */
  parked_in?: string;
  /** APP_NAME from the site's .env: the inbox its mail files under. */
  app_name?: string;
  /** Local service ports the site's .env points at (database, Redis, mail...). */
  env_ports?: number[];
  /** A linked site's folder was moved or deleted. */
  path_missing?: boolean;
}

export interface BinaryInfo {
  /** Bytes on disk when installed. */
  size?: number;
  name: string;
  channel: string;
  version: string;
  installed: boolean;
  /** Where the build lives, or will live once installed. */
  dir: string;
  /** Hosts it downloads from; every download is checksummed. */
  sources: string[];
}

/** A long-running daemon operation (GET /api/tasks, "tasks" events). */
export interface Task {
  id: string;
  kind: "binary.install" | "service.create" | "service.clone" | "setup" | "site.new" | "site.delete" | string;
  title: string;
  /** "php/8.4" for installs, the instance name for services. */
  target: string;
  phase?: string;
  state: "running" | "done" | "failed" | "cancelled";
  cancellable: boolean;
  download?: DownloadProgress;
  /** The task's own output, when it keeps one (GET /api/tasks/{id}/log). */
  log_file?: string;
  error?: string;
  started_at: string;
  ended_at?: string;
}

/** GET /api/sites/{name}/delete: what deleting a site can clean up. An
 *  absent field doesn't apply to this site. */
export interface SiteDeletePlan {
  site: Site;
  folder?: string;
  database?: { service: string; name: string };
  /** An instance made for this site alone. */
  service?: string;
  mail_tag?: string;
  log_file?: string;
}

/** POST /api/sites/{name}/delete: which parts of the plan to remove. */
export interface SiteDeleteRequest {
  folder: boolean;
  database: boolean;
  service: boolean;
  mail: boolean;
  log: boolean;
}

export interface DownloadProgress {
  name: string;
  version: string;
  phase: "downloading" | "extracting" | "done" | "error";
  file: number;
  files: number;
  received: number;
  total: number;
  error?: string;
}

export interface DaemonStatus {
  version: string;
  os: string;
  arch: string;
  pid: number;
  started_at: string;
  uptime_seconds: number;
}

export interface Service {
  name: string;
  service: string; // mysql | mariadb | postgresql | valkey | meilisearch | rustfs | mailpit
  channel: string;
  version: string;
  port: number;
  ports?: Record<string, number>; // secondary ports by role (http, console)
  /** Supervisor state (starting | running | backoff | stopped | failed) or
   *  a lifecycle phase (installing | initializing | cloning | stopping |
   *  deleting). */
  state: string;
  pid?: number;
  autostart: boolean;
  data_dir: string;
  /** The process output; GET /api/services/{name}/logs serves it. */
  log_file: string;
  /** Client connection URL (databases). */
  url?: string;
  /** The bundled terminal client's command line (databases). */
  client?: string;
  /** Web console (RustFS, Meilisearch) and the login it asks for. */
  console_url?: string;
  console_login?: Array<{ label: string; value: string; secret: boolean }>;
  env: string[];
  notice?: string;
  error?: string;
}

/** One `bench doctor` diagnostic (GET /api/doctor). */
export interface Check {
  id: string;
  title: string;
  status: "ok" | "warn" | "fail";
  /** What's wrong, in a sentence or two. */
  detail: string;
  /** The fix, in order, one action each. Backticked spans are commands. */
  steps?: string[];
  /** The button the fix asks for: run the setup, or check again. */
  action?: "setup" | "recheck";
}

export interface ServiceType {
  service: string;
  label: string;
  channels: string[];
  default_port: number;
  singleton?: boolean;
}

/** Mailpit's message summary (relayed verbatim through /api/mail/v1). */
export interface MailSummary {
  ID: string;
  Read: boolean;
  From: { Name: string; Address: string } | null;
  To: { Name: string; Address: string }[] | null;
  Subject: string;
  Created: string;
  Tags: string[];
  Snippet: string;
}

export interface MailList {
  total: number;
  unread: number;
  messages_count: number;
  /** unread among the messages this list or search matched */
  messages_unread: number;
  tags: string[];
  messages: MailSummary[];
}

export type MailAddress = { Name: string; Address: string };

export interface MailMessage extends MailSummary {
  HTML: string;
  Text: string;
  Date: string;
  MessageID?: string;
  Cc?: MailAddress[] | null;
  ReplyTo?: MailAddress[] | null;
  Attachments?: MailAttachment[] | null;
}

export interface MailAttachment {
  PartID: string;
  FileName: string;
  ContentType: string;
  Size: number;
}

export interface DaemonEvent {
  type: "status" | "download" | "sites" | "process" | "services" | "tasks" | "mail";
  status?: DaemonStatus;
  download?: DownloadProgress;
  sites?: Site[];
  services?: Service[];
  tasks?: Task[];
  mail?: { Type: string };
}

/** api performs an authenticated request against benchd. Errors carry the
 *  daemon's plain-text message so the UI can render the real cause. */
export async function api<T>(
  conn: Conn,
  method: "GET" | "POST" | "PUT" | "DELETE",
  path: string,
  body?: unknown,
): Promise<T> {
  const resp = await fetch(`http://${conn.addr}${path}`, {
    method,
    headers: {
      Authorization: `Bearer ${conn.token}`,
      ...(body !== undefined ? { "Content-Type": "application/json" } : {}),
    },
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  if (!resp.ok) {
    const text = (await resp.text()).trim();
    throw new Error(text || `Bench answered ${resp.status}`);
  }
  // Some endpoints answer 2xx with no body (204) or plain text (Mailpit's
  // "ok" for deletes): only JSON is parsed; T is then undefined.
  if (!(resp.headers.get("Content-Type") ?? "").includes("application/json")) {
    return undefined as T;
  }
  // The daemon's JSON is trusted to match T; json() itself is untyped.
  return resp.json() as Promise<T>;
}

export function formatBytes(n: number): string {
  // Non-breaking space between value and unit.
  if (n >= 1 << 20) return `${(n / (1 << 20)).toFixed(1)} MB`;
  if (n >= 1 << 10) return `${Math.round(n / (1 << 10))} KB`;
  return `${n} B`;
}

/** apiText is api for endpoints that answer plain text (Mailpit's raw
 *  message source). */
export async function apiText(conn: Conn, path: string): Promise<string> {
  const resp = await fetch(`http://${conn.addr}${path}`, { headers: { Authorization: `Bearer ${conn.token}` } });
  const text = await resp.text();
  if (!resp.ok) throw new Error(text.trim() || `Bench answered ${resp.status}`);
  return text;
}
