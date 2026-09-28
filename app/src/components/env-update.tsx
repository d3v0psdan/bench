import { useEffect, useState } from "react";
import { FileTextIcon } from "@phosphor-icons/react";
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
import { Field, FieldLabel } from "@/components/ui/field";
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { useDaemonContext } from "@/hooks/daemon-provider";
import { api, type Service, type Site } from "@/lib/api";
import { DEFAULT_TIMEOUT_MS, errorMessage, withTimeout } from "@/lib/async";
import { revealPath } from "@/lib/open";
import { isSecretEnv, MASK, usePrivacy } from "@/lib/privacy";

const SKIP = "__skip";

// Which .env block each service kind fills.
const ENV_KINDS: Array<{ kind: string; label: string; services: string[] }> = [
  { kind: "database", label: "Database", services: ["mysql", "mariadb", "postgresql"] },
  { kind: "cache", label: "Cache", services: ["valkey"] },
  { kind: "mail", label: "Mail", services: ["mailpit"] },
  { kind: "search", label: "Search", services: ["meilisearch"] },
  { kind: "storage", label: "Storage", services: ["rustfs"] },
];

interface EnvChange {
  key: string;
  old: string;
  new: string;
  added: boolean;
}

interface EnvResult {
  path: string;
  changes: EnvChange[];
}

/** envLines is what connecting a site to the picked services sets: its
 *  URL, each service's lines, and a database named after the site. */
export function envLines(site: Site, picked: Array<Service | undefined>): string[] {
  const lines = [`APP_URL=${site.url}`];
  for (const s of picked) {
    if (!s) continue;
    lines.push(...s.env.filter((l) => /^[A-Z][A-Z0-9_]*=/.test(l)));
    if (["mysql", "mariadb", "postgresql"].includes(s.service))
      lines.push(`DB_DATABASE=${site.name.replace(/-/g, "_")}`);
  }
  return lines;
}

/** EnvUpdateDialog lets Bench write a site's service connections into its
 *  .env. Nothing is written until the person has seen every line that
 *  will be added or changed (ui-standard C1); each written line is marked
 *  "# Bench generated". */
export function EnvUpdateDialog({
  site,
  open,
  onOpenChange,
}: {
  site: Site;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { conn, services } = useDaemonContext();
  const [picked, setPicked] = useState<Record<string, string>>({});
  const [preview, setPreview] = useState<EnvResult | null>(null);
  const [previewError, setPreviewError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const privacy = usePrivacy();
  // The site's own .env values can hold real credentials.
  const shownValue = (key: string, value: string) =>
    privacy.secrets && isSecretEnv(key, value) ? MASK : privacy.text(value);

  const groups = ENV_KINDS.map((g) => ({
    ...g,
    options: (services ?? []).filter((s) => g.services.includes(s.service) && s.env.length > 0),
  })).filter((g) => g.options.length > 0);
  const choice = (g: (typeof groups)[number]) => picked[g.kind] ?? g.options[0].name;
  const lines = envLines(
    site,
    groups.map((g) => g.options.find((o) => o.name === choice(g))),
  );
  const key = JSON.stringify(lines);

  // A dry run of the current picks shows exactly what would change.
  useEffect(() => {
    if (!open || !conn) return;
    let cancelled = false;
    setPreviewError(null);
    withTimeout(
      api<EnvResult>(conn, "POST", `/api/sites/${encodeURIComponent(site.name)}/env`, {
        lines: JSON.parse(key),
        dry_run: true,
      }),
      DEFAULT_TIMEOUT_MS,
      "Bench didn't answer in time",
    )
      .then((r) => !cancelled && setPreview(r))
      .catch((e: unknown) => !cancelled && setPreviewError(errorMessage(e)));
    return () => {
      cancelled = true;
    };
  }, [open, conn, site.name, key]);

  const save = async () => {
    if (!conn || saving) return;
    setSaving(true);
    setSaveError(null);
    try {
      const r = await withTimeout(
        api<EnvResult>(conn, "POST", `/api/sites/${encodeURIComponent(site.name)}/env`, { lines }),
        DEFAULT_TIMEOUT_MS,
        "Bench didn't answer in time",
      );
      onOpenChange(false);
      toast.success(`${privacy.host(site.host)}'s .env updated`, {
        description: `${r.changes.length} line${r.changes.length === 1 ? "" : "s"} set`,
        action: { label: "Show file", onClick: () => revealPath(r.path) },
      });
    } catch (e) {
      setSaveError(errorMessage(e));
    } finally {
      setSaving(false);
    }
  };

  const changes = preview?.changes ?? [];

  return (
    <Dialog open={open} onOpenChange={(o) => !saving && onOpenChange(o)}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>Let Bench update {privacy.host(site.host)}'s .env?</DialogTitle>
          <DialogDescription>
            Only the lines below change, each marked
            <code className="ml-1 font-mono text-xs">"# Bench generated"</code>.
          </DialogDescription>
        </DialogHeader>

        {services === null ? (
          <Skeleton className="h-40 w-full rounded-lg" aria-busy />
        ) : (
          <div className="flex flex-col gap-4">
            {groups.length > 0 ? (
              <div className="grid grid-cols-2 gap-3">
                {groups.map((g) => (
                  <Field key={g.kind}>
                    <FieldLabel htmlFor={`env-${g.kind}`}>{g.label}</FieldLabel>
                    <Select value={choice(g)} onValueChange={(v) => setPicked((p) => ({ ...p, [g.kind]: v }))}>
                      <SelectTrigger id={`env-${g.kind}`} className="w-full">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectGroup>
                          {g.options.map((o) => (
                            <SelectItem key={o.name} value={o.name}>
                              {o.name}
                            </SelectItem>
                          ))}
                          <SelectItem value={SKIP}>Don't change</SelectItem>
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                  </Field>
                ))}
              </div>
            ) : (
              <p className="text-[13px] text-muted-foreground">
                No services yet, so only APP_URL can be set.
              </p>
            )}

            {previewError ? (
              <Alert variant="destructive">
                <AlertTitle>Couldn't read the .env</AlertTitle>
                <AlertDescription className="break-words">{previewError}</AlertDescription>
              </Alert>
            ) : preview === null ? (
              <Skeleton className="h-32 w-full rounded-lg" aria-busy />
            ) : changes.length === 0 ? (
              <p className="rounded-lg bg-well p-3 text-[13px] text-muted-foreground">
                The .env already has these values.
              </p>
            ) : (
              <div className="max-h-72 overflow-auto rounded-lg bg-well">
                <table className="w-full text-left text-xs">
                  <thead className="sticky top-0 bg-well text-muted-foreground">
                    <tr>
                      <th className="px-3 py-2 font-medium">Key</th>
                      <th className="px-3 py-2 font-medium">Now</th>
                      <th className="px-3 py-2 font-medium">After</th>
                    </tr>
                  </thead>
                  <tbody className="font-mono">
                    {changes.map((c) => (
                      <tr key={c.key} className="border-t">
                        <td className="px-3 py-1.5 whitespace-nowrap">{c.key}</td>
                        <td className="max-w-40 truncate px-3 py-1.5 text-muted-foreground">
                          {c.added ? (
                            <Badge variant="secondary" className="font-sans">
                              Added
                            </Badge>
                          ) : (
                            shownValue(c.key, c.old) || '""'
                          )}
                        </td>
                        <td className="max-w-48 truncate px-3 py-1.5">{shownValue(c.key, c.new) || '""'}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
            {preview ? (
              <p className="truncate font-mono text-xs text-muted-foreground">{privacy.path(preview.path)}</p>
            ) : null}
            {saveError ? (
              <p role="alert" className="text-[13px] break-words text-destructive">
                {saveError}
              </p>
            ) : null}
          </div>
        )}

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
            Cancel
          </Button>
          <Button onClick={() => void save()} disabled={saving || changes.length === 0 || !!previewError}>
            {saving ? (
              <Spinner data-icon="inline-start" className="motion-reduce:animate-none" />
            ) : (
              <FileTextIcon data-icon="inline-start" />
            )}
            {changes.length > 0 ? `Update ${changes.length} line${changes.length === 1 ? "" : "s"}` : "Update .env"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
