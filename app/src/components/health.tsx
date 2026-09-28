import { Fragment } from "react";
import { ArrowsClockwiseIcon, ClipboardTextIcon, LockSimpleIcon, WarningCircleIcon } from "@phosphor-icons/react";
import { toast } from "sonner";
import { Alert, AlertAction, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { SetupAction } from "@/components/machine-actions";
import { useDaemonContext } from "@/hooks/daemon-provider";
import { useDoctor, useHealth } from "@/hooks/use-doctor";
import { useRoute } from "@/hooks/route";
import type { Check } from "@/lib/api";
import { errorMessage } from "@/lib/async";
import { openExternal } from "@/lib/open";
import { usePrivacy } from "@/lib/privacy";
import { cn } from "@/lib/utils";

// A URL, minus sentence punctuation after it ("see https://x.y.").
const URL_PATTERN = /(https?:\/\/[^\s)]*[^\s).,;:])/;

/** FixText renders daemon text, turning `backticked` spans into code so CLI
 *  commands read as commands instead of stray backticks, and URLs into links
 *  (ui-standard F6). */
export function FixText({ text }: { text: string }) {
  return (
    <>
      {text.split("`").map((part, i) =>
        i % 2 === 1 ? (
          <code key={i} className="rounded bg-muted px-1 py-0.5 font-mono text-[12px]">
            {part}
          </code>
        ) : (
          <Fragment key={i}>
            {part.split(URL_PATTERN).map((piece, j) =>
              j % 2 === 1 ? (
                <button
                  key={j}
                  type="button"
                  onClick={() => openExternal(piece)}
                  className="break-all text-brand underline-offset-2 outline-none hover:underline focus-visible:ring-3 focus-visible:ring-ring"
                >
                  {piece}
                </button>
              ) : (
                piece
              ),
            )}
          </Fragment>
        ),
      )}
    </>
  );
}

/** CheckSteps shows a check's fix as numbered steps, or as one line when
 *  there is a single step. */
export function CheckSteps({ steps, className }: { steps?: string[]; className?: string }) {
  if (!steps?.length) return null;
  if (steps.length === 1) {
    return (
      <div className={cn("text-foreground", className)}>
        <FixText text={steps[0]} />
      </div>
    );
  }
  return (
    <ol className={cn("flex list-decimal flex-col gap-0.5 pl-5 text-foreground marker:text-muted-foreground", className)}>
      {steps.map((step, i) => (
        <li key={i}>
          <FixText text={step} />
        </li>
      ))}
    </ol>
  );
}

/** RecheckButton runs the diagnostics again, with a pending state. */
export function RecheckButton({ size = "sm" }: { size?: "sm" | "default" }) {
  const { rerun, running } = useDoctor();
  return (
    <Button variant="outline" size={size} onClick={rerun} disabled={running}>
      {running ? (
        <Spinner data-icon="inline-start" className="motion-reduce:animate-none" />
      ) : (
        <ArrowsClockwiseIcon data-icon="inline-start" />
      )}
      Check again
    </Button>
  );
}

/** CheckAction is the button a check asks for (api Check.action). */
export function CheckAction({ check }: { check: Check }) {
  if (check.status === "ok") return null;
  if (check.action === "setup") {
    return (
      <SetupAction
        trigger={
          <Button variant="outline" size="sm">
            <LockSimpleIcon data-icon="inline-start" />
            Run setup
          </Button>
        }
      />
    );
  }
  if (check.action === "recheck") return <RecheckButton />;
  return null;
}

/** HealthAlert tells the truth where users look (ui-standard F4, O3): when
 *  Bench can't serve sites it says why, names Laravel Herd when that's the
 *  cause, and offers the next step. Not dismissible: it describes something
 *  still broken, and disappears once the checks pass. */
export function HealthAlert({ className }: { className?: string }) {
  const { canServe, needsSetup, herd } = useHealth();
  const { byId } = useDoctor();
  const { navigate } = useRoute();
  const { services } = useDaemonContext();
  const privacy = usePrivacy();
  const https = byId("https");
  // What Bench moved to stay out of Herd's way (a notice means the port
  // differs from the service's default), with lines apps need (UI-06).
  const moved = (services ?? []).filter((s) => s.notice);
  const copyMoved = () =>
    navigator.clipboard.writeText(moved.flatMap((s) => s.env).join("\n")).then(
      () => toast.success(".env lines copied"),
      (e: unknown) => toast.error("Couldn't copy", { description: errorMessage(e) }),
    );

  if (canServe === false && https) {
    return (
      <Alert variant="destructive" role="status" className={className}>
        <WarningCircleIcon />
        <AlertTitle>{herd ? "Laravel Herd is using port 443" : "Bench can't serve sites right now"}</AlertTitle>
        <AlertDescription>
          <div className="text-foreground">{privacy.text(https.detail)}</div>
          <CheckSteps steps={https.steps} className="mt-1.5" />
          {herd && moved.length > 0 ? (
            <div className="mt-1.5">
              Moved to free ports: {moved.map((s) => `${s.name} to ${s.port}`).join(", ")}. Apps need the new ports in
              their .env.
            </div>
          ) : null}
        </AlertDescription>
        <AlertAction className="flex items-center gap-2">
          {herd && moved.length > 0 ? (
            <Button variant="outline" size="sm" onClick={() => void copyMoved()}>
              <ClipboardTextIcon data-icon="inline-start" />
              Copy .env
            </Button>
          ) : null}
          <Button variant="ghost" size="sm" onClick={() => navigate({ kind: "home", section: "settings", settings: "diagnostics" })}>
            Details
          </Button>
          <RecheckButton />
        </AlertAction>
      </Alert>
    );
  }
  if (needsSetup) {
    return (
      <Alert variant="warning" role="status" className={className}>
        <LockSimpleIcon />
        <AlertTitle>Finish the HTTPS and DNS setup</AlertTitle>
        <AlertDescription className="text-foreground">
          Until then, *.test sites show browser warnings or don't load.
        </AlertDescription>
        <AlertAction>
          <SetupAction
            trigger={
              <Button variant="outline" size="sm">
                <LockSimpleIcon data-icon="inline-start" />
                Run setup
              </Button>
            }
          />
        </AlertAction>
      </Alert>
    );
  }
  return null;
}
