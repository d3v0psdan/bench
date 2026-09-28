import { useEffect, useState, type ReactNode } from "react";
import { EnvelopeSimpleIcon, GearSixIcon, MagnifyingGlassIcon, PlayIcon, ScrollIcon, TrashIcon, TrayIcon } from "@phosphor-icons/react";
import { toast } from "sonner";
import { Alert, AlertAction, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { ConfirmAction } from "@/components/confirm-action";
import { InfoTip } from "@/components/info-tip";
import { DismissibleAlert, LetterTile, PageHeader } from "@/components/page";
import { LoadFailed } from "@/components/load-failed";
import { formatTime, MessageView } from "@/components/mail/message-view";
import { ALL, useMailList } from "@/components/mail/use-mail-list";
import { ServiceStateDot } from "@/components/services/service-meta";
import { ServiceSheet } from "@/components/services/service-sheet";
import { useDaemonContext } from "@/hooks/daemon-provider";
import { useRoute } from "@/hooks/route";
import { api, type Conn, type MailSummary, type Service } from "@/lib/api";
import { DEFAULT_TIMEOUT_MS, errorMessage, withTimeout } from "@/lib/async";
import { MASK, usePrivacy } from "@/lib/privacy";
import { cn } from "@/lib/utils";


/** MailPane: the local mail catcher. Apps send with MAIL_USERNAME set to
 *  their APP_NAME, which files each message under that project's inbox. */
export function MailPane() {
  const { conn, services, loadError, reloadData } = useDaemonContext();
  const mail = services?.find((s) => s.service === "mailpit");
  // Enabling lives here, not in EnableMail: the new instance's row shows
  // up mid-create, and its pending state and any error must survive that.
  const [enabling, setEnabling] = useState(false);
  const [enableError, setEnableError] = useState<string | null>(null);
  const [serverOpen, setServerOpen] = useState(false);
  const [serverLogs, setServerLogs] = useState(false);
  const openServer = (logs: boolean) => {
    setServerLogs(logs);
    setServerOpen(true);
  };
  // A crash toast's "View logs" arrives here with serviceLogs set.
  const { route, navigate } = useRoute();
  const logsRequested = route.kind === "home" && !!route.serviceLogs;
  useEffect(() => {
    if (!logsRequested) return;
    setServerLogs(true);
    setServerOpen(true);
    navigate({ kind: "home", section: "mail" });
  }, [logsRequested, navigate]);

  const enable = async () => {
    if (!conn || enabling) return;
    setEnabling(true);
    setEnableError(null);
    try {
      await withTimeout(
        api<Service>(conn, "POST", "/api/services", { service: "mailpit", name: "mail", autostart: true }),
        10 * 60_000,
        "Enabling mail is taking too long; check your connection and try again",
      );
    } catch (e) {
      setEnableError(errorMessage(e));
    } finally {
      setEnabling(false);
    }
  };

  const body = (() => {
    if (services === null && loadError) return <LoadFailed title="Couldn't load mail" message={loadError} onRetry={reloadData} />;
    if (services === null) return <Skeleton className="h-64 w-full rounded-lg" aria-busy />;
    if (!mail || mail.state === "installing" || mail.state === "initializing") {
      return <EnableMail conn={conn} busy={enabling || !!mail} error={enableError} onEnable={() => void enable()} />;
    }
    if (mail.state !== "running") return <MailStopped conn={conn} mail={mail} onViewLogs={() => openServer(true)} />;
    return <MailInbox conn={conn} mail={mail} onOpenServer={() => openServer(false)} />;
  })();
  // The server's status and settings show once it exists, stopped or not.
  const ready = mail !== undefined && mail.state !== "installing" && mail.state !== "initializing";

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader
        title={
          ready ? (
            <span className="flex items-center gap-3">
              Mail
              <ServiceStateDot state={mail.state} />
            </span>
          ) : (
            "Mail"
          )
        }
        actions={
          ready ? (
            <Button variant="outline" size="sm" onClick={() => openServer(false)}>
              <GearSixIcon data-icon="inline-start" />
              Server settings
            </Button>
          ) : undefined
        }
      />
      {body}
      {/* One home for Mail (audit UI-21): the server's start, stop,
          autostart, port and logs live here, in its sheet. */}
      <ServiceSheet name={serverOpen ? mail?.name : undefined} showLogs={serverLogs} onClose={() => setServerOpen(false)} />
    </div>
  );
}

function EnableMail({
  conn,
  busy,
  error,
  onEnable,
}: {
  conn: Conn | null;
  busy: boolean;
  error: string | null;
  onEnable: () => void;
}) {
  return (
    <Empty className="border border-dashed bg-card">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <EnvelopeSimpleIcon />
        </EmptyMedia>
        <EmptyTitle>Catch every email your apps send</EmptyTitle>
        <EmptyDescription>
          A local SMTP server. Nothing leaves your machine.{" "}
          <InfoTip label="About inboxes">
            Each app gets an inbox named after its SMTP username, so {'MAIL_USERNAME="${APP_NAME}"'} sorts mail by
            app.
          </InfoTip>
        </EmptyDescription>
      </EmptyHeader>
      <EmptyContent>
        <Button variant="brand" onClick={onEnable} disabled={busy || !conn}>
          {busy ? (
            <Spinner data-icon="inline-start" className="motion-reduce:animate-none" />
          ) : (
            <EnvelopeSimpleIcon data-icon="inline-start" />
          )}
          {busy ? "Enabling Mail…" : "Enable Mail"}
        </Button>
        {error ? (
          <p role="alert" className="text-sm break-words text-destructive">
            {error}
          </p>
        ) : null}
      </EmptyContent>
    </Empty>
  );
}

function MailStopped({ conn, mail, onViewLogs }: { conn: Conn | null; mail: Service; onViewLogs: () => void }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const detail = error ?? mail.error;

  const start = async () => {
    if (!conn || busy) return;
    setBusy(true);
    setError(null);
    try {
      await withTimeout(
        api(conn, "POST", `/api/services/${encodeURIComponent(mail.name)}/start`),
        60_000,
        "Mail is taking too long to start; its logs may say why",
      );
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    // A lasting state, not a fresh error: polite status, like the other
    // standing red alerts (ui-standard A6). A start error inside still alerts.
    <Alert
      variant="destructive"
      role="status"
      // With no error line the title stands alone; center it with the icon
      // and actions, which span both rows of the alert's grid.
      className={cn(!detail && "items-center *:[svg]:translate-y-0")}
    >
      <EnvelopeSimpleIcon />
      <AlertTitle className={cn("flex items-center gap-2", !detail && "row-span-2")}>
        Mail is not running <ServiceStateDot state={mail.state} />
      </AlertTitle>
      {detail ? (
        <AlertDescription>
          <span role={error ? "alert" : undefined} className="block break-words">
            {detail}
          </span>
        </AlertDescription>
      ) : null}
      <AlertAction className="flex items-center gap-2">
        <Button size="sm" variant="outline" onClick={onViewLogs}>
          <ScrollIcon data-icon="inline-start" />
          View logs
        </Button>
        <Button
          size="sm"
          variant="outline"
          onClick={() => void start()}
          disabled={busy || !conn || (mail.state !== "stopped" && mail.state !== "failed")}
        >
          {busy ? (
            <Spinner data-icon="inline-start" className="motion-reduce:animate-none" />
          ) : (
            <PlayIcon data-icon="inline-start" />
          )}
          Start mail
        </Button>
      </AlertAction>
    </Alert>
  );
}

function MailInbox({ conn, mail, onOpenServer }: { conn: Conn | null; mail: Service; onOpenServer: () => void }) {
  const { inbox, setInbox, list, listError, retry, query, setQuery, search, loadMore, fetching, tags } =
    useMailList(conn);
  const [selected, setSelected] = useState<string | null>(null);
  // Inboxes are named after apps, so they hide with site names.
  const privacy = usePrivacy();
  const inboxLabel = (tag: string) => (privacy.names ? MASK : tag);

  // Every Mailpit list and search answers with the mailbox-wide tags and
  // unread count, plus the unread count of what it matched.
  const allUnread = list?.unread ?? 0;
  const messages = list?.messages ?? [];
  const selectedGone = selected !== null && list !== null && !messages.some((m) => m.ID === selected);
  const hasMore = list !== null && messages.length < list.messages_count;
  const reading = selected !== null && !selectedGone;
  const pickInbox = (next: string) => {
    setInbox(next);
    setSelected(null);
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3">
      {mail.port !== 2525 ? (
        <DismissibleAlert
          id={`mail-port-${mail.port}`}
          variant="warning"
          className="shrink-0"
          action={
            <Button size="sm" variant="outline" onClick={onOpenServer}>
              <GearSixIcon data-icon="inline-start" />
              Server settings
            </Button>
          }
        >
          <TrayIcon />
          <AlertTitle>Mail listens on port {mail.port}</AlertTitle>
          <AlertDescription>
            Port 2525 was taken, so set MAIL_PORT={mail.port} in your apps' .env.
          </AlertDescription>
        </DismissibleAlert>
      ) : null}

      {/* A mail client in one card: inboxes, messages, the open message.
          It sizes to its own width, not the window's (the sidebar can be
          open or not): one pane below 36rem (the list, or the open message
          with a back button), list and reader from 36rem, and the inbox
          rail from 48rem; below that the rail folds into a picker. */}
      <div className="@container flex min-h-80 flex-1">
        <div className="grid flex-1 grid-cols-1 grid-rows-[minmax(0,1fr)] overflow-hidden rounded-xl bg-card shadow-card @xl:grid-cols-[15rem_minmax(0,1fr)] @3xl:grid-cols-[10rem_16rem_minmax(0,1fr)] @5xl:grid-cols-[12rem_20rem_minmax(0,1fr)]">
          <nav aria-label="Inboxes" className="hidden min-h-0 flex-col overflow-y-auto border-r bg-well @3xl:flex">
            <p className="flex h-11 shrink-0 items-center border-b px-4 text-xs font-medium text-muted-foreground">Inboxes</p>
            <div className="flex flex-col gap-0.5 p-2">
              <InboxButton
                label="All inboxes"
                icon={<TrayIcon className="size-5 shrink-0 text-muted-foreground" />}
                unread={allUnread}
                current={inbox === ALL}
                onClick={() => pickInbox(ALL)}
              />
              {tags.map((t) => (
                <InboxButton
                  key={t}
                  label={inboxLabel(t)}
                  icon={<LetterTile name={inboxLabel(t)} />}
                  current={inbox === t}
                  onClick={() => pickInbox(t)}
                />
              ))}
            </div>
          </nav>

          <div className={cn("min-h-0 flex-col @xl:flex @xl:border-r", reading ? "hidden" : "flex")}>
            <div className="flex h-11 shrink-0 items-center gap-1 border-b pr-2 pl-3">
              <div className="relative min-w-0 flex-1">
                <MagnifyingGlassIcon className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
                <Input
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                  placeholder="Search mail, e.g. invoice"
                  aria-label="Search mail"
                  className="h-7 pl-8 text-xs md:text-xs"
                />
              </div>
              {/* Delete empties a whole inbox, so it hides while a search
                  shows only part of one. */}
              {search === "" ? (
                <DeleteMessages
                  conn={conn}
                  inbox={inbox}
                  count={inbox === ALL ? (list?.messages_count ?? messages.length) : messages.length}
                  disabled={messages.length === 0}
                />
              ) : null}
            </div>
            <div className="border-b py-2 pr-2 pl-3 @3xl:hidden">
              <Select value={inbox} onValueChange={pickInbox}>
                <SelectTrigger size="sm" className="w-full" aria-label="Inbox">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value={ALL}>All inboxes</SelectItem>
                    {tags.map((t) => (
                      <SelectItem key={t} value={t}>
                        {inboxLabel(t)}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </div>

            {listError && list === null ? (
              <div className="flex flex-col items-start gap-2 p-3">
                <p role="alert" className="text-[13px] break-words text-destructive">
                  Couldn't load messages: {listError}
                </p>
                <Button size="sm" variant="outline" disabled={fetching} onClick={retry}>
                  {fetching ? <Spinner data-icon="inline-start" className="motion-reduce:animate-none" /> : null}
                  Try again
                </Button>
              </div>
            ) : list === null ? (
              <div className="flex flex-col gap-4 p-3" aria-busy>
                {[0, 1, 2, 3].map((i) => (
                  <div key={i} className="flex flex-col gap-1.5">
                    <Skeleton className="h-3.5 w-2/3" />
                    <Skeleton className="h-3 w-full" />
                  </div>
                ))}
              </div>
            ) : messages.length === 0 && search !== "" ? (
              <div className="flex flex-col items-center gap-3 px-4 py-10 text-center">
                <p className="text-[13px] text-muted-foreground">No messages match “{search}”</p>
                <Button size="sm" variant="outline" onClick={() => setQuery("")}>
                  Clear search
                </Button>
              </div>
            ) : messages.length === 0 ? (
              <div className="flex flex-col gap-1.5 px-4 py-10 text-center text-[13px] text-muted-foreground">
                <p>{inbox === ALL ? "No messages yet" : `No messages for ${inboxLabel(inbox)}`}</p>
                {/* In the one-pane layout the reader, and its how-to, is hidden. */}
                <p className="text-balance @xl:hidden">Copy .env from Server settings into your app.</p>
              </div>
            ) : (
              // Native scrolling: ScrollArea sizes its content as a table, which
              // stretches rows past the column and defeats truncate.
              <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain">
                <ul>
                  {messages.map((m) => (
                    <li key={m.ID}>
                      <MessageRow message={m} current={selected === m.ID} onClick={() => setSelected(m.ID)} />
                    </li>
                  ))}
                  {hasMore ? (
                    <li className="flex flex-col gap-1.5 p-2">
                      {listError ? (
                        <p role="alert" className="px-2 text-xs break-words text-destructive">
                          Couldn't load more: {listError}
                        </p>
                      ) : null}
                      <Button
                        variant="ghost"
                        size="sm"
                        className="w-full"
                        disabled={fetching}
                        onClick={() => (listError ? retry() : loadMore())}
                      >
                        {fetching ? <Spinner data-icon="inline-start" className="motion-reduce:animate-none" /> : null}
                        {listError ? "Try again" : `Load more (${list.messages_count - messages.length} left)`}
                      </Button>
                    </li>
                  ) : null}
                </ul>
              </div>
            )}

            {list ? (
              <p className="mt-auto border-t px-3 py-2 text-xs text-muted-foreground tabular-nums">
                {list.messages_count} message{list.messages_count === 1 ? "" : "s"}
                {list.messages_unread > 0 ? `, ${list.messages_unread} unread` : ""}
              </p>
            ) : null}
          </div>

          <div className={cn("min-h-0 min-w-0 @xl:block", reading ? "block" : "hidden")}>
            {reading ? (
              <MessageView conn={conn} id={selected} onBack={() => setSelected(null)} />
            ) : list !== null && messages.length === 0 && search === "" ? (
              <Empty className="h-full">
                <EmptyHeader>
                  <EmptyMedia variant="icon">
                    <EnvelopeSimpleIcon />
                  </EmptyMedia>
                  <EmptyTitle>{inbox === ALL ? "Waiting for mail" : `Nothing from ${inboxLabel(inbox)} yet`}</EmptyTitle>
                  <EmptyDescription>Copy .env from Server settings into your app.</EmptyDescription>
                </EmptyHeader>
              </Empty>
            ) : (
              <Empty className="h-full">
                <EmptyHeader>
                  <EmptyTitle className="text-base">Select a message</EmptyTitle>
                </EmptyHeader>
              </Empty>
            )}
          </div>
        </div>
      </div>

      {listError && list !== null && !hasMore ? (
        <div className="flex shrink-0 items-center gap-3">
          <p role="alert" className="min-w-0 flex-1 text-xs break-words text-destructive">
            Couldn't refresh: {listError}
          </p>
          <Button variant="outline" size="sm" disabled={fetching} onClick={() => retry()}>
            {fetching ? <Spinner data-icon="inline-start" className="motion-reduce:animate-none" /> : null}
            Try again
          </Button>
        </div>
      ) : null}
    </div>
  );
}

/** InboxButton is one entry in the inbox rail: All inboxes or one app's. */
function InboxButton({
  label,
  icon,
  unread = 0,
  current,
  onClick,
}: {
  label: string;
  icon: ReactNode;
  unread?: number;
  current: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-current={current ? "true" : undefined}
      className={cn(
        "flex h-8 shrink-0 items-center gap-2 rounded-md px-2 text-left text-[13px] text-muted-foreground outline-none transition-colors duration-100 ease-out",
        "hover:bg-subtle/60 hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring",
        "aria-[current=true]:bg-card aria-[current=true]:font-medium aria-[current=true]:text-foreground aria-[current=true]:shadow-card",
      )}
    >
      {icon}
      <span className="min-w-0 flex-1 truncate">{label}</span>
      {unread > 0 ? (
        <span className="rounded-full bg-brand px-1.5 text-[11px] leading-4 font-medium text-brand-foreground tabular-nums">
          <span className="sr-only">Unread: </span>
          {unread}
        </span>
      ) : null}
    </button>
  );
}

/** MessageRow: sender and time, then the subject and the first line of text. */
function MessageRow({ message: m, current, onClick }: { message: MailSummary; current: boolean; onClick: () => void }) {
  const privacy = usePrivacy();
  return (
    <button
      type="button"
      onClick={onClick}
      aria-current={current}
      className={cn(
        "relative flex w-full flex-col gap-0.5 border-b px-3 py-2.5 text-left outline-none transition-colors duration-150 ease-out hover:bg-well focus-visible:bg-well focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset",
        current &&
          "bg-subtle hover:bg-subtle before:absolute before:inset-y-2 before:left-0 before:w-0.5 before:rounded-full before:bg-brand",
      )}
    >
      <span className="flex items-center gap-1.5">
        {!m.Read ? (
          <>
            <span className="size-1.5 shrink-0 rounded-full bg-brand" aria-hidden />
            <span className="sr-only">Unread:</span>
          </>
        ) : null}
        <span className={cn("min-w-0 flex-1 truncate text-[13px]", m.Read ? "font-medium" : "font-semibold")}>
          {privacy.mail ? MASK : m.From?.Name || m.From?.Address || "Unknown sender"}
        </span>
        <span className="shrink-0 text-xs text-muted-foreground tabular-nums">{formatTime(m.Created)}</span>
      </span>
      <span className={cn("truncate text-[13px]", m.Read && "text-muted-foreground")}>
        {privacy.mail ? MASK : m.Subject || "(no subject)"}
      </span>
      {m.Snippet && !privacy.mail ? <span className="truncate text-xs text-muted-foreground">{m.Snippet}</span> : null}
    </button>
  );
}

function DeleteMessages({
  conn,
  inbox,
  count,
  disabled,
}: {
  conn: Conn | null;
  inbox: string;
  count: number;
  disabled: boolean;
}) {
  const privacy = usePrivacy();
  const shown = privacy.names ? MASK : inbox;
  const scope = inbox === ALL ? "all messages" : `every message in ${shown}`;

  const remove = async () => {
    if (!conn) throw new Error("Bench isn't connected");
    const path =
      inbox === ALL
        ? "/api/mail/v1/messages"
        : `/api/mail/v1/search?query=${encodeURIComponent(`tag:"${inbox}"`)}`;
    await withTimeout(api(conn, "DELETE", path), DEFAULT_TIMEOUT_MS, "Mail didn't answer in time");
    toast.success(inbox === ALL ? "All messages deleted" : `Messages in ${shown} deleted`);
  };

  // The dialog's trigger is also the tooltip's: AlertDialogTrigger and
  // TooltipTrigger both hand their props down to the one button.
  return (
    <Tooltip>
      <ConfirmAction
        trigger={
          <TooltipTrigger asChild>
            <Button size="icon-sm" variant="ghost" disabled={disabled} aria-label="Delete messages">
              <TrashIcon />
            </Button>
          </TooltipTrigger>
        }
        title={`Delete ${scope}?`}
        description="Caught messages can't be recovered."
        details={[
          { label: "Inbox", value: inbox === ALL ? "All inboxes" : shown },
          { label: "Messages", value: `${count} ${count === 1 ? "message" : "messages"}` },
        ]}
        confirmLabel="Delete messages"
        confirmIcon={<TrashIcon data-icon="inline-start" />}
        tone="destructive"
        onConfirm={remove}
      />
      <TooltipContent>Delete messages…</TooltipContent>
    </Tooltip>
  );
}
