import { useEffect, useRef, useState, type ComponentType, type ReactNode } from "react";
import { XIcon, type Icon } from "@phosphor-icons/react";
import { cn } from "@/lib/utils";
import { Alert, AlertAction } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";

const DISMISSED_PREFIX = "bench.dismissed.";

/** DismissibleAlert: an informational callout the user can close for good.
 *  The id is remembered in localStorage (a UI preference, not app state),
 *  so put anything that should bring it back, like a port, into the id.
 *  Errors are never dismissible: they describe something still broken. */
export function DismissibleAlert({
  id,
  variant = "default",
  action,
  className,
  children,
}: {
  id: string;
  variant?: "default" | "warning";
  action?: ReactNode;
  className?: string;
  children: ReactNode;
}) {
  const key = DISMISSED_PREFIX + id;
  const [dismissed, setDismissed] = useState(() => localStorage.getItem(key) === "1");
  if (dismissed) return null;

  const dismiss = () => {
    localStorage.setItem(key, "1");
    setDismissed(true);
  };

  return (
    <Alert variant={variant} className={className}>
      {children}
      <AlertAction className="-mr-1 gap-1">
        {action}
        <Button variant="ghost" size="icon-sm" aria-label="Dismiss" onClick={dismiss} className="text-current hover:text-current">
          <XIcon />
        </Button>
      </AlertAction>
    </Alert>
  );
}

/** PageHeader: the title row every page opens with, actions on the right. */
export function PageHeader({
  title,
  description,
  actions,
  leading,
}: {
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  leading?: ReactNode;
}) {
  return (
    <header
      className={cn(
        "mb-6 flex flex-wrap justify-between gap-x-6 gap-y-3",
        // A lone title shares one center with its actions (ui-standard V3).
        description ? "items-start" : "items-center",
      )}
    >
      <div className="flex min-w-0 items-center gap-3">
        {leading}
        <div className="min-w-0">
          <h1 className="truncate text-xl font-medium tracking-tight">{title}</h1>
          {description ? (
            <div className="mt-1 text-[13px] text-muted-foreground">{description}</div>
          ) : null}
        </div>
      </div>
      {actions ? <div className="flex shrink-0 items-center gap-2">{actions}</div> : null}
    </header>
  );
}

/** SectionTitle: the small heading above a list or grid ("Latest deployments"). */
export function SectionTitle({ children, action }: { children: ReactNode; action?: ReactNode }) {
  return (
    <div className="mb-3 flex items-center justify-between gap-3">
      <h2 className="text-[15px] font-medium">{children}</h2>
      {action}
    </div>
  );
}

/** SectionCard: a settings block. A recessed frame carries the
 *  title, a raised card inside it carries the rows. */
export function SectionCard({
  title,
  description,
  action,
  tone = "default",
  children,
  className,
}: {
  title: ReactNode;
  description?: ReactNode;
  action?: ReactNode;
  tone?: "default" | "danger";
  children: ReactNode;
  className?: string;
}) {
  return (
    <section className={cn("rounded-xl border border-subtle bg-well p-1", className)}>
      <header className="flex items-start justify-between gap-4 px-4 pt-3 pb-3.5">
        <div className="min-w-0">
          {/* Focusable by script only: the settings sub-nav moves focus here. */}
          <h2
            tabIndex={-1}
            className={cn(
              "rounded-sm text-sm font-medium outline-none focus-visible:ring-3 focus-visible:ring-ring",
              tone === "danger" && "text-destructive",
            )}
          >
            {title}
          </h2>
          {description ? <p className="mt-0.5 text-[13px] text-muted-foreground">{description}</p> : null}
        </div>
        {action}
      </header>
      <div className="divide-y rounded-lg bg-card shadow-card">{children}</div>
    </section>
  );
}

/** SectionRow: label and description on the left, the control on the right. */
export function SectionRow({
  label,
  description,
  children,
  className,
}: {
  label: ReactNode;
  description?: ReactNode;
  children?: ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("flex flex-wrap items-center justify-between gap-x-6 gap-y-3 px-4 py-4", className)}>
      <div className="min-w-0 flex-1 basis-56">
        <div className="text-sm font-medium">{label}</div>
        {description ? <div className="mt-0.5 text-[13px] text-muted-foreground">{description}</div> : null}
      </div>
      {children ? <div className="flex min-w-0 shrink-0 items-center gap-2">{children}</div> : null}
    </div>
  );
}

const DOT_TONES = {
  success: { text: "text-success", dot: "bg-success-solid" },
  warning: { text: "text-warning", dot: "bg-warning-solid" },
  danger: { text: "text-destructive", dot: "bg-destructive-solid" },
  neutral: { text: "text-muted-foreground", dot: "bg-muted-foreground/60" },
} as const;

export type DotTone = keyof typeof DOT_TONES;

/** One vocabulary for diagnostic checks everywhere (ui-standard N3, A2). */
export const CHECK_TONES = { ok: "success", warn: "warning", fail: "danger" } as const satisfies Record<string, DotTone>;
export const CHECK_LABELS = { ok: "OK", warn: "Warning", fail: "Failing" } as const;

/** StatusDot: a coloured dot and a word ("● Enabled"); busy swaps the dot
 *  for a spinner. */
export function StatusDot({
  tone,
  busy = false,
  children,
}: {
  tone: DotTone;
  busy?: boolean;
  children: ReactNode;
}) {
  const t = DOT_TONES[tone];
  return (
    <span className={cn("inline-flex items-center gap-1.5 text-[13px] font-medium whitespace-nowrap", t.text)}>
      {busy ? (
        <Spinner className="size-3 motion-reduce:animate-none" />
      ) : (
        <span aria-hidden className={cn("size-1.5 shrink-0 rounded-full", t.dot)} />
      )}
      {children}
    </span>
  );
}

const TILE_TONES = [
  "bg-success-solid/10 text-success ring-success-solid/25",
  "bg-brand/8 text-brand ring-brand/25",
  "bg-warning-solid/15 text-warning ring-warning-solid/35",
  "bg-destructive-solid/8 text-destructive ring-destructive-solid/25",
];

/** LetterTile: a square tile with the object's initial. The tint is
 *  stable per name. */
export function LetterTile({ name, size = "sm" }: { name: string; size?: "sm" | "lg" }) {
  let hash = 0;
  for (const ch of name) hash = (hash * 31 + ch.charCodeAt(0)) | 0;
  return (
    <span
      aria-hidden
      className={cn(
        "inline-flex shrink-0 items-center justify-center rounded-md font-medium uppercase ring-1 ring-inset",
        TILE_TONES[Math.abs(hash) % TILE_TONES.length],
        size === "lg" ? "size-10 text-lg" : "size-5 text-[11px]",
      )}
    >
      {name.charAt(0)}
    </span>
  );
}

/** One card on a settings page. The nav's sub-links and the rendered
 *  sections both come from this, so they can't drift apart. */
export interface SettingsSection {
  id: string;
  label: string;
  Content: ComponentType;
}

export interface SettingsNavItem<T extends string> {
  value: T;
  label: string;
  icon: Icon;
  sections: SettingsSection[];
}

// How far below the scroller's top a section counts as the one being read.
const SECTION_READ_LINE_PX = 48;

/** SettingsLayout: a settings page, a titled icon sub-nav on
 *  the left and the selected page's section cards on the right. Pages with
 *  more than one section list them under their nav item; a sub-link scrolls
 *  its section into view and moves focus to its heading. A sub-link on
 *  another page goes through onChange with its section, since the app
 *  remounts the pane on every route change; `section` arrives with it. */
export function SettingsLayout<T extends string>({
  items,
  value,
  section,
  onChange,
}: {
  items: SettingsNavItem<T>[];
  value: T;
  /** The section to scroll to when the layout mounts. */
  section?: string;
  onChange: (value: T, section?: string) => void;
}) {
  const scroller = useRef<HTMLDivElement>(null);
  const [active, setActive] = useState<string | null>(section ?? null);
  // A fresh object per click, so picking the same section twice scrolls twice.
  const [target, setTarget] = useState<{ page: T; id: string } | null>(section ? { page: value, id: section } : null);
  // The section last picked stays highlighted until the user scrolls by
  // hand, even when it is too short to reach the top.
  const picked = useRef<string | null>(section ?? null);
  const page = items.find((item) => item.value === value) ?? items[0];
  // Before any scrolling, the page is read from its top.
  const reading = active ?? page.sections[0]?.id;

  const track = () => {
    const el = scroller.current;
    if (!el || picked.current) return;
    const sections = [...el.querySelectorAll<HTMLElement>("[data-settings-section]")];
    const line = el.getBoundingClientRect().top + SECTION_READ_LINE_PX;
    const atEnd = el.scrollTop > 0 && el.scrollTop + el.clientHeight >= el.scrollHeight - 1;
    const passed = atEnd ? sections : sections.filter((s) => s.getBoundingClientRect().top <= line);
    const current = passed[passed.length - 1] ?? sections[0];
    setActive(current?.dataset.settingsSection ?? null);
  };

  useEffect(() => {
    if (!target || target.page !== value) return;
    const el = scroller.current;
    const section = document.getElementById(`settings-${target.page}-${target.id}`);
    if (!el || !section) return;
    const top = section.getBoundingClientRect().top - el.getBoundingClientRect().top + el.scrollTop;
    const reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    el.scrollTo({ top, behavior: reduce ? "auto" : "smooth" });
    section.querySelector<HTMLElement>("h2")?.focus({ preventScroll: true });
  }, [target, value]);

  const openPage = (next: T) => {
    picked.current = null;
    setTarget(null);
    setActive(null);
    onChange(next);
  };

  const openSection = (next: T, id: string) => {
    picked.current = id;
    setActive(id);
    setTarget({ page: next, id });
    if (next !== value) onChange(next, id);
  };

  const releasePick = () => {
    picked.current = null;
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <h1 className="mb-6 shrink-0 text-xl font-medium tracking-tight">Settings</h1>
      <div className="grid min-h-0 flex-1 grid-cols-[11rem_minmax(0,1fr)] grid-rows-[minmax(0,1fr)] items-start gap-10">
        <nav aria-label="Settings" className="-m-1 -ml-4 flex max-h-full flex-col gap-0.5 overflow-y-auto p-1">
          {items.map((item) => (
            <div key={item.value} className="flex flex-col gap-0.5">
              <button
                type="button"
                onClick={() => openPage(item.value)}
                aria-current={value === item.value ? "page" : undefined}
                className={cn(
                  "flex h-8 shrink-0 items-center gap-2.5 rounded-md px-3 text-left text-sm text-muted-foreground outline-none transition-colors duration-100 ease-out",
                  "hover:bg-subtle/60 hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring",
                  "aria-[current=page]:bg-subtle aria-[current=page]:font-medium aria-[current=page]:text-foreground",
                )}
              >
                <item.icon className="size-4 shrink-0" />
                {item.label}
              </button>
              {/* One section is the page itself; a sub-link would repeat it. */}
              {item.sections.length > 1 ? (
                <ul aria-label={`${item.label} sections`} className="mb-1 ml-5 flex flex-col gap-0.5 border-l pl-2">
                  {item.sections.map((s) => (
                    <li key={s.id} className="flex">
                      <button
                        type="button"
                        onClick={() => openSection(item.value, s.id)}
                        aria-current={value === item.value && reading === s.id ? "location" : undefined}
                        className={cn(
                          "relative flex h-7 min-w-0 flex-1 items-center rounded-md px-2.5 text-left text-[13px] text-muted-foreground outline-none transition-colors duration-100 ease-out",
                          "hover:bg-subtle/60 hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring",
                          "before:absolute before:inset-y-1.5 before:-left-[9px] before:w-px aria-[current=location]:text-foreground aria-[current=location]:before:bg-foreground",
                        )}
                      >
                        <span className="truncate">{s.label}</span>
                      </button>
                    </li>
                  ))}
                </ul>
              ) : null}
            </div>
          ))}
        </nav>
        {/* The only scrolling part; the padding keeps card shadows unclipped. */}
        <div
          ref={scroller}
          onScroll={track}
          onWheel={releasePick}
          onTouchMove={releasePick}
          onKeyDown={releasePick}
          onPointerDown={releasePick}
          className="-m-2 flex max-h-full min-w-0 flex-col gap-6 overflow-y-auto overscroll-contain p-2 [scrollbar-gutter:stable]"
        >
          {page.sections.map((s) => (
            <div
              key={`${page.value}-${s.id}`}
              id={`settings-${page.value}-${s.id}`}
              data-settings-section={s.id}
              className="flex flex-col gap-6"
            >
              <s.Content />
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

/** ListCard: the bordered row container used for lists (logs, deployments). */
export function ListCard({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cn("divide-y overflow-hidden rounded-lg bg-card shadow-card", className)}>{children}</div>;
}
