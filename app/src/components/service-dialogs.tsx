import { useEffect, useRef, useState, type FormEvent } from "react";
import { CopyIcon, PlusIcon } from "@phosphor-icons/react";
import { toast } from "sonner";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Field,
  FieldContent,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field";
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
import { Switch } from "@/components/ui/switch";
import { useDaemonContext } from "@/hooks/daemon-provider";
import { useRoute } from "@/hooks/route";
import { api, type Service, type ServiceType } from "@/lib/api";
import { DEFAULT_TIMEOUT_MS, errorMessage, withTimeout } from "@/lib/async";

// Same rule as the daemon (one DNS label): lowercase letters, digits,
// hyphens, not starting or ending with a hyphen.
const NAME_RE = /^[a-z0-9]([a-z0-9-]*[a-z0-9])?$/;


// A first create downloads the build (MySQL on Linux is ~1 GB).
const CREATE_TIMEOUT_MS = 30 * 60_000;

function OptionalChip() {
  return (
    <Badge variant="secondary" className="font-normal text-muted-foreground">
      Optional
    </Badge>
  );
}

function nameError(name: string): string | null {
  if (name === "" || NAME_RE.test(name)) return null;
  return "Use lowercase letters, digits and hyphens.";
}

/** useHandoff runs a long request from a dialog (ui-standard C4). The
 *  dialog stays open only until the daemon registers the task, so fast
 *  failures (a taken name, a busy port) still show inline; then it closes
 *  and the Activity tray shows progress and the cancel button. */
function useHandoff(kind: string, onOpenChange: (open: boolean) => void) {
  const { tasks } = useDaemonContext();
  const { navigate } = useRoute();
  const [pending, setPending] = useState<string | null>(null); // task target
  const [error, setError] = useState<string | null>(null);
  const handedOff = useRef(false);

  useEffect(() => {
    if (!pending) return;
    if (tasks?.some((t) => t.kind === kind && t.target === pending && t.state === "running")) {
      handedOff.current = true;
      setPending(null);
      onOpenChange(false);
    }
  }, [tasks, pending, kind, onOpenChange]);

  const start = (
    target: string,
    request: Promise<Service>,
    messages: { done: string; failed: string; cancelled: string },
  ) => {
    handedOff.current = false;
    setError(null);
    setPending(target);
    request.then(
      (svc) => {
        if (!handedOff.current) {
          handedOff.current = true;
          setPending(null);
          onOpenChange(false);
        }
        // The next steps: look at it, or connect an app (C4, UI-07, UI-12).
        toast.success(messages.done, {
          action: svc?.name
            ? { label: "View", onClick: () => navigate({ kind: "home", section: "services", service: svc.name }) }
            : undefined,
          cancel:
            svc?.env?.length > 0
              ? {
                  label: "Copy .env",
                  onClick: () =>
                    void navigator.clipboard.writeText(svc.env.join("\n")).then(
                      () => toast.success(`.env lines for ${svc.name} copied`),
                      (e: unknown) => toast.error("Couldn't copy", { description: errorMessage(e) }),
                    ),
                }
              : undefined,
        });
      },
      (e: unknown) => {
        const message = errorMessage(e);
        if (!handedOff.current) {
          setPending(null);
          setError(message);
          return;
        }
        if (message.trim() === "cancelled") toast(messages.cancelled);
        else toast.error(messages.failed, { description: message });
      },
    );
  };

  // The user can always let go of the dialog: the work continues and its
  // outcome arrives as a toast (and in Activity), even if a live update
  // announcing the task was missed.
  const continueInBackground = () => {
    handedOff.current = true;
    setPending(null);
    onOpenChange(false);
  };

  return { submitting: pending !== null, error, setError, start, continueInBackground };
}

/** freeName is the name a new instance gets when the field is left empty:
 *  the kind, or the first free "<kind>-n", so the form opens valid. */
function freeName(kind: string, services: Service[] | null): string {
  if (!kind) return "";
  const taken = new Set((services ?? []).map((s) => s.name));
  if (!taken.has(kind)) return kind;
  for (let i = 2; ; i++) if (!taken.has(`${kind}-${i}`)) return `${kind}-${i}`;
}

/** CreateServiceDialog: pick a service, version, name and port. Once the
 *  daemon has the task the dialog closes and Activity tracks the download. */
export function CreateServiceDialog({
  open,
  onOpenChange,
  initialService,
  initialName,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  initialService?: string;
  /** A name to start with, e.g. the site this database is for. */
  initialName?: string;
}) {
  const { conn, services, binaries } = useDaemonContext();
  const [catalog, setCatalog] = useState<ServiceType[] | null>(null);
  const [catalogError, setCatalogError] = useState<string | null>(null);
  const [kind, setKind] = useState(initialService ?? "");
  const [channel, setChannel] = useState("");
  const [name, setName] = useState("");
  const [port, setPort] = useState("");
  const [autostart, setAutostart] = useState(true);
  const { submitting, error, setError, start, continueInBackground } = useHandoff("service.create", onOpenChange);

  const loadCatalog = () => {
    if (!conn) return;
    setCatalogError(null);
    withTimeout(api<ServiceType[]>(conn, "GET", "/api/services/catalog"), DEFAULT_TIMEOUT_MS, "Bench didn't answer in time")
      .then((c) => {
        setCatalog(c);
        const first = c.find((t) => t.service === (initialService ?? kind)) ?? c[0];
        if (first) {
          setKind(first.service);
          setChannel(first.channels[first.channels.length - 1]);
        }
      })
      .catch((e) => setCatalogError(errorMessage(e)));
  };

  useEffect(() => {
    if (!open) return;
    setError(null);
    setName(initialName && !services?.some((s) => s.name === initialName) ? initialName : "");
    setPort("");
    setAutostart(true);
    loadCatalog();
    // Reset and reload on each open only; loadCatalog reads current state.
  }, [open]);

  const type = catalog?.find((t) => t.service === kind);
  const suggestedName = freeName(kind, services);
  // Creating the first instance of a version downloads it: say so (C1).
  const build = binaries?.find((b) => b.name === kind && b.channel === channel);
  const effectiveName = name || suggestedName;
  // While submitting, the new instance itself shows up in the list.
  const existing = !submitting && name !== "" && services?.some((s) => s.name === name);
  const singletonTaken = !submitting && type?.singleton && services?.some((s) => s.service === kind);
  const portNumber = port === "" ? 0 : Number(port);
  const portInvalid = port !== "" && (!Number.isInteger(portNumber) || portNumber < 1 || portNumber > 65535);
  const invalidName = nameError(name);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!conn || submitting || !type || invalidName || portInvalid || existing || singletonTaken) return;
    const target = effectiveName;
    start(
      target,
      withTimeout(
        api<Service>(conn, "POST", "/api/services", { service: kind, channel, name: target, port: portNumber, autostart }),
        CREATE_TIMEOUT_MS,
        `${target} is still being created; follow it in Activity`,
      ),
      {
        done: `${target} created`,
        failed: `Couldn't create ${target}`,
        cancelled: `Creating ${target} cancelled`,
      },
    );
  };

  return (
    <Dialog open={open} onOpenChange={(o) => !submitting && onOpenChange(o)}>
      <DialogContent>
        <form onSubmit={(e) => void submit(e)} className="flex flex-col gap-6">
          <DialogHeader>
            <DialogTitle>New service</DialogTitle>
            <DialogDescription className="sr-only">Pick a service, version, name and port.</DialogDescription>
          </DialogHeader>

          {catalog === null && !catalogError ? (
            <div className="flex flex-col gap-3" aria-busy>
              <Skeleton className="h-9 w-full" />
              <Skeleton className="h-9 w-full" />
              <Skeleton className="h-9 w-full" />
            </div>
          ) : catalogError ? (
            <Alert variant="destructive">
              <AlertTitle>Couldn't load the service catalog</AlertTitle>
              <AlertDescription>
                {catalogError}
                <Button type="button" size="sm" variant="outline" className="mt-2" onClick={loadCatalog}>
                  Try again
                </Button>
              </AlertDescription>
            </Alert>
          ) : (
            <FieldGroup>
              <div className="grid grid-cols-2 gap-3">
                <Field>
                  <FieldLabel htmlFor="svc-kind">Service</FieldLabel>
                  <Select
                    value={kind}
                    onValueChange={(v) => {
                      setKind(v);
                      const t = catalog?.find((c) => c.service === v);
                      setChannel(t ? t.channels[t.channels.length - 1] : "");
                    }}
                    disabled={submitting}
                  >
                    <SelectTrigger id="svc-kind" className="w-full">
                      <SelectValue placeholder="Choose a service" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        {catalog?.map((t) => (
                          <SelectItem key={t.service} value={t.service}>
                            {t.label}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </Field>
                <Field>
                  <FieldLabel htmlFor="svc-version">Version</FieldLabel>
                  {/* Keyed by service: Radix can't show a value whose item wasn't
                      rendered before the value changed, so remount per list. */}
                  <Select key={kind} value={channel} onValueChange={setChannel} disabled={submitting || !type}>
                    <SelectTrigger id="svc-version" className="w-full">
                      <SelectValue placeholder="Version" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        {type?.channels.map((c) => (
                          <SelectItem key={c} value={c}>
                            {c}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </Field>
              </div>

              <div className="grid grid-cols-2 gap-3">
                <Field data-invalid={!!invalidName || !!existing || undefined}>
                  <FieldLabel htmlFor="svc-name">
                    Name <OptionalChip />
                  </FieldLabel>
                  <Input
                    id="svc-name"
                    name="name"
                    value={name}
                    placeholder={suggestedName || "name"}
                    onChange={(e) => setName(e.target.value.toLowerCase())}
                    aria-invalid={!!invalidName || !!existing || undefined}
                    disabled={submitting}
                    autoComplete="off"
                    spellCheck={false}
                  />
                  <FieldError>
                    {invalidName ?? (existing ? `${effectiveName} already exists` : null)}
                  </FieldError>
                </Field>
                <Field data-invalid={portInvalid || undefined}>
                  <FieldLabel htmlFor="svc-port">
                    Port <OptionalChip />
                  </FieldLabel>
                  <Input
                    id="svc-port"
                    name="port"
                    inputMode="numeric"
                    value={port}
                    placeholder={type ? String(type.default_port) : ""}
                    onChange={(e) => setPort(e.target.value.replace(/\D/g, ""))}
                    aria-invalid={portInvalid || undefined}
                    disabled={submitting}
                    autoComplete="off"
                  />
                  <FieldError>{portInvalid ? "Use a port from 1 to 65535." : null}</FieldError>
                </Field>
              </div>
              {build ? (
                <p className="rounded-lg bg-well px-3 py-2.5 text-[13px] text-muted-foreground">
                  {build.installed
                    ? `${type?.label} ${build.version} is already downloaded.`
                    : `Downloads ${type?.label} ${build.version} from ${build.sources?.join(", ") || "the vendor"}, checksum verified.`}
                </p>
              ) : null}

              <Field orientation="horizontal">
                <Switch
                  id="svc-autostart"
                  checked={autostart}
                  onCheckedChange={setAutostart}
                  disabled={submitting}
                />
                <FieldContent>
                  <FieldLabel htmlFor="svc-autostart">Start with Bench</FieldLabel>
                </FieldContent>
              </Field>

              {singletonTaken ? (
                <p role="alert" className="text-sm text-destructive">
                  Only one {type?.label} instance is allowed, and it already exists.
                </p>
              ) : null}
            </FieldGroup>
          )}

          {submitting ? (
            <p className="text-[13px] text-muted-foreground" aria-live="polite">
              You can cancel it in Activity.
            </p>
          ) : null}

          {error ? (
            <Alert variant="destructive">
              <AlertTitle>Couldn't create the service</AlertTitle>
              <AlertDescription className="break-words whitespace-pre-wrap">{error}</AlertDescription>
            </Alert>
          ) : null}

          <DialogFooter>
            {submitting ? (
              <Button type="button" variant="outline" onClick={continueInBackground}>
                Continue in background
              </Button>
            ) : (
              <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
                Cancel
              </Button>
            )}
            <Button
              type="submit"
              disabled={
                submitting || !type || !!invalidName || portInvalid || !!existing || !!singletonTaken
              }
            >
              {submitting ? (
                <Spinner data-icon="inline-start" className="motion-reduce:animate-none" />
              ) : (
                <PlusIcon data-icon="inline-start" />
              )}
              {submitting ? "Starting…" : "Create service"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

/** CloneServiceDialog copies an instance with its data (the source stops
 *  briefly for a consistent copy). */
export function CloneServiceDialog({
  source,
  open,
  onOpenChange,
}: {
  source: Service;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { conn, services } = useDaemonContext();
  const [name, setName] = useState("");
  const { submitting, error, setError, start, continueInBackground } = useHandoff("service.clone", onOpenChange);

  useEffect(() => {
    if (!open) return;
    setName(`${source.name}-copy`);
    setError(null);
  }, [open, source.name]);

  const invalidName = name === "" ? "Enter a name." : nameError(name);
  const existing = !submitting && services?.some((s) => s.name === name);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!conn || submitting || invalidName || existing) return;
    start(
      name,
      withTimeout(
        api<Service>(conn, "POST", `/api/services/${encodeURIComponent(source.name)}/clone`, { name }),
        CREATE_TIMEOUT_MS,
        `Cloning ${source.name} is still running; follow it in Activity`,
      ),
      {
        done: `${source.name} cloned to ${name}`,
        failed: `Couldn't clone ${source.name}`,
        cancelled: `Cloning ${source.name} cancelled`,
      },
    );
  };

  return (
    <Dialog open={open} onOpenChange={(o) => !submitting && onOpenChange(o)}>
      <DialogContent>
        <form onSubmit={(e) => void submit(e)} className="flex flex-col gap-6">
          <DialogHeader>
            <DialogTitle>Clone {source.name}</DialogTitle>
            <DialogDescription>
              Copies it with all its data.
              {source.state === "running" ? ` ${source.name} stops briefly during the copy.` : ""}
            </DialogDescription>
          </DialogHeader>
          <Field data-invalid={(!!invalidName && name !== "") || !!existing || undefined}>
            <FieldLabel htmlFor={`clone-${source.name}`}>New name</FieldLabel>
            <Input
              id={`clone-${source.name}`}
              name="name"
              value={name}
              onChange={(e) => setName(e.target.value.toLowerCase())}
              aria-invalid={(!!invalidName && name !== "") || !!existing || undefined}
              disabled={submitting}
              autoComplete="off"
              spellCheck={false}
            />
            <FieldError>{existing ? `${name} already exists` : name !== "" ? invalidName : null}</FieldError>
          </Field>
          {error ? (
            <Alert variant="destructive">
              <AlertTitle>Couldn't clone {source.name}</AlertTitle>
              <AlertDescription className="break-words whitespace-pre-wrap">{error}</AlertDescription>
            </Alert>
          ) : null}
          <DialogFooter>
            {submitting ? (
              <Button type="button" variant="outline" onClick={continueInBackground}>
                Continue in background
              </Button>
            ) : (
              <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
                Cancel
              </Button>
            )}
            <Button type="submit" disabled={submitting || !!invalidName || !!existing}>
              {submitting ? (
                <Spinner data-icon="inline-start" className="motion-reduce:animate-none" />
              ) : (
                <CopyIcon data-icon="inline-start" />
              )}
              {submitting ? "Cloning…" : "Clone"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
