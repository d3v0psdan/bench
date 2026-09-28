import { useEffect, useState } from "react";
import { CaretDownIcon, CodeIcon } from "@phosphor-icons/react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { ButtonGroup } from "@/components/ui/button-group";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Spinner } from "@/components/ui/spinner";
import { InfoTip } from "@/components/info-tip";
import { SectionCard, SectionRow } from "@/components/page";
import { useDaemonContext } from "@/hooks/daemon-provider";
import { api, type Conn, type Site } from "@/lib/api";
import { DEFAULT_TIMEOUT_MS, errorMessage, withTimeout } from "@/lib/async";
import { currentPrivacy, usePrivacy } from "@/lib/privacy";

// Opening things in the user's own apps goes through the daemon (it finds
// the app and launches it), so the GUI, the CLI and curl behave the same.

const EDITOR_SETTING = "editor.default";
/** The Select value for "no saved choice" (Radix items can't be ""). */
const FIRST_INSTALLED = "auto";

export interface Editor {
  id: string;
  name: string;
  installed: boolean;
}

interface Opened {
  app: string;
}

/** openIn asks the daemon to open something and names the app it used. */
function openIn(conn: Conn, path: string, body?: unknown) {
  return withTimeout(api<Opened>(conn, "POST", path, body), DEFAULT_TIMEOUT_MS, "The app didn't open in time");
}

/** openWithToast runs an open from a menu, where the item closes at once:
 *  the toast carries the pending, success and failure states. */
export function openWithToast(conn: Conn | null, path: string, body: unknown, what: string) {
  if (!conn) {
    toast.error("Bench isn't connected");
    return;
  }
  const shown = currentPrivacy().text(what);
  toast.promise(openIn(conn, path, body), {
    loading: `Opening ${shown}…`,
    success: (r) => `Opened ${shown} in ${r.app}`,
    error: (e: unknown) => ({ message: `Couldn't open ${shown}`, description: errorMessage(e) }),
  });
}

/** useEditors loads the editors Bench knows, with which are installed. */
export function useEditors() {
  const { conn } = useDaemonContext();
  const [editors, setEditors] = useState<Editor[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    if (!conn) return;
    let cancelled = false;
    withTimeout(api<Editor[]>(conn, "GET", "/api/editors"), DEFAULT_TIMEOUT_MS, "Bench didn't list editors in time").then(
      (list) => !cancelled && setEditors(list),
      (e: unknown) => !cancelled && setError(errorMessage(e)),
    );
    return () => {
      cancelled = true;
    };
  }, [conn]);
  return { editors, error };
}

/** OpenInEditorButton opens the site's folder in the default editor; the
 *  caret picks another installed one. */
export function OpenInEditorButton({ site }: { site: Site }) {
  const { conn } = useDaemonContext();
  const { editors } = useEditors();
  const [pending, setPending] = useState(false);
  const privacy = usePrivacy();
  const installed = (editors ?? []).filter((e) => e.installed);
  const path = `/api/sites/${encodeURIComponent(site.name)}/open`;

  const open = async (editor?: string) => {
    if (!conn || pending) return;
    setPending(true);
    try {
      const r = await openIn(conn, path, { in: "editor", editor });
      toast.success(`Opened ${privacy.host(site.host)} in ${r.app}`);
    } catch (e) {
      toast.error(`Couldn't open ${privacy.host(site.host)} in an editor`, { description: errorMessage(e) });
    } finally {
      setPending(false);
    }
  };

  return (
    <ButtonGroup>
      <Button variant="outline" onClick={() => void open()} disabled={pending}>
        {pending ? (
          <Spinner data-icon="inline-start" className="motion-reduce:animate-none" />
        ) : (
          <CodeIcon data-icon="inline-start" />
        )}
        Open in editor
      </Button>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="outline" size="icon" aria-label="Choose an editor" disabled={pending}>
            <CaretDownIcon />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuLabel>Open in</DropdownMenuLabel>
          <DropdownMenuGroup>
            {installed.map((e) => (
              <DropdownMenuItem key={e.id} onSelect={() => void open(e.id)}>
                {e.name}
              </DropdownMenuItem>
            ))}
            {editors !== null && installed.length === 0 ? (
              <DropdownMenuItem disabled>No supported editor found</DropdownMenuItem>
            ) : null}
            {editors === null ? <DropdownMenuItem disabled>Looking for editors…</DropdownMenuItem> : null}
          </DropdownMenuGroup>
        </DropdownMenuContent>
      </DropdownMenu>
    </ButtonGroup>
  );
}

/** EditorCard is the "Default editor" setting on Settings > General. */
export function EditorCard() {
  const { conn } = useDaemonContext();
  const { editors, error: listError } = useEditors();
  const [value, setValue] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!conn) return;
    let cancelled = false;
    withTimeout(
      api<Record<string, string>>(conn, "GET", "/api/settings"),
      DEFAULT_TIMEOUT_MS,
      "Bench didn't answer in time",
    ).then(
      (s) => !cancelled && setValue(s[EDITOR_SETTING] || FIRST_INSTALLED),
      (e: unknown) => !cancelled && setError(errorMessage(e)),
    );
    return () => {
      cancelled = true;
    };
  }, [conn]);

  const save = async (next: string) => {
    if (!conn) return;
    const previous = value;
    setValue(next);
    setSaving(true);
    setError(null);
    try {
      await withTimeout(
        api(conn, "POST", "/api/settings", { key: EDITOR_SETTING, value: next === FIRST_INSTALLED ? "" : next }),
        DEFAULT_TIMEOUT_MS,
        "Saving took too long",
      );
    } catch (e) {
      setValue(previous);
      setError(errorMessage(e));
    } finally {
      setSaving(false);
    }
  };

  const loadError = listError ?? error;
  const installed = (editors ?? []).filter((e) => e.installed);
  // A saved editor that has since been uninstalled stays visible, marked.
  const saved = editors?.find((e) => e.id === value && !e.installed);

  return (
    <SectionCard title="Editor">
      <SectionRow
        label={
          <span className="inline-flex items-center gap-1">
            Default editor
            <InfoTip label="Supported editors">Bench finds VS Code, PhpStorm, Cursor and Zed.</InfoTip>
          </span>
        }
        description={
          loadError ? (
            <span role="alert" className="text-destructive">
              {loadError}
            </span>
          ) : null
        }
      >
        {saving ? <Spinner className="text-muted-foreground motion-reduce:animate-none" /> : null}
        <Select value={value ?? undefined} onValueChange={(v) => void save(v)} disabled={value === null || saving}>
          <SelectTrigger className="w-48" aria-label="Default editor">
            <SelectValue placeholder="Loading…" />
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              <SelectItem value={FIRST_INSTALLED}>First one installed</SelectItem>
              {installed.map((e) => (
                <SelectItem key={e.id} value={e.id}>
                  {e.name}
                </SelectItem>
              ))}
              {saved ? <SelectItem value={saved.id}>{saved.name} (not installed)</SelectItem> : null}
            </SelectGroup>
          </SelectContent>
        </Select>
      </SectionRow>
    </SectionCard>
  );
}
