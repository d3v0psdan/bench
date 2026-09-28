import { useEffect, useState } from "react";
import { TrashIcon } from "@phosphor-icons/react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { ConfirmAction } from "@/components/confirm-action";
import { useDaemonContext } from "@/hooks/daemon-provider";
import { useRoute } from "@/hooks/route";
import { api, type Site, type SiteDeletePlan, type SiteDeleteRequest, type Task } from "@/lib/api";
import { DEFAULT_TIMEOUT_MS, errorMessage, withTimeout } from "@/lib/async";
import { usePrivacy } from "@/lib/privacy";

// What the OS calls the place deleted folders go.
const TRASH = navigator.userAgent.includes("Windows") ? "Recycle Bin" : "Trash";

const EVERYTHING: SiteDeleteRequest = { folder: true, database: true, service: true, mail: true, log: true };

/** DeleteSiteDialog deletes a site and what Bench made for it (ui-standard
 *  C1): it asks the daemon what applies to this site, lists it, and lets
 *  each part be kept. The folder goes to the Recycle Bin or Trash, so it
 *  can be restored. The deletion runs as a task in Activity (C3, C4). */
export function DeleteSiteDialog({
  site,
  open,
  onOpenChange,
}: {
  site: Site;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { conn } = useDaemonContext();
  const { navigate } = useRoute();
  const [plan, setPlan] = useState<SiteDeletePlan | null>(null);
  const [planError, setPlanError] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);
  const [picked, setPicked] = useState<SiteDeleteRequest>(EVERYTHING);
  const privacy = usePrivacy();
  const path = `/api/sites/${encodeURIComponent(site.name)}/delete`;

  useEffect(() => {
    if (!open || !conn) return;
    let cancelled = false;
    setPlan(null);
    setPlanError(null);
    setPicked(EVERYTHING);
    withTimeout(api<SiteDeletePlan>(conn, "GET", path), DEFAULT_TIMEOUT_MS, "Bench didn't answer in time").then(
      (p) => !cancelled && setPlan(p),
      (e: unknown) => !cancelled && setPlanError(errorMessage(e)),
    );
    return () => {
      cancelled = true;
    };
  }, [open, conn, path, attempt]);

  // A parked site is any folder in a parked folder: it goes only with its folder.
  const parked = site.kind === "parked";
  const req: SiteDeleteRequest = { ...picked, folder: parked || picked.folder };

  const remove = async () => {
    if (!conn) throw new Error("Bench isn't connected");
    if (!plan) throw new Error("Bench is still checking what to delete");
    const task = await withTimeout(api<Task>(conn, "POST", path, req), DEFAULT_TIMEOUT_MS, "Bench didn't answer in time");
    navigate({ kind: "home", section: "sites" });
    toast(`Deleting ${privacy.host(site.host)}`);
    return task;
  };

  const set = (key: keyof SiteDeleteRequest) => (on: boolean) => setPicked((p) => ({ ...p, [key]: on }));

  return (
    <ConfirmAction
      open={open}
      onOpenChange={onOpenChange}
      title={`Delete ${privacy.host(site.host)}?`}
      description={
        site.kind === "proxy"
          ? "Bench stops proxying this host."
          : `The folder goes to the ${TRASH}, so you can restore it.`
      }
      details={[
        { label: "Site", value: privacy.text(site.url) },
        ...(plan?.folder
          ? [{ label: "Folder", value: <span className="font-mono text-xs">{privacy.path(plan.folder)}</span> }]
          : []),
        ...(site.php_pinned ? [{ label: "Lost", value: `Its PHP ${site.php_pinned} pin` }] : []),
      ]}
      confirmLabel="Delete site"
      confirmIcon={<TrashIcon data-icon="inline-start" />}
      confirmText={site.host}
      confirmTextHint={privacy.names ? "the site's name" : undefined}
      tone="destructive"
      onConfirm={remove}
    >
      {planError ? (
        <div className="flex items-center gap-3">
          <p role="alert" className="min-w-0 flex-1 text-[13px] break-words text-destructive">
            Couldn't check what to delete: {planError}
          </p>
          <Button variant="outline" size="sm" onClick={() => setAttempt((n) => n + 1)}>
            Try again
          </Button>
        </div>
      ) : !plan ? (
        <div className="flex flex-col gap-3" aria-busy>
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-10 w-full" />
        </div>
      ) : (
        <FieldGroup className="gap-3">
          {plan.folder ? (
            <Option
              id="delete-folder"
              checked={req.folder}
              disabled={parked}
              onChange={set("folder")}
              label={`Move folder to ${TRASH}`}
            />
          ) : null}
          {plan.service ? (
            <Option id="delete-service" checked={req.service} onChange={set("service")} label={`Delete ${plan.service}`} />
          ) : null}
          {plan.database && !(plan.service && req.service) ? (
            <Option
              id="delete-database"
              checked={req.database}
              onChange={set("database")}
              label={`Drop ${plan.database.name} database`}
            />
          ) : null}
          {plan.mail_tag ? (
            <Option id="delete-mail" checked={req.mail} onChange={set("mail")} label="Delete mail" />
          ) : null}
          {plan.log_file ? (
            <Option id="delete-log" checked={req.log} onChange={set("log")} label="Delete creation log" />
          ) : null}
        </FieldGroup>
      )}
    </ConfirmAction>
  );
}

function Option({
  id,
  checked,
  disabled,
  onChange,
  label,
}: {
  id: string;
  checked: boolean;
  disabled?: boolean;
  onChange: (on: boolean) => void;
  label: string;
}) {
  return (
    <Field orientation="horizontal">
      <Switch id={id} checked={checked} disabled={disabled} onCheckedChange={onChange} />
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
    </Field>
  );
}
