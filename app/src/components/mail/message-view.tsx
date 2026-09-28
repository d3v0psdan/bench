import { useEffect, useState } from "react";
import { ArrowLeftIcon, PaperclipIcon, TrashIcon } from "@phosphor-icons/react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { ConfirmAction } from "@/components/confirm-action";
import { CopyButton } from "@/components/copy-button";
import { api, apiText, formatBytes, type Conn, type MailAddress, type MailAttachment, type MailMessage } from "@/lib/api";
import { DEFAULT_TIMEOUT_MS, errorMessage, withTimeout } from "@/lib/async";
import { openExternal, revealPath } from "@/lib/open";
import { MASK, usePrivacy } from "@/lib/privacy";

export function MessageView({ conn, id, onBack }: { conn: Conn | null; id: string; onBack: () => void }) {
  const [message, setMessage] = useState<MailMessage | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [retry, setRetry] = useState(0);
  const privacy = usePrivacy();

  useEffect(() => {
    if (!conn) return;
    let stale = false;
    setMessage(null);
    setError(null);
    withTimeout(
      api<MailMessage>(conn, "GET", `/api/mail/v1/message/${encodeURIComponent(id)}`),
      DEFAULT_TIMEOUT_MS,
      "Mail didn't answer in time",
    )
      .then((m) => !stale && setMessage(m))
      .catch((e) => !stale && setError(errorMessage(e)));
    return () => {
      stale = true;
    };
  }, [conn, id, retry]);

  if (error) {
    return (
      <div className="flex flex-col items-start gap-3 p-4">
        <Button variant="ghost" size="sm" className="-ml-2 @xl:hidden" onClick={onBack}>
          <ArrowLeftIcon data-icon="inline-start" />
          Back to messages
        </Button>
        <p role="alert" className="text-sm break-words text-destructive">
          Couldn't open this message: {error}
        </p>
        <Button variant="outline" size="sm" onClick={() => setRetry((n) => n + 1)}>
          Try again
        </Button>
      </div>
    );
  }
  if (!message) {
    return (
      <div className="flex flex-col gap-2 p-4" aria-busy>
        <Skeleton className="h-5 w-2/3" />
        <Skeleton className="h-4 w-1/2" />
        <Skeleton className="mt-2 h-40 w-full" />
      </div>
    );
  }

  const fields: Array<[string, string]> = [
    ["From", formatAddresses(message.From ? [message.From] : [])],
    ["To", formatAddresses(message.To)],
    ["Cc", formatAddresses(message.Cc)],
    ["Reply-To", formatAddresses(message.ReplyTo)],
    ["Date", formatTime(message.Date || message.Created)],
    ["Message-ID", message.MessageID ?? ""],
  ];
  const attachments = message.Attachments ?? [];

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex flex-col gap-2 px-4 py-3">
        <div className="flex items-start gap-2">
          <Button variant="ghost" size="icon-sm" className="-ml-1.5 @xl:hidden" aria-label="Back to messages" onClick={onBack}>
            <ArrowLeftIcon />
          </Button>
          <p className="min-w-0 flex-1 self-center font-medium break-words">
            {privacy.mail ? MASK : message.Subject || "(no subject)"}
          </p>
          {message.Tags?.map((t) => (
            <Badge key={t} variant="secondary" className="mt-0.5">
              {privacy.names ? MASK : t}
            </Badge>
          ))}
          <DeleteMessage conn={conn} message={message} onDeleted={onBack} />
        </div>
        <dl className="grid grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-x-3 gap-y-0.5 text-xs">
          {fields
            .filter(([, v]) => v)
            .map(([k, v]) => (
              <div key={k} className="contents">
                <dt className="text-muted-foreground">{k}</dt>
                <dd className="truncate">{privacy.mail && k !== "Date" ? MASK : v}</dd>
                <dd>{k !== "Date" ? <CopyButton text={v} label={k} /> : null}</dd>
              </div>
            ))}
        </dl>
        {attachments.length > 0 && !privacy.mail ? (
          <div className="flex flex-wrap gap-1.5">
            {attachments.map((a) => (
              <AttachmentChip key={a.PartID} conn={conn} id={message.ID} attachment={a} />
            ))}
          </div>
        ) : null}
      </div>
      <Separator />
      {privacy.mail ? (
        <p className="p-4 text-[13px] text-muted-foreground">Hidden while privacy mode is on.</p>
      ) : (
        <MessageTabs conn={conn} message={message} />
      )}
    </div>
  );
}

function formatAddresses(list: MailAddress[] | null | undefined): string {
  return (list ?? []).map((a) => (a.Name ? `${a.Name} <${a.Address}>` : a.Address)).join(", ");
}

/** MessageTabs shows the message every way a test needs (audit UI-33):
 *  rendered, plain text, HTML source, headers and raw source. Headers and
 *  raw load when their tab opens. */
function MessageTabs({ conn, message }: { conn: Conn | null; message: MailMessage }) {
  const hasHtml = !!message.HTML;
  const [tab, setTab] = useState(hasHtml ? "preview" : "text");
  const [headers, setHeaders] = useState<Record<string, string[]> | null>(null);
  const [raw, setRaw] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!conn || (tab !== "headers" && tab !== "raw")) return;
    if ((tab === "headers" && headers) || (tab === "raw" && raw !== null)) return;
    let stale = false;
    setError(null);
    const path = `/api/mail/v1/message/${encodeURIComponent(message.ID)}/${tab}`;
    const load =
      tab === "headers"
        ? api<Record<string, string[]>>(conn, "GET", path).then((h) => {
            if (!stale) setHeaders(h);
          })
        : apiText(conn, path).then((r) => {
            if (!stale) setRaw(r);
          });
    withTimeout(load, DEFAULT_TIMEOUT_MS, "Mail didn't answer in time").catch((e: unknown) => {
      if (!stale) setError(errorMessage(e));
    });
    return () => {
      stale = true;
    };
  }, [conn, tab, message.ID, headers, raw]);

  const pre = (text: string) => (
    <ScrollArea className="min-h-0 flex-1">
      <pre className="p-4 font-mono text-xs break-all whitespace-pre-wrap">{text}</pre>
    </ScrollArea>
  );
  const loading = (
    <div className="p-4" aria-busy>
      <Skeleton className="h-40 w-full" />
    </div>
  );
  const headerText = headers
    ? Object.entries(headers)
        .flatMap(([k, vs]) => vs.map((v) => `${k}: ${v}`))
        .join("\n")
    : "";

  return (
    <Tabs value={tab} onValueChange={setTab} className="min-h-0 flex-1 gap-0">
      <TabsList variant="line" className="h-9 px-3">
        {hasHtml ? <TabsTrigger value="preview">Preview</TabsTrigger> : null}
        <TabsTrigger value="text">Plain text</TabsTrigger>
        {hasHtml ? <TabsTrigger value="html">HTML source</TabsTrigger> : null}
        <TabsTrigger value="headers">Headers</TabsTrigger>
        <TabsTrigger value="raw">Raw</TabsTrigger>
      </TabsList>
      <Separator />
      {error ? (
        <p role="alert" className="p-4 text-sm break-words text-destructive">
          {error}
        </p>
      ) : null}
      <TabsContent value="preview" className="flex min-h-0 flex-col">
        {/* The sandbox allows no scripts, forms or navigation: the email is
            inert content. allow-same-origin (without allow-scripts, which
            would break the sandbox) only lets this pane see link clicks and
            send them to the system browser. Emails are authored for a light
            canvas, whatever the app theme is. */}
        <iframe
          title={`Email: ${message.Subject || "no subject"}`}
          sandbox="allow-same-origin"
          srcDoc={`<meta name="color-scheme" content="light">${message.HTML}`}
          onLoad={(e) => routeLinksToBrowser(e.currentTarget)}
          className="min-h-0 w-full flex-1"
        />
      </TabsContent>
      <TabsContent value="text" className="flex min-h-0 flex-col">
        {message.Text ? (
          <ScrollArea className="min-h-0 flex-1">
            <pre className="p-4 font-sans text-sm whitespace-pre-wrap">{message.Text}</pre>
          </ScrollArea>
        ) : (
          <p className="p-4 text-sm text-muted-foreground">This message has no plain-text part.</p>
        )}
      </TabsContent>
      <TabsContent value="html" className="flex min-h-0 flex-col">
        {pre(message.HTML)}
      </TabsContent>
      <TabsContent value="headers" className="flex min-h-0 flex-col">
        {headers ? pre(headerText) : error ? null : loading}
      </TabsContent>
      <TabsContent value="raw" className="flex min-h-0 flex-col">
        {raw !== null ? pre(raw) : error ? null : loading}
      </TabsContent>
    </Tabs>
  );
}

/** AttachmentChip saves an attachment to Downloads (through the daemon;
 *  the app itself has no file access) and shows it in its folder. */
function AttachmentChip({ conn, id, attachment: a }: { conn: Conn | null; id: string; attachment: MailAttachment }) {
  const [saving, setSaving] = useState(false);
  const save = async () => {
    if (!conn || saving) return;
    setSaving(true);
    try {
      const r = await withTimeout(
        api<{ path: string }>(conn, "POST", "/api/mail/save-attachment", { id, part: a.PartID }),
        DEFAULT_TIMEOUT_MS,
        "Saving took too long",
      );
      toast.success(`${a.FileName} saved to Downloads`, {
        action: { label: "Show file", onClick: () => revealPath(r.path) },
      });
    } catch (e) {
      toast.error(`Couldn't save ${a.FileName}`, { description: errorMessage(e) });
    } finally {
      setSaving(false);
    }
  };
  return (
    <Button
      size="xs"
      variant="outline"
      onClick={() => void save()}
      disabled={saving}
      title={`Save ${a.FileName} to Downloads`}
    >
      {saving ? (
        <Spinner data-icon="inline-start" className="motion-reduce:animate-none" />
      ) : (
        <PaperclipIcon data-icon="inline-start" />
      )}
      <span className="max-w-40 truncate">{a.FileName}</span>
      <span className="text-muted-foreground tabular-nums">{formatBytes(a.Size)}</span>
    </Button>
  );
}

/** DeleteMessage removes the open message. Caught mail can't come back,
 *  so it confirms first (ui-standard C1). */
function DeleteMessage({
  conn,
  message,
  onDeleted,
}: {
  conn: Conn | null;
  message: MailMessage;
  onDeleted: () => void;
}) {
  const privacy = usePrivacy();
  const remove = async () => {
    if (!conn) throw new Error("Bench isn't connected");
    await withTimeout(
      api(conn, "DELETE", "/api/mail/v1/messages", { IDs: [message.ID] }),
      DEFAULT_TIMEOUT_MS,
      "Mail didn't answer in time",
    );
    toast.success("Message deleted");
    onDeleted();
  };

  // As in DeleteMessages: one button is both the dialog's and the tooltip's trigger.
  return (
    <Tooltip>
      <ConfirmAction
        trigger={
          <TooltipTrigger asChild>
            <Button size="icon-sm" variant="ghost" className="-mt-0.5 -mr-1.5" aria-label="Delete message">
              <TrashIcon />
            </Button>
          </TooltipTrigger>
        }
        title="Delete this message?"
        description="Caught messages can't be recovered."
        details={[
          { label: "Subject", value: privacy.mail ? MASK : message.Subject || "(no subject)" },
          {
            label: "From",
            value: privacy.mail ? MASK : formatAddresses(message.From ? [message.From] : []) || "Unknown sender",
          },
        ]}
        confirmLabel="Delete message"
        confirmIcon={<TrashIcon data-icon="inline-start" />}
        tone="destructive"
        onConfirm={remove}
      />
      <TooltipContent>Delete message…</TooltipContent>
    </Tooltip>
  );
}

// Only these schemes leave the app; anything else (javascript:, file:,
// data:) in an email link is ignored.
const OPENABLE = /^(https?:|mailto:)/i;

/** routeLinksToBrowser opens clicked email links in the system browser. */
function routeLinksToBrowser(frame: HTMLIFrameElement) {
  frame.contentDocument?.addEventListener("click", (e) => {
    const link = (e.target as Element | null)?.closest?.("a[href]"); // Element: click targets are nodes
    if (!link) return;
    e.preventDefault();
    const href = link.getAttribute("href") ?? "";
    // openExternal shows a failure as a toast (audit UI-49).
    if (OPENABLE.test(href)) openExternal(href);
  });
}

export function formatTime(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const today = new Date();
  return d.toDateString() === today.toDateString()
    ? d.toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" })
    : d.toLocaleDateString(undefined, { month: "short", day: "numeric" });
}
