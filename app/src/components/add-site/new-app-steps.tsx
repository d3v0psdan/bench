import { useState, type ReactNode } from "react";
import { open as pickDirectory } from "@tauri-apps/plugin-dialog";
import { DiceFiveIcon } from "@phosphor-icons/react";
import { InfoTip } from "@/components/info-tip";
import { Button } from "@/components/ui/button";
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldTitle,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { Service } from "@/lib/api";
import { stepsFor, type Database, type NewAppAnswers, type Stack } from "@/lib/new-app-steps";
import { nameProblem, randomName } from "@/lib/site-name";
import { cn } from "@/lib/utils";
import livewireLogo from "../stack-logos/livewire.svg";
import reactLogo from "../stack-logos/react.svg";
import svelteLogo from "../stack-logos/svelte.svg";
import vueLogo from "../stack-logos/vue.svg";
import type { Tool } from "./use-add-site-flow";

export const cleanName = (n: string) =>
  n
    .trim()
    .toLowerCase()
    .replace(/\.test$/, "");

/** Question is one step's heading; tip is secondary help behind an info icon. */
export function Question({ title, tip, children }: { title: string; tip?: string; children?: ReactNode }) {
  return (
    <>
      <div className="flex items-center gap-1">
        <h3 className="text-base font-medium">{title}</h3>
        {tip ? <InfoTip label="More about this question">{tip}</InfoTip> : null}
      </div>
      {children}
    </>
  );
}

interface Option<T extends string> {
  value: T;
  label: string;
  description?: string;
  /** Why it can't be picked, shown instead of the description. */
  disabled?: string;
  /** A logo or picture shown before the label. */
  media?: ReactNode;
}

// The starter kits' logos, as their projects publish them.
const STACK_LOGOS: Partial<Record<Stack, string>> = {
  react: reactLogo,
  svelte: svelteLogo,
  vue: vueLogo,
  livewire: livewireLogo,
};

export function StackLogo({ stack, size = "md" }: { stack: Stack; size?: "sm" | "md" }) {
  const src = STACK_LOGOS[stack];
  if (!src) return null;
  return (
    <img
      src={src}
      alt=""
      aria-hidden
      draggable={false}
      className={cn("shrink-0 object-contain", size === "sm" ? "size-4" : "size-7")}
    />
  );
}

/** KitLogos is the four starter-kit stacks, overlapping. */
export function KitLogos() {
  return (
    <span className="flex [&>*+*]:-ml-1.5" aria-hidden>
      {(["react", "vue", "svelte", "livewire"] as const).map((s) => (
        <span key={s} className="flex size-6 items-center justify-center rounded-full bg-card ring-2 ring-well">
          <StackLogo stack={s} size="sm" />
        </span>
      ))}
    </span>
  );
}

/** Choices is one question's answers as radio cards; arrow keys move. */
export function Choices<T extends string>({
  value,
  onChange,
  options,
}: {
  value: T;
  onChange: (v: T) => void;
  options: Option<T>[];
}) {
  return (
    // The radio values are the options' values, so v is always a T.
    <RadioGroup value={value} onValueChange={(v) => onChange(v as T)} className="grid-cols-2 gap-2">
      {options.map((o) => (
        <FieldLabel key={o.value || "none"} htmlFor={`choice-${o.value || "none"}`} className="cursor-pointer">
          <Field orientation="horizontal" data-disabled={o.disabled ? true : undefined}>
            {o.media ? <span className="flex shrink-0 items-center">{o.media}</span> : null}
            <FieldContent>
              <FieldTitle>{o.label}</FieldTitle>
              {o.disabled || o.description ? (
                <FieldDescription className={cn("text-xs", o.disabled && "text-destructive")}>
                  {o.disabled ?? o.description}
                </FieldDescription>
              ) : null}
            </FieldContent>
            <RadioGroupItem value={o.value} id={`choice-${o.value || "none"}`} disabled={!!o.disabled} />
          </Field>
        </FieldLabel>
      ))}
    </RadioGroup>
  );
}

export function YesNo({ value, onChange }: { value: boolean; onChange: (v: boolean) => void }) {
  return (
    <Choices
      value={value ? "yes" : "no"}
      onChange={(v) => onChange(v === "yes")}
      options={[
        { value: "yes", label: "Yes" },
        { value: "no", label: "No" },
      ]}
    />
  );
}

function joinPath(dir: string, name: string) {
  const sep = dir.includes("\\") ? "\\" : "/";
  return `${dir.replace(/[\\/]+$/, "")}${sep}${name}`;
}

export function NameStep({
  answers,
  set,
  taken,
  dir,
  onDir,
}: {
  answers: NewAppAnswers;
  set: (p: Partial<NewAppAnswers>) => void;
  taken: string[];
  dir: string;
  onDir: (dir: string) => void;
}) {
  const problem = answers.name === "" ? null : nameProblem(answers.name, taken);
  const name = cleanName(answers.name) || "example-app";
  const [rolls, setRolls] = useState(0); // restarts the dice spin per roll
  const change = async () => {
    const picked = await pickDirectory({ directory: true, title: "Create the app in" });
    if (typeof picked === "string") onDir(picked);
  };
  return (
    <Question title="What is the name of your project?">
      <Field data-invalid={!!problem || undefined}>
        <FieldLabel htmlFor="new-app-name" className="sr-only">
          Project name
        </FieldLabel>
        <div className="flex items-center gap-2">
          <Input
            id="new-app-name"
            value={answers.name}
            onChange={(e) => set({ name: e.target.value })}
            placeholder="example-app"
            aria-invalid={!!problem || undefined}
            autoComplete="off"
            spellCheck={false}
            autoFocus
          />
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                type="button"
                variant="outline"
                size="icon"
                aria-label="Suggest a random name"
                onClick={() => {
                  setRolls((n) => n + 1);
                  set({ name: randomName(taken) });
                }}
              >
                <DiceFiveIcon key={rolls} className="duration-300 animate-in spin-in-180 motion-reduce:animate-none" />
              </Button>
            </TooltipTrigger>
            <TooltipContent>Random name</TooltipContent>
          </Tooltip>
        </div>
        {problem ? <FieldError>{problem}</FieldError> : null}
      </Field>
      <dl className="grid grid-cols-[auto_minmax(0,1fr)] items-center gap-x-4 gap-y-2 rounded-lg bg-well p-3 text-[13px]">
        <dt className="text-muted-foreground">URL</dt>
        <dd className="truncate font-mono text-xs">https://{name}.test</dd>
        <dt className="text-muted-foreground">Folder</dt>
        <dd className="flex min-w-0 items-center gap-2">
          <span className="truncate font-mono text-xs">{dir ? joinPath(dir, name) : "Loading…"}</span>
          <Button type="button" variant="ghost" size="xs" onClick={() => void change()} className="ml-auto shrink-0">
            Change…
          </Button>
        </dd>
      </dl>
    </Question>
  );
}

const DB_KINDS: Record<Exclude<Database, "sqlite">, string> = {
  mysql: "mysql",
  mariadb: "mariadb",
  pgsql: "postgresql",
};
const DB_LABELS: Record<Database, string> = {
  sqlite: "SQLite",
  mysql: "MySQL",
  mariadb: "MariaDB",
  pgsql: "PostgreSQL",
};

export function DatabaseStep({
  answers,
  set,
  services,
}: {
  answers: NewAppAnswers;
  set: (p: Partial<NewAppAnswers>) => void;
  services: Service[];
}) {
  const kind = answers.database === "sqlite" ? null : DB_KINDS[answers.database];
  const instances = kind ? services.filter((s) => s.service === kind) : [];
  const newName = `${cleanName(answers.name) || "app"}-${kind}`;
  // Picking a server database defaults to a running instance of that kind.
  const pick = (database: Database) => {
    const k = database === "sqlite" ? null : DB_KINDS[database];
    const same = k ? services.filter((s) => s.service === k) : [];
    const existing = same.find((s) => s.state === "running") ?? same[0];
    set({ database, service: existing?.name ?? "" });
  };
  return (
    <Question title="Which database will your application use?">
      <Choices<Database>
        value={answers.database}
        onChange={pick}
        options={[
          { value: "sqlite", label: "SQLite", description: "No server needed" },
          { value: "mysql", label: "MySQL" },
          { value: "mariadb", label: "MariaDB" },
          { value: "pgsql", label: "PostgreSQL" },
        ]}
      />
      {kind ? (
        <FieldGroup className="gap-3">
          <Field orientation="horizontal">
            <FieldLabel htmlFor="new-app-service" className="font-normal text-muted-foreground">
              Instance
            </FieldLabel>
            <Select value={answers.service || "__new"} onValueChange={(v) => set({ service: v === "__new" ? "" : v })}>
              <SelectTrigger id="new-app-service" className="ml-auto w-60">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  {instances.map((s) => (
                    <SelectItem key={s.name} value={s.name}>
                      {s.name}
                      {s.state !== "running" ? " (starts it)" : ""}
                    </SelectItem>
                  ))}
                  <SelectItem value="__new">Create {newName}</SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
          </Field>
          <Field orientation="horizontal">
            <div className="flex items-center gap-1">
              <FieldLabel htmlFor="new-app-migrate">Run the default migrations</FieldLabel>
              <InfoTip label="About the default migrations">Creates the users, cache and jobs tables.</InfoTip>
            </div>
            <Switch id="new-app-migrate" checked={answers.migrate} onCheckedChange={(v) => set({ migrate: v })} />
          </Field>
        </FieldGroup>
      ) : null}
    </Question>
  );
}

export function JavaScriptStep({
  answers,
  set,
  tools,
  toolsError,
  onRetry,
}: {
  answers: NewAppAnswers;
  set: (p: Partial<NewAppAnswers>) => void;
  tools: Tool[] | null;
  toolsError: string | null;
  onRetry: () => void;
}) {
  const found = (n: string) => tools?.find((t) => t.name === n && t.path);
  const missing = (n: string) => (tools === null || found(n) ? undefined : `${n} isn't installed`);
  const version = (n: string) => {
    const v = found(n)?.version;
    return v ? `Version ${v}` : undefined;
  };
  return (
    <Question
      title="Which package manager should install and build the front end?"
      tip="Bench uses the Node or bun already on this machine and never installs its own."
    >
      <Choices<"npm" | "bun" | "">
        value={answers.packageManager}
        onChange={(v) => set({ packageManager: v })}
        options={[
          { value: "npm", label: "npm", description: version("npm"), disabled: missing("npm") },
          { value: "bun", label: "bun", description: version("bun"), disabled: missing("bun") },
          { value: "", label: "Skip for now", description: "Run install and build yourself" },
        ]}
      />
      {toolsError ? (
        <div className="mt-3 flex items-center gap-3">
          <p role="alert" className="min-w-0 flex-1 text-xs text-destructive">
            Couldn't check which tools are installed: {toolsError}
          </p>
          <Button variant="outline" size="sm" onClick={onRetry}>
            Try again
          </Button>
        </div>
      ) : null}
    </Question>
  );
}

export function Review({ answers: a, dir }: { answers: NewAppAnswers; dir: string }) {
  const name = cleanName(a.name);
  const steps = stepsFor(a);
  const stack = a.stack[0].toUpperCase() + a.stack.slice(1);
  const database =
    a.database === "sqlite"
      ? "SQLite"
      : `${DB_LABELS[a.database]} on ${a.service || `a new instance, ${name}-${DB_KINDS[a.database]}`}${a.migrate ? ", migrated" : ""}`;
  const rows: Array<[string, string]> = [
    ["URL", `https://${name}.test`],
    ["Folder", joinPath(dir, name)],
    [
      "Stack",
      a.starterKit
        ? `${stack} starter kit, ${a.auth === "workos" ? "WorkOS" : "Laravel"} auth`
        : `${stack}, no starter kit`,
    ],
    ...(steps.includes("livewire") ? [["Components", a.singleFile ? "Single-file" : "Class"] as [string, string]] : []),
    ...(steps.includes("teams") ? [["Teams", a.teams ? "Yes" : "No"] as [string, string]] : []),
    ["Testing", a.testing === "pest" ? "Pest" : "PHPUnit"],
    ["Laravel Boost", a.boost ? "Yes" : "No"],
    ["Database", database],
    ["Front end", a.packageManager ? `${a.packageManager} install and build` : "Skipped"],
  ];
  // Consent (ui-standard C1): name what gets downloaded before it happens.
  const downloads = [
    "Composer and PHP, if they aren't installed yet",
    "Laravel and its packages from Packagist",
    ...(a.packageManager ? [`JavaScript packages through ${a.packageManager}`] : []),
    ...(a.database !== "sqlite" && !a.service ? [`${DB_LABELS[a.database]}, if it isn't installed yet`] : []),
  ];
  return (
    <Question title={`Create ${name}.test?`}>
      <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-2 rounded-lg bg-well p-3 text-[13px]">
        {rows.map(([k, v]) => (
          <div key={k} className="contents">
            <dt className="text-muted-foreground">{k}</dt>
            <dd
              className={cn(
                "flex min-w-0 items-center gap-1.5",
                (k === "URL" || k === "Folder") && "font-mono text-xs",
              )}
            >
              {k === "Stack" ? <StackLogo stack={a.stack} size="sm" /> : null}
              <span className="truncate">{v}</span>
            </dd>
          </div>
        ))}
      </dl>
      <div className="text-xs text-muted-foreground">
        <p className="mb-1 font-medium text-foreground">Downloads</p>
        <ul className="list-inside list-disc">
          {downloads.map((d) => (
            <li key={d}>{d}</li>
          ))}
        </ul>
      </div>
    </Question>
  );
}
