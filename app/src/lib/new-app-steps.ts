// The "New Laravel app" wizard asks the Laravel installer's questions in
// its order (laravel/installer NewCommand.php); these decide which apply.

export type Stack = "blade" | "react" | "svelte" | "vue" | "livewire";
export type Database = "sqlite" | "mysql" | "mariadb" | "pgsql";

export interface NewAppAnswers {
  name: string;
  starterKit: boolean;
  stack: Stack;
  auth: "laravel" | "workos";
  singleFile: boolean;
  teams: boolean;
  testing: "pest" | "phpunit";
  boost: boolean;
  database: Database;
  /** A Bench instance to use; "" creates one. */
  service: string;
  migrate: boolean;
  /** "" skips install and build. */
  packageManager: "npm" | "bun" | "";
}

export type NewAppStep =
  "name" | "kit" | "stack" | "auth" | "livewire" | "teams" | "testing" | "boost" | "database" | "javascript" | "review";

export const DEFAULT_ANSWERS: NewAppAnswers = {
  name: "",
  starterKit: true,
  stack: "react",
  auth: "laravel",
  singleFile: true,
  teams: false,
  testing: "pest",
  boost: true,
  database: "sqlite",
  service: "",
  migrate: true,
  packageManager: "npm",
};

/** stepsFor lists the steps that apply to these answers, in order: auth,
 *  single-file components and teams only exist for starter kits, as in the
 *  installer. */
export function stepsFor(a: NewAppAnswers): NewAppStep[] {
  const laravelAuth = a.starterKit && a.auth === "laravel";
  return [
    "name",
    "kit",
    "stack",
    ...(a.starterKit ? (["auth"] as const) : []),
    ...(laravelAuth && a.stack === "livewire" ? (["livewire"] as const) : []),
    ...(laravelAuth && (a.stack !== "livewire" || a.singleFile) ? (["teams"] as const) : []),
    "testing",
    "boost",
    "database",
    "javascript",
    "review",
  ];
}

/** The body for POST /api/sites/new (daemon api.NewSiteRequest). */
export function toRequest(a: NewAppAnswers, dir: string) {
  const steps = stepsFor(a);
  return {
    name: a.name
      .trim()
      .toLowerCase()
      .replace(/\.test$/, ""),
    dir,
    starter_kit: a.starterKit,
    stack: a.stack,
    auth: a.starterKit ? a.auth : undefined,
    class_components: steps.includes("livewire") && !a.singleFile,
    teams: steps.includes("teams") && a.teams,
    testing: a.testing,
    boost: a.boost,
    database: a.database,
    service: a.database === "sqlite" ? undefined : a.service || undefined,
    migrate: a.database === "sqlite" || a.migrate,
    package_manager: a.packageManager || undefined,
  };
}
