import { useRef, useState, type ReactNode } from "react";
import {
  CaretDownIcon,
  CheckCircleIcon,
  DatabaseIcon,
  FileTextIcon,
  GlobeIcon,
  LockSimpleIcon,
  WarningCircleIcon,
  XIcon,
} from "@phosphor-icons/react";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import { Spinner } from "@/components/ui/spinner";
import { useAddSite } from "@/components/add-site/add-site";
import { EnvUpdateDialog } from "@/components/env-update";
import { RecheckButton } from "@/components/health";
import { SetupAction } from "@/components/machine-actions";
import { InstallButton } from "@/components/php-pane";
import { CreateServiceDialog } from "@/components/service-dialogs";
import { useDaemonContext } from "@/hooks/daemon-provider";
import { useDoctor, useHealth } from "@/hooks/use-doctor";
import { useRoute } from "@/hooks/route";
import { usePrivacy } from "@/lib/privacy";
import { cn } from "@/lib/utils";

const DATABASES = new Set(["mysql", "mariadb", "postgresql"]);
const COLLAPSED_KEY = "bench.getStartedCollapsed";
const COMPLETE_KEY = "bench.setupCompleteHidden";

interface Step {
  id: string;
  title: string;
  description?: ReactNode;
  /** true done, false to do, null still checking */
  done: boolean | null;
  action?: ReactNode;
  /** Why the step couldn't be checked, and how to try again. */
  failure?: { message: string; retry: ReactNode };
}

/** GetStarted is the first-run path (ui-standard O1): every step's state is
 *  read from the daemon, every step carries its own action, and the guide
 *  can be collapsed but not dismissed while a step is open. It disappears
 *  once everything is done, and comes back if something breaks later. */
export function GetStarted() {
  const { sites, services, binaries, defaultPhp, loadError, reloadData } = useDaemonContext();
  const { canServe, needsSetup, herd, error: checkError } = useHealth();
  const { byId } = useDoctor();
  const { navigate } = useRoute();
  const [collapsed, setCollapsed] = useState(() => localStorage.getItem(COLLAPSED_KEY) === "1");
  const [dbOpen, setDbOpen] = useState(false);
  const [envOpen, setEnvOpen] = useState(false);
  const [completeHidden, setCompleteHidden] = useState(() => localStorage.getItem(COMPLETE_KEY) === "1");
  const add = useAddSite();
  const privacy = usePrivacy();
  const firstSite = sites?.find((s) => s.kind !== "proxy");
  // An app is connected once its .env points at a Bench database.
  const dbPorts = (services ?? []).filter((s) => DATABASES.has(s.service)).map((s) => s.port);
  const connected = sites?.some((s) => s.env_ports?.some((p) => dbPorts.includes(p))) ?? false;

  const php = binaries?.find((b) => b.name === "php" && b.channel === defaultPhp);
  const checkFailure = checkError ? { message: checkError, retry: <RecheckButton /> } : undefined;
  const loadFailure = loadError
    ? {
        message: loadError,
        retry: (
          <Button variant="outline" size="sm" onClick={reloadData}>
            Try again
          </Button>
        ),
      }
    : undefined;
  const steps: Step[] = [
    {
      id: "running",
      title: "Bench is running",
      done: true,
    },
    {
      id: "port",
      title: herd && canServe === false ? "Quit Laravel Herd" : "Free port 443",
      description:
        canServe === false
          ? herd
            ? "Herd is using port 443. Quit it from its tray icon, then check again."
            : privacy.text(byId("https")?.detail ?? "Another program is using port 443.")
          : undefined,
      done: canServe,
      action: <RecheckButton />,
      failure: checkFailure,
    },
    {
      id: "setup",
      title: "Trust HTTPS and route *.test",
      description: "Asks for administrator rights once.",
      done: needsSetup === null ? null : !needsSetup,
      failure: checkFailure,
      action: (
        <SetupAction
          trigger={
            <Button variant="outline" size="sm">
              <LockSimpleIcon data-icon="inline-start" />
              Run setup
            </Button>
          }
        />
      ),
    },
    {
      id: "php",
      title: php ? `Install PHP ${php.channel}` : "Install PHP",
      description: "The default for your sites.",
      done: binaries === null ? null : !!php?.installed,
      failure: loadFailure,
      action: php ? (
        <InstallButton build={php} />
      ) : (
        <Button variant="outline" size="sm" onClick={() => navigate({ kind: "home", section: "php" })}>
          Choose a version
        </Button>
      ),
    },
    {
      id: "site",
      title: "Add site",
      done: sites === null ? null : sites.length > 0,
      failure: loadFailure,
      action: (
        <Button variant="outline" size="sm" onClick={() => add.start()}>
          <GlobeIcon data-icon="inline-start" />
          Add site
        </Button>
      ),
    },
    {
      id: "database",
      title: "Add a database",
      description: "MySQL, MariaDB or PostgreSQL.",
      done: services === null ? null : services.some((s) => DATABASES.has(s.service)),
      failure: loadFailure,
      action: (
        <Button variant="outline" size="sm" onClick={() => setDbOpen(true)}>
          <DatabaseIcon data-icon="inline-start" />
          New service
        </Button>
      ),
    },
    {
      id: "connect",
      title: "Connect your app",
      description: firstSite
        ? `Put the database settings in ${privacy.host(firstSite.host)}'s .env.`
        : "Add a site and a database first.",
      done: sites === null || services === null ? null : connected,
      failure: loadFailure,
      action: firstSite ? (
        <Button variant="outline" size="sm" onClick={() => setEnvOpen(true)}>
          <FileTextIcon data-icon="inline-start" />
          Update .env
        </Button>
      ) : undefined,
    },
  ];

  const doneCount = steps.filter((s) => s.done === true).length;
  if (doneCount === steps.length) {
    // Informational now, so it can go for good (ui-standard O2); it comes
    // back if a step breaks later, because then this branch isn't taken.
    if (completeHidden) return null;
    return (
      <div role="status" className="mb-8 flex items-center gap-2 rounded-xl border border-subtle bg-well px-4 py-2.5 text-[13px]">
        <CheckCircleIcon weight="fill" className="size-4 text-success-solid" />
        <span className="flex-1">Setup complete.</span>
        <Button
          variant="ghost"
          size="icon-xs"
          aria-label="Hide setup complete"
          onClick={() => {
            localStorage.setItem(COMPLETE_KEY, "1");
            setCompleteHidden(true);
          }}
        >
          <XIcon />
        </Button>
      </div>
    );
  }
  const next = steps.find((s) => s.done === false);

  const toggle = () => {
    localStorage.setItem(COLLAPSED_KEY, collapsed ? "0" : "1");
    setCollapsed(!collapsed);
  };

  return (
    <section aria-labelledby="get-started" className="mb-8 rounded-xl border border-subtle bg-well bg-brand-glow p-1">
      <header className="flex items-center gap-4 px-4 pt-3 pb-3.5">
        <div className="min-w-0 flex-1">
          <h2 id="get-started" className="text-sm font-medium">
            Get started
          </h2>
          <p className="mt-0.5 text-[13px] text-muted-foreground">
            {doneCount} of {steps.length} done{next && collapsed ? ` · next: ${next.title}` : ""}
          </p>
        </div>
        <Progress className="h-1.5 w-32" value={(doneCount / steps.length) * 100} aria-label="Setup progress" />
        <Button
          variant="ghost"
          size="icon-sm"
          aria-expanded={!collapsed}
          aria-label={collapsed ? "Show steps" : "Hide steps"}
          onClick={toggle}
        >
          <CaretDownIcon className={cn("transition-transform duration-150 motion-reduce:transition-none", collapsed && "-rotate-90")} />
        </Button>
      </header>
      {collapsed ? null : (
        <ol className="divide-y overflow-hidden rounded-lg bg-card shadow-card">
          {steps.map((step, i) => (
            <li
              key={step.id}
              className={cn(
                "relative flex items-center gap-4 px-4 py-3.5 transition-colors duration-200",
                step === next && "bg-brand/[0.035] before:absolute before:inset-y-2 before:left-0 before:w-0.5 before:rounded-full before:bg-brand",
              )}
            >
              <StepMarker
                index={i + 1}
                done={step.done}
                current={step === next}
                failed={step.done === null && !!step.failure}
              />
              <div className="min-w-0 flex-1">
                <p className={cn("text-sm font-medium", step.done && "text-muted-foreground")}>{step.title}</p>
                <StepNote step={step} />
              </div>
              {step.done === false ? (
                <div className="shrink-0">{step.action}</div>
              ) : step.done === null && step.failure ? (
                <div className="shrink-0">{step.failure.retry}</div>
              ) : null}
            </li>
          ))}
        </ol>
      )}
      <CreateServiceDialog
        open={dbOpen}
        onOpenChange={setDbOpen}
        initialService="mysql"
        initialName={firstSite ? `${firstSite.name}-db` : undefined}
      />
      {firstSite ? <EnvUpdateDialog site={firstSite} open={envOpen} onOpenChange={setEnvOpen} /> : null}
      {add.dialogs}
    </section>
  );
}

/** StepNote is the line under an open step: why it couldn't be checked, or its description. */
function StepNote({ step }: { step: Step }) {
  if (step.done) return null;
  const note = step.done === null && step.failure ? `Couldn't check this: ${step.failure.message}` : step.description;
  if (!note) return null;
  return <p className="mt-0.5 text-[13px] text-muted-foreground">{note}</p>;
}

function StepMarker({
  index,
  done,
  current,
  failed,
}: {
  index: number;
  done: boolean | null;
  current: boolean;
  /** The check behind this step couldn't run. */
  failed: boolean;
}) {
  // Pop the check only when a step completes while the list is on screen,
  // not for steps that were already done when it appeared.
  const doneOnMount = useRef(done);
  if (failed) {
    return <WarningCircleIcon weight="fill" className="size-6 shrink-0 text-warning-solid" aria-label="Couldn't check" />;
  }
  if (done === null) {
    return (
      <span className="flex size-6 shrink-0 items-center justify-center" aria-label="Checking">
        <Spinner className="size-4 text-muted-foreground motion-reduce:animate-none" />
      </span>
    );
  }
  if (done) {
    return (
      <CheckCircleIcon
        weight="fill"
        className={cn(
          "size-6 shrink-0 text-success-solid",
          !doneOnMount.current && "animate-in duration-300 ease-out zoom-in-50 fade-in-0 motion-reduce:animate-none",
        )}
        aria-label="Done"
      />
    );
  }
  return (
    <span
      aria-label={`Step ${index}`}
      className={cn(
        "flex size-6 shrink-0 items-center justify-center rounded-full text-xs font-semibold tabular-nums ring-1 ring-inset",
        current ? "bg-brand text-brand-foreground ring-brand" : "bg-card text-muted-foreground ring-border",
      )}
    >
      {index}
    </span>
  );
}
