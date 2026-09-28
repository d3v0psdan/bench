import { useState, type FormEvent } from "react";
import { TrashIcon } from "@phosphor-icons/react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Spinner } from "@/components/ui/spinner";
import { PageHeader, SectionCard, SectionRow } from "@/components/page";
import { DeleteSiteDialog } from "@/components/sites/delete-site";
import { useDaemonContext } from "@/hooks/daemon-provider";
import { api, type Site } from "@/lib/api";
import { errorMessage, withTimeout } from "@/lib/async";
import { usePrivacy } from "@/lib/privacy";

const SITE_TIMEOUT_MS = 60_000;

/** SiteSettings is one page (audit UI-30): what the site runs on, then
 *  the Danger card. Where it lives and its URL are in the header. */
export function SiteSettings({ site }: { site: Site }) {
  const [removeOpen, setRemoveOpen] = useState(false);
  const privacy = usePrivacy();

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="Settings" description={privacy.host(site.host)} />
      {site.kind === "proxy" ? (
        <ProxySettings site={site} />
      ) : (
        <SitePhpSettings site={site} />
      )}

      <SectionCard title="Danger" tone="danger">
        <SectionRow label="Delete site">
          <Button variant="destructive" size="sm" onClick={() => setRemoveOpen(true)}>
            <TrashIcon data-icon="inline-start" />
            Delete site…
          </Button>
        </SectionRow>
      </SectionCard>
      <DeleteSiteDialog site={site} open={removeOpen} onOpenChange={setRemoveOpen} />
    </div>
  );
}

/** ProxySettings changes where a proxy forwards: instant and reversible
 *  (ui-standard C2), so no confirm. */
function ProxySettings({ site }: { site: Site }) {
  const { conn, setSites } = useDaemonContext();
  const [target, setTarget] = useState(site.proxy_to ?? "");
  const privacy = usePrivacy();
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const changed = target.trim() !== "" && target.trim() !== site.proxy_to;

  const save = async (e: FormEvent) => {
    e.preventDefault();
    if (!conn || saving || !changed) return;
    setSaving(true);
    setError(null);
    try {
      setSites(
        await withTimeout(
          api<Site[]>(conn, "POST", "/api/sites/proxy", { name: site.name, target: target.trim() }),
          SITE_TIMEOUT_MS,
          "Bench didn't answer in time",
        ),
      );
      toast.success(`${privacy.host(site.host)} now forwards to ${target.trim()}`);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setSaving(false);
    }
  };

  return (
    <SectionCard title="Proxy">
      <SectionRow
        label="Forwards to"
        description={
          error ? (
            <span role="alert" className="text-destructive">
              {error}
            </span>
          ) : null
        }
      >
        <form onSubmit={(e) => void save(e)} className="flex items-center gap-2">
          <Input
            value={target}
            onChange={(e) => setTarget(e.target.value)}
            placeholder="5173 or 127.0.0.1:5173"
            aria-label={`Proxy target for ${site.host}`}
            className="w-48 font-mono text-xs md:text-xs"
            disabled={saving}
            spellCheck={false}
          />
          <Button type="submit" variant="outline" size="sm" disabled={!changed || saving}>
            {saving ? <Spinner data-icon="inline-start" className="motion-reduce:animate-none" /> : null}
            Save
          </Button>
        </form>
      </SectionRow>
    </SectionCard>
  );
}

function SitePhpSettings({ site }: { site: Site }) {
  const { conn, binaries, defaultPhp, setSites, status } = useDaemonContext();
  const privacy = usePrivacy();
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const installedPhp = (binaries ?? []).filter((b) => b.name === "php" && b.installed).map((b) => b.channel);
  // A pinned version might no longer be installed: keep it listed so the
  // state is visible rather than silently misrendered.
  const phpOptions =
    site.php_pinned && !installedPhp.includes(site.php_pinned) ? [...installedPhp, site.php_pinned] : installedPhp;

  const setPhp = async (version: string) => {
    if (!conn || saving) return;
    setSaving(true);
    setError(null);
    try {
      setSites(
        await withTimeout(
          api<Site[]>(conn, "POST", "/api/sites/php", { name: site.name, version }),
          SITE_TIMEOUT_MS,
          "Bench didn't answer in time",
        ),
      );
      const host = privacy.host(site.host);
      toast.success(version ? `${host} now runs PHP ${version}` : `${host} follows the default PHP`);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setSaving(false);
    }
  };

  return (
    <SectionCard title="PHP">
      <SectionRow
        label="PHP version"
        description={
          error ? (
            <span role="alert" className="text-destructive">
              {error}
            </span>
          ) : null
        }
      >
        {saving ? <Spinner className="text-muted-foreground motion-reduce:animate-none" /> : null}
        <Select
          value={site.php_pinned || "default"}
          onValueChange={(v) => void setPhp(v === "default" ? "" : v)}
          disabled={saving || !conn}
        >
          <SelectTrigger className="w-44" aria-label={`PHP version for ${site.host}`}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              {/* Say so wherever a version isn't installed (audit UI-02). */}
              <SelectItem value="default">
                Default{defaultPhp ? ` (${defaultPhp}${installedPhp.includes(defaultPhp) ? "" : ", not installed"})` : ""}
              </SelectItem>
              {phpOptions.map((ch) => (
                <SelectItem key={ch} value={ch}>
                  PHP {ch}
                  {installedPhp.includes(ch) ? "" : " (not installed)"}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
      </SectionRow>
      <SectionRow label="Server">
        <span className="font-mono text-[13px]">{status?.os === "windows" ? "php-cgi" : "php-fpm"}</span>
      </SectionRow>
    </SectionCard>
  );
}
