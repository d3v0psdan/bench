import { useId, useRef, useState, type ReactNode } from "react";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
import { Field, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
import { errorMessage, withTimeout } from "@/lib/async";

export interface ConfirmDetail {
  label: string;
  value: ReactNode;
}

const CONFIRM_TIMEOUT_MS = 60_000;

/** ConfirmAction is the only way to run something costly or hard to undo
 *  (docs/ui-standard.md C1): it names the action, lists exactly what will
 *  happen, and runs onConfirm only after an explicit yes. The dialog stays
 *  open with a spinner until onConfirm settles; a failure shows inside it.
 *  Long work should start in onConfirm and continue in Activity, so
 *  onConfirm resolves as soon as the work is under way.
 *
 *  Pass `trigger` for a button that opens it, or `open`/`onOpenChange` to
 *  open it from elsewhere (a menu item). Pass `confirmText` for the worst
 *  cases: the confirm stays disabled until it is typed exactly. */
export function ConfirmAction({
  trigger,
  open,
  onOpenChange,
  title,
  description,
  details,
  children,
  confirmLabel,
  confirmIcon,
  confirmText,
  confirmTextHint,
  tone = "default",
  timeoutMs = CONFIRM_TIMEOUT_MS,
  onConfirm,
}: {
  trigger?: ReactNode;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  title: string;
  description: ReactNode;
  details?: ConfirmDetail[];
  /** Extra controls, e.g. a "keep data" switch. */
  children?: ReactNode;
  /** A specific verb: "Install PHP 8.4", "Delete mysql". Never "OK". */
  confirmLabel: string;
  confirmIcon?: ReactNode;
  /** Text to retype before confirming, e.g. the site's host. */
  confirmText?: string;
  /** Names confirmText without printing it ("the site's name"), for text
   *  privacy mode hides; the box then has no placeholder either. */
  confirmTextHint?: string;
  tone?: "default" | "destructive";
  /** How long to wait for onConfirm before showing a timeout error. */
  timeoutMs?: number;
  onConfirm: () => Promise<unknown> | void;
}) {
  const [innerOpen, setInnerOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [typed, setTyped] = useState("");
  const [wasOpen, setWasOpen] = useState(false);
  const typedId = useId();
  const typedRef = useRef<HTMLInputElement>(null);
  const isOpen = open ?? innerOpen;
  const typedMatches = confirmText === undefined || typed === confirmText;
  // Start every opening clean (no old error, an empty box), however it
  // was opened: by the trigger or by the parent's `open`.
  if (isOpen !== wasOpen) {
    setWasOpen(isOpen);
    if (isOpen) {
      setError(null);
      setTyped("");
    }
  }

  const setOpen = (next: boolean) => {
    if (busy) return; // stay until the action settles
    setInnerOpen(next);
    onOpenChange?.(next);
  };

  const runConfirmed = async () => {
    setBusy(true);
    setError(null);
    try {
      await withTimeout(Promise.resolve(onConfirm()), timeoutMs, `${confirmLabel} is taking too long`);
      setBusy(false);
      setInnerOpen(false);
      onOpenChange?.(false);
    } catch (e) {
      setBusy(false);
      setError(errorMessage(e));
    }
  };

  return (
    <AlertDialog open={isOpen} onOpenChange={setOpen}>
      {trigger ? <AlertDialogTrigger asChild>{trigger}</AlertDialogTrigger> : null}
      <AlertDialogContent
        onOpenAutoFocus={(e) => {
          if (confirmText === undefined) return; // default: focus Cancel
          e.preventDefault();
          typedRef.current?.focus();
        }}
      >
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          <AlertDialogDescription>{description}</AlertDialogDescription>
        </AlertDialogHeader>
        {details && details.length > 0 ? (
          <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-6 gap-y-2 rounded-lg bg-well px-4 py-3 text-[13px]">
            {details.map((d) => (
              <div key={d.label} className="contents">
                <dt className="text-muted-foreground">{d.label}</dt>
                <dd className="min-w-0 break-words">{d.value}</dd>
              </div>
            ))}
          </dl>
        ) : null}
        {children}
        {confirmText !== undefined ? (
          <Field>
            <FieldLabel htmlFor={typedId}>
              <span>
                Type {confirmTextHint ?? <span className="font-mono">{confirmText}</span>} to confirm
              </span>
            </FieldLabel>
            <Input
              ref={typedRef}
              id={typedId}
              value={typed}
              onChange={(e) => setTyped(e.target.value)}
              onKeyDown={(e) => {
                if (e.key !== "Enter") return;
                e.preventDefault();
                if (typedMatches && !busy) void runConfirmed();
              }}
              placeholder={confirmTextHint === undefined ? confirmText : undefined}
              disabled={busy}
              autoComplete="off"
              spellCheck={false}
            />
          </Field>
        ) : null}
        {error ? (
          <p role="alert" className="text-[13px] break-words text-destructive">
            {error}
          </p>
        ) : null}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={busy}>Cancel</AlertDialogCancel>
          <AlertDialogAction
            variant={tone === "destructive" ? "destructive-solid" : "default"}
            disabled={busy || !typedMatches}
            onClick={(e) => {
              e.preventDefault(); // close only after onConfirm settles
              void runConfirmed();
            }}
          >
            {busy ? <Spinner data-icon="inline-start" className="motion-reduce:animate-none" /> : confirmIcon}
            {confirmLabel}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
