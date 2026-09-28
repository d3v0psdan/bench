import { useEffect, useState, type FormEvent, type KeyboardEvent } from "react";
import { open as pickDirectory } from "@tauri-apps/plugin-dialog";
import {
  ArrowLeftIcon,
  ArrowRightIcon,
  ArrowsLeftRightIcon,
  FolderSimplePlusIcon,
  type Icon,
  LinkSimpleIcon,
  PlusIcon,
  SparkleIcon,
} from "@phosphor-icons/react";
import { toast } from "sonner";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Progress } from "@/components/ui/progress";
import { Spinner } from "@/components/ui/spinner";
import { useDaemonContext } from "@/hooks/daemon-provider";
import { useRoute, type Route } from "@/hooks/route";
import type { Service, Site, Task } from "@/lib/api";
import { DEFAULT_TIMEOUT_MS } from "@/lib/async";
import { toRequest, type NewAppAnswers, type Stack } from "@/lib/new-app-steps";
import { openExternal, revealPath } from "@/lib/open";
import { MASK, usePrivacy } from "@/lib/privacy";
import { folderName, nameProblem, suggestName } from "@/lib/site-name";
import { cn } from "@/lib/utils";
import { LinkStep, ProxyStep, targetError } from "./link-proxy-steps";
import {
  Choices,
  cleanName,
  DatabaseStep,
  JavaScriptStep,
  KitLogos,
  NameStep,
  Question,
  Review,
  StackLogo,
  YesNo,
} from "./new-app-steps";
import { useAddSiteFlow, type AddSiteKind, type Step, type Tool } from "./use-add-site-flow";

export type { AddSiteKind };

/** useAddSite is the one way to add a site (audit UI-23): the Add site
 *  dialog, opened at its first step or straight at one kind (the palette
 *  and empty states). A new app is created by a daemon task, which
 *  useNewAppLanding follows from the shell. */
export function useAddSite() {
  const [open, setOpen] = useState(false);
  const [kind, setKind] = useState<AddSiteKind | null>(null);

  const start = (next?: AddSiteKind) => {
    setKind(next ?? null);
    setOpen(true);
  };

  const dialogs = (
    <AddSiteDialog open={open} onOpenChange={setOpen} initialKind={kind} onCreating={(t) => landing.add(t.id)} />
  );
  return { start, dialogs };
}

// New apps started from this window, followed until their task ends. A
// module set, not pane state: the pane that started one unmounts when the
// user moves on, and the app still has to land.
const landing = new Set<string>();

/** useNewAppLanding lands on a new app when its creation finishes, with
 *  "Open site", wherever the user is by then; a failure or cancel says what
 *  was left. Mounted once, in the shell. */
export function useNewAppLanding(navigate: (route: Route) => void) {
  const { tasks } = useDaemonContext();
  const privacy = usePrivacy();
  useEffect(() => {
    for (const task of tasks ?? []) {
      if (!landing.has(task.id) || task.state === "running") continue;
      landing.delete(task.id);
      const realHost = `${task.target}.test`;
      const host = privacy.host(realHost);
      const log = task.log_file;
      const showLog = log ? { label: "Show log file", onClick: () => revealPath(log) } : undefined;
      if (task.state === "done") {
        navigate({ kind: "site", name: task.target, tab: "overview" });
        toast.success(`${host} is ready`, {
          action: { label: "Open site", onClick: () => openExternal(`https://${realHost}`) },
        });
      } else if (task.state === "failed") {
        toast.error(`Couldn't finish ${host}`, { description: task.error && privacy.text(task.error), action: showLog });
      } else {
        toast(`Stopped creating ${host}`, { description: sentence(task.error && privacy.text(task.error)), action: showLog });
      }
    }
  }, [tasks, navigate, privacy]);
}

function sentence(text?: string) {
  return text ? text[0].toUpperCase() + text.slice(1) + "." : undefined;
}

/** AddSiteButton opens the Add site dialog. */
export function AddSiteButton({ onClick, variant = "brand" }: { onClick: () => void; variant?: "brand" | "outline" }) {
  const { conn } = useDaemonContext();
  return (
    <Button variant={variant} onClick={onClick} disabled={!conn}>
      <PlusIcon data-icon="inline-start" />
      Add site
    </Button>
  );
}

const KINDS: Array<{ kind: AddSiteKind; icon: Icon; title: string; example: string }> = [
  { kind: "new", icon: SparkleIcon, title: "New Laravel app", example: "A starter kit, set up and served" },
  { kind: "link", icon: LinkSimpleIcon, title: "Link a project", example: "~/work/api as https://api.test" },
  { kind: "park", icon: FolderSimplePlusIcon, title: "Park a folder", example: "Every project inside it gets a URL" },
  { kind: "proxy", icon: ArrowsLeftRightIcon, title: "Proxy a local port", example: "Port 5173 as https://vite.test" },
];

function AddSiteDialog({
  open,
  onOpenChange,
  initialKind,
  onCreating,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  initialKind: AddSiteKind | null;
  onCreating: (task: Task) => void;
}) {
  const { services, setSites } = useDaemonContext();
  const { navigate } = useRoute();
  const privacy = usePrivacy();
  const {
    kind,
    index,
    back,
    answers,
    set,
    projectsDir,
    dir,
    setDir,
    tools,
    toolsError,
    retryTools,
    linkPath,
    setLinkPath,
    linkName,
    setLinkName,
    proxyName,
    setProxyName,
    proxyTarget,
    setProxyTarget,
    submitting,
    error,
    steps,
    step,
    taken,
    go,
    choose,
    post,
    run,
  } = useAddSiteFlow(open, initialKind);

  /** siteDone opens a linked or proxied site with the "Open site" toast;
   *  saved but not served is reported as partial (ui-standard F5). */
  const siteDone = (next: Site[], name: string, verb: string) => {
    setSites(next);
    const site = next.find((s) => s.name === name);
    onOpenChange(false);
    if (!site) return;
    navigate({ kind: "site", name: site.name, tab: "overview" });
    if (site.error) {
      // The site page shows the full reason; the toast only points there.
      toast.warning(`${privacy.host(site.host)} ${verb}, but Bench can't serve it yet`);
    } else {
      toast.success(`${privacy.host(site.host)} ${verb}`, { action: { label: "Open site", onClick: () => openExternal(site.url) } });
    }
  };

  const park = async () => {
    const picked = await pickDirectory({ directory: true, title: "Park a folder" });
    if (typeof picked !== "string") return;
    const before = new Set(taken);
    const next = await post<Site[]>("/api/sites/park", { path: picked });
    setSites(next);
    const added = next.filter((s) => !before.has(s.name));
    const unserved = added.filter((s) => s.error).length;
    const pickedName = privacy.folders ? MASK : folderName(picked);
    onOpenChange(false);
    navigate({ kind: "home", section: "sites" });
    if (unserved > 0) {
      // Saved but not all served: partial, not success (ui-standard F5).
      toast.warning(`${pickedName} parked, but Bench can't serve ${unserved} of its sites yet`, {
        description: "Open each site to see why.",
      });
    } else {
      toast.success(`${pickedName} parked`, {
        description:
          added.length > 0 ? `${added.length} site${added.length === 1 ? "" : "s"} added` : "It has no project folders yet.",
      });
    }
  };

  // Answered at once with the daemon task; progress, cancel and the result
  // live in Activity, so the dialog closes (ui-standard C3, C4).
  const create = async () => {
    const task = await post<Task>(
      "/api/sites/new",
      toRequest(answers, dir === projectsDir ? "" : dir),
      DEFAULT_TIMEOUT_MS,
    );
    onCreating(task);
    onOpenChange(false);
    toast(`Creating ${privacy.host(`${cleanName(answers.name)}.test`)}`);
  };

  const primary: { label: string; disabled: boolean; action: () => void } = (() => {
    switch (step) {
      case "choose":
        return { label: "Next", disabled: kind === null, action: () => go(1) };
      case "name":
        return { label: "Next", disabled: !!nameProblem(answers.name, taken) || !dir, action: () => go(index + 1) };
      case "link":
        return {
          label: `Link ${cleanName(linkName) || "site"}.test`,
          disabled: !linkPath || !!nameProblem(linkName, taken),
          action: () =>
            void run(async () =>
              siteDone(
                await post<Site[]>("/api/sites/link", { path: linkPath, name: cleanName(linkName) }),
                cleanName(linkName),
                "linked",
              ),
            ),
        };
      case "park":
        return { label: "Choose folder…", disabled: false, action: () => void run(park) };
      case "proxy":
        return {
          label: `Proxy ${cleanName(proxyName) || "site"}.test`,
          disabled: !!nameProblem(proxyName, taken) || !!targetError(proxyTarget),
          action: () =>
            void run(async () =>
              siteDone(
                await post<Site[]>("/api/sites/proxy", { name: cleanName(proxyName), target: proxyTarget.trim() }),
                cleanName(proxyName),
                "now proxies",
              ),
            ),
        };
      case "review":
        return { label: `Create ${cleanName(answers.name)}.test`, disabled: false, action: () => void run(create) };
      default:
        return { label: "Next", disabled: false, action: () => go(index + 1) };
    }
  })();

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (!primary.disabled && !submitting) primary.action();
  };
  // Enter moves on from any control: radio cards are buttons, which don't
  // submit a form themselves.
  const onKeyDown = (e: KeyboardEvent) => {
    if (e.key !== "Enter" || e.defaultPrevented || step === "choose") return;
    e.preventDefault();
    if (!primary.disabled && !submitting) primary.action();
  };

  const title = kind === null ? "Add site" : (KINDS.find((k) => k.kind === kind)?.title ?? "Add site");

  return (
    <Dialog open={open} onOpenChange={(o) => !submitting && onOpenChange(o)}>
      <DialogContent className="gap-5 sm:max-w-lg">
        <form onSubmit={onSubmit} onKeyDown={onKeyDown} className="flex flex-col gap-5">
          <DialogHeader className="gap-3">
            <div className="flex items-center justify-between gap-3 pr-8">
              <DialogTitle>{title}</DialogTitle>
              <span className="text-xs text-muted-foreground tabular-nums">
                Step {index + 1} of {steps.length}
              </span>
            </div>
            <Progress
              value={((index + 1) / steps.length) * 100}
              aria-label={`Step ${index + 1} of ${steps.length}`}
              className="h-1"
            />
            <DialogDescription className="sr-only">Add a site to Bench, one question at a time.</DialogDescription>
          </DialogHeader>

          <div
            key={step}
            className={cn(
              "flex min-h-64 flex-col gap-4 duration-150 animate-in fade-in-0 motion-reduce:animate-none",
              back ? "slide-in-from-left-4" : "slide-in-from-right-4",
            )}
          >
            <StepBody
              step={step}
              kind={kind}
              onChoose={choose}
              answers={answers}
              set={set}
              taken={taken}
              dir={dir}
              onDir={setDir}
              services={services ?? []}
              tools={tools}
              toolsError={toolsError}
              onToolsRetry={retryTools}
              link={{ path: linkPath, name: linkName, onName: setLinkName }}
              onLinkPick={(p) => {
                setLinkPath(p);
                setLinkName(suggestName(folderName(p)));
              }}
              proxy={{ name: proxyName, target: proxyTarget, onName: setProxyName, onTarget: setProxyTarget }}
            />
          </div>

          {error ? (
            <Alert variant="destructive">
              <AlertTitle>That didn't work</AlertTitle>
              <AlertDescription className="break-words whitespace-pre-wrap">{error}</AlertDescription>
            </Alert>
          ) : null}

          <DialogFooter>
            {index > 0 ? (
              <Button
                type="button"
                variant="ghost"
                onClick={() => go(index - 1)}
                disabled={submitting}
                className="sm:mr-auto"
              >
                <ArrowLeftIcon data-icon="inline-start" />
                Back
              </Button>
            ) : null}
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={submitting}>
              Cancel
            </Button>
            {step !== "choose" ? (
              <Button type="submit" disabled={primary.disabled || submitting}>
                {submitting ? <Spinner data-icon="inline-start" className="motion-reduce:animate-none" /> : null}
                {primary.label}
                {primary.label === "Next" && !submitting ? <ArrowRightIcon data-icon="inline-end" /> : null}
              </Button>
            ) : null}
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function StepBody({
  step,
  kind,
  onChoose,
  answers,
  set,
  taken,
  dir,
  onDir,
  services,
  tools,
  toolsError,
  onToolsRetry,
  link,
  onLinkPick,
  proxy,
}: {
  step: Step;
  kind: AddSiteKind | null;
  onChoose: (k: AddSiteKind) => void;
  answers: NewAppAnswers;
  set: (p: Partial<NewAppAnswers>) => void;
  taken: string[];
  dir: string;
  onDir: (dir: string) => void;
  services: Service[];
  tools: Tool[] | null;
  toolsError: string | null;
  onToolsRetry: () => void;
  link: { path: string | null; name: string; onName: (v: string) => void };
  onLinkPick: (path: string) => void;
  proxy: { name: string; target: string; onName: (v: string) => void; onTarget: (v: string) => void };
}) {
  switch (step) {
    case "choose":
      return <ChooseKind value={kind} onChoose={onChoose} />;
    case "name":
      return <NameStep answers={answers} set={set} taken={taken} dir={dir} onDir={onDir} />;
    case "kit":
      return (
        <Question title="Do you want to use a starter kit?">
          <Choices
            value={answers.starterKit ? "yes" : "no"}
            onChange={(v) => set({ starterKit: v === "yes", stack: v === "yes" ? "react" : "blade" })}
            options={[
              {
                value: "yes",
                label: "Yes",
                description: "Login, registration and a dashboard",
                media: <KitLogos />,
              },
              { value: "no", label: "No", description: "A plain Laravel app" },
            ]}
          />
        </Question>
      );
    case "stack":
      return (
        <Question
          title={
            answers.starterKit
              ? "Which frontend stack should your starter kit use?"
              : "Which frontend stack do you want to build on?"
          }
        >
          <Choices<Stack>
            value={answers.stack}
            onChange={(v) => set({ stack: v })}
            options={[
              ...(answers.starterKit
                ? []
                : [{ value: "blade" as const, label: "Blade", description: "Server-rendered views" }]),
              { value: "react", label: "React", description: "Inertia with React", media: <StackLogo stack="react" /> },
              {
                value: "svelte",
                label: "Svelte",
                description: "Inertia with Svelte",
                media: <StackLogo stack="svelte" />,
              },
              { value: "vue", label: "Vue", description: "Inertia with Vue", media: <StackLogo stack="vue" /> },
              {
                value: "livewire",
                label: "Livewire",
                description: "Reactive Blade components",
                media: <StackLogo stack="livewire" />,
              },
            ]}
          />
        </Question>
      );
    case "auth":
      return (
        <Question title="Which authentication provider do you prefer?">
          <Choices
            value={answers.auth}
            onChange={(v) => set({ auth: v })}
            options={[
              { value: "laravel", label: "Laravel's built-in authentication" },
              { value: "workos", label: "WorkOS", description: "Needs a WorkOS account" },
            ]}
          />
        </Question>
      );
    case "livewire":
      return (
        <Question title="Would you like to use single-file Livewire components?">
          <YesNo value={answers.singleFile} onChange={(v) => set({ singleFile: v })} />
        </Question>
      );
    case "teams":
      return (
        <Question title="Would you like to add teams support to your application?">
          <YesNo value={answers.teams} onChange={(v) => set({ teams: v })} />
        </Question>
      );
    case "testing":
      return (
        <Question title="Which testing framework do you prefer?">
          <Choices
            value={answers.testing}
            onChange={(v) => set({ testing: v })}
            options={[
              { value: "pest", label: "Pest" },
              { value: "phpunit", label: "PHPUnit" },
            ]}
          />
        </Question>
      );
    case "boost":
      return (
        <Question title="Do you want to install Laravel Boost to improve AI assisted coding?">
          <YesNo value={answers.boost} onChange={(v) => set({ boost: v })} />
        </Question>
      );
    case "database":
      return <DatabaseStep answers={answers} set={set} services={services} />;
    case "javascript":
      return (
        <JavaScriptStep
          answers={answers}
          set={set}
          tools={tools}
          toolsError={toolsError}
          onRetry={onToolsRetry}
        />
      );
    case "review":
      return <Review answers={answers} dir={dir} />;
    case "link":
      return <LinkStep path={link.path} name={link.name} taken={taken} onPick={onLinkPick} onName={link.onName} />;
    case "park":
      return (
        <Question
          title="Which folder?"
          tip="Each project folder inside it becomes a site: ~/Code/shop is served at https://shop.test."
        />
      );
    case "proxy":
      return <ProxyStep {...proxy} taken={taken} />;
  }
}

/** ChooseKind is the first step: four cards, one click moves on. */
function ChooseKind({ value, onChoose }: { value: AddSiteKind | null; onChoose: (k: AddSiteKind) => void }) {
  return (
    <div className="grid grid-cols-2 gap-3" role="group" aria-label="What do you want to add?">
      {KINDS.map((k) => (
        <button
          key={k.kind}
          type="button"
          onClick={() => onChoose(k.kind)}
          aria-pressed={value === k.kind}
          className={cn(
            "group flex flex-col items-start gap-3 rounded-xl bg-card p-4 text-left shadow-card outline-none",
            "transition-[translate,background-color,box-shadow] duration-150 ease-out hover:-translate-y-0.5 hover:bg-well",
            "focus-visible:ring-3 focus-visible:ring-ring aria-pressed:ring-2 aria-pressed:ring-brand",
            "motion-reduce:transition-none motion-reduce:hover:translate-y-0",
          )}
        >
          <span
            className={cn(
              "flex size-9 items-center justify-center rounded-lg ring-1 ring-inset",
              k.kind === "new" ? "bg-brand/10 text-brand ring-brand/20" : "bg-well text-muted-foreground ring-border",
            )}
          >
            <k.icon className="size-4.5" weight={k.kind === "new" ? "fill" : "regular"} />
          </span>
          <span className="flex flex-col gap-0.5">
            <span className="text-sm font-medium">{k.title}</span>
            <span className="text-xs text-muted-foreground">{k.example}</span>
          </span>
        </button>
      ))}
    </div>
  );
}
