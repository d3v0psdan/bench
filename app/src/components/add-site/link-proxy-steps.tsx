import { open as pickDirectory } from "@tauri-apps/plugin-dialog";
import { Button } from "@/components/ui/button";
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { nameProblem } from "@/lib/site-name";
import { cn } from "@/lib/utils";
import { cleanName, Question } from "./new-app-steps";

export function LinkStep({
  path,
  name,
  taken,
  onPick,
  onName,
}: {
  path: string | null;
  name: string;
  taken: string[];
  onPick: (path: string) => void;
  onName: (name: string) => void;
}) {
  const pick = async () => {
    const picked = await pickDirectory({ directory: true, title: "Link a project" });
    if (typeof picked === "string") onPick(picked);
  };
  const problem = name === "" ? null : nameProblem(name, taken);
  const host = `${cleanName(name) || "name"}.test`;
  return (
    <Question title="Which project?">
      <div className="flex items-center gap-2 rounded-lg bg-well p-3">
        <span className={cn("min-w-0 flex-1 truncate font-mono text-xs", !path && "text-muted-foreground")}>
          {path ?? "No folder chosen yet"}
        </span>
        <Button type="button" variant="outline" size="sm" onClick={() => void pick()} autoFocus={!path}>
          {path ? "Change…" : "Choose folder…"}
        </Button>
      </div>
      {path ? (
        <Field data-invalid={!!problem || undefined}>
          <FieldLabel htmlFor="link-name">Site name</FieldLabel>
          <Input
            id="link-name"
            value={name}
            onChange={(e) => onName(e.target.value)}
            placeholder="shop"
            aria-invalid={!!problem || undefined}
            autoComplete="off"
            spellCheck={false}
            autoFocus
          />
          {problem ? (
            <FieldError>{problem}</FieldError>
          ) : (
            <FieldDescription>
              Serves at <span className="font-mono text-foreground">https://{host}</span>
            </FieldDescription>
          )}
        </Field>
      ) : null}
    </Question>
  );
}

export function ProxyStep({
  name,
  target,
  taken,
  onName,
  onTarget,
}: {
  name: string;
  target: string;
  taken: string[];
  onName: (v: string) => void;
  onTarget: (v: string) => void;
}) {
  const problem = name === "" ? null : nameProblem(name, taken);
  const targetProblem = target === "" ? null : targetError(target);
  const host = `${cleanName(name) || "name"}.test`;
  return (
    <Question title="Which port?">
      <FieldGroup>
        <Field data-invalid={!!problem || undefined}>
          <FieldLabel htmlFor="proxy-name">Site name</FieldLabel>
          <Input
            id="proxy-name"
            value={name}
            onChange={(e) => onName(e.target.value)}
            placeholder="vite"
            aria-invalid={!!problem || undefined}
            autoComplete="off"
            spellCheck={false}
            autoFocus
          />
          {problem ? (
            <FieldError>{problem}</FieldError>
          ) : (
            <FieldDescription>
              Serves at <span className="font-mono text-foreground">https://{host}</span>
            </FieldDescription>
          )}
        </Field>
        <Field data-invalid={!!targetProblem || undefined}>
          <FieldLabel htmlFor="proxy-target">Forward to</FieldLabel>
          <Input
            id="proxy-target"
            value={target}
            onChange={(e) => onTarget(e.target.value)}
            placeholder="5173 or 127.0.0.1:5173"
            aria-invalid={!!targetProblem || undefined}
            autoComplete="off"
            spellCheck={false}
          />
          {targetProblem ? <FieldError>{targetProblem}</FieldError> : null}
        </Field>
      </FieldGroup>
    </Question>
  );
}

/** targetError mirrors the daemon's check: a port, or host:port. */
export function targetError(target: string): string | null {
  const t = target.trim();
  if (t === "") return "Enter a port.";
  const port = t.includes(":") ? t.slice(t.lastIndexOf(":") + 1) : t;
  const n = Number(port);
  if (!/^\d+$/.test(port) || n < 1 || n > 65535) return "Use a port from 1 to 65535, or host:port.";
  return null;
}
