import { useEffect } from "react";
import {
  ArrowLeftIcon,
  ClockCounterClockwiseIcon,
  DatabaseIcon,
  EnvelopeSimpleIcon,
  FileCodeIcon,
  GearSixIcon,
  GlobeIcon,
  type Icon,
  SidebarSimpleIcon,
  SquaresFourIcon,
} from "@phosphor-icons/react";
import { Button } from "@/components/ui/button";
import { Kbd } from "@/components/ui/kbd";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  useSidebar,
} from "@/components/ui/sidebar";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useRoute, type Route, type Section, type SiteTab } from "@/hooks/route";
import { IS_MAC } from "@/lib/utils";

// The navigation model (PLAN.md §4, Phase 4 scope): a grouped sidebar.
// Machine scope is Overview; Local; Inspect; Settings. On a site page the
// sidebar lists the site's own pages instead. Phase 3 adds Dumps under
// Inspect and Logs, Dumps and Dev under the site.

export interface NavItem {
  section: Section;
  label: string;
  icon: Icon;
  /** The digit of its Mod+digit shortcut. */
  key?: string;
}

export const NAV_GROUPS: Array<{ label?: string; items: NavItem[] }> = [
  { items: [{ section: "overview", label: "Overview", icon: SquaresFourIcon, key: "1" }] },
  {
    label: "Local",
    items: [
      { section: "sites", label: "Sites", icon: GlobeIcon, key: "2" },
      { section: "services", label: "Services", icon: DatabaseIcon, key: "3" },
      { section: "php", label: "Runtimes", icon: FileCodeIcon, key: "4" },
    ],
  },
  {
    label: "Inspect",
    items: [
      { section: "mail", label: "Mail", icon: EnvelopeSimpleIcon, key: "5" },
      { section: "activity", label: "Activity", icon: ClockCounterClockwiseIcon, key: "6" },
    ],
  },
];

export const SETTINGS_ITEM: NavItem = { section: "settings", label: "Settings", icon: GearSixIcon, key: "," };

const SITE_PAGES: Array<{ tab: SiteTab; label: string; icon: Icon }> = [
  { tab: "overview", label: "Overview", icon: SquaresFourIcon },
  { tab: "settings", label: "Settings", icon: GearSixIcon },
];

const MOD = IS_MAC ? "⌘" : "Ctrl+";

/** shortcutLabel renders a nav item's shortcut for hints ("Ctrl+2"). */
export function shortcutLabel(item: NavItem) {
  return item.key ? `${MOD}${item.key}` : "";
}

/** AppSidebar: the grouped page list, or a site's pages while one is open. */
export function AppSidebar() {
  const { route, navigate } = useRoute();
  useNavShortcuts();

  if (route.kind === "site") {
    return (
      <Shell>
        <TopRow>
          <SidebarMenuItem>
            <SidebarMenuButton
              tooltip={`All sites (${IS_MAC ? "⌘[" : "Alt+Left"})`}
              onClick={() => navigate({ kind: "home", section: "sites" })}
            >
              <ArrowLeftIcon />
              <span>All sites</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </TopRow>
        <SidebarContent>
          {/* No site name here: the header's site picker and the page title
              already show it. */}
          <SidebarGroup className="py-1">
            <SidebarGroupContent>
              <SidebarMenu>
                {SITE_PAGES.map((p) => (
                  <SidebarMenuItem key={p.tab}>
                    <SidebarMenuButton
                      isActive={route.tab === p.tab}
                      aria-current={route.tab === p.tab ? "page" : undefined}
                      tooltip={p.label}
                      onClick={() => navigate({ kind: "site", name: route.name, tab: p.tab })}
                    >
                      <p.icon />
                      <span>{p.label}</span>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                ))}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        </SidebarContent>
      </Shell>
    );
  }

  const [top, ...groups] = NAV_GROUPS;
  const item = (n: NavItem) => (
    <SidebarMenuItem key={n.section}>
      <SidebarMenuButton
        isActive={route.section === n.section}
        aria-current={route.section === n.section ? "page" : undefined}
        tooltip={`${n.label} (${shortcutLabel(n)})`}
        onClick={() => navigate({ kind: "home", section: n.section })}
      >
        <n.icon />
        <span className="flex-1">{n.label}</span>
        <Kbd className="opacity-0 transition-opacity duration-150 group-hover/menu-button:opacity-100 group-focus-visible/menu-button:opacity-100 group-data-[collapsible=icon]:hidden motion-reduce:transition-none">
          {shortcutLabel(n)}
        </Kbd>
      </SidebarMenuButton>
    </SidebarMenuItem>
  );

  return (
    <Shell>
      <TopRow>{top.items.map(item)}</TopRow>
      <SidebarContent>
        {groups.map((g, i) => (
          <SidebarGroup key={g.label ?? i} className="py-1">
            {g.label ? <SidebarGroupLabel>{g.label}</SidebarGroupLabel> : null}
            <SidebarGroupContent>
              <SidebarMenu>{g.items.map(item)}</SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        ))}
      </SidebarContent>
      <SidebarFooter>
        <SidebarMenu>{item(SETTINGS_ITEM)}</SidebarMenu>
      </SidebarFooter>
    </Shell>
  );
}

/** TopRow is the sidebar's first row: its first item with the collapse
 *  button on the right. Collapsed to icons they stack with the expand
 *  button first, in the DOM too, so focus order matches what's seen. */
function TopRow({ children }: { children: React.ReactNode }) {
  const { state } = useSidebar();
  const menu = <SidebarMenu className="min-w-0 flex-1">{children}</SidebarMenu>;
  return (
    <SidebarHeader className="flex-row items-center gap-1 pb-1 group-data-[collapsible=icon]:flex-col">
      {state === "collapsed" ? (
        <>
          <CollapseButton />
          {menu}
        </>
      ) : (
        <>
          {menu}
          <CollapseButton />
        </>
      )}
    </SidebarHeader>
  );
}

function CollapseButton() {
  const { state, toggleSidebar } = useSidebar();
  const label = state === "expanded" ? "Collapse sidebar" : "Expand sidebar";
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label={label}
          onClick={toggleSidebar}
          className="shrink-0 text-muted-foreground"
        >
          <SidebarSimpleIcon />
        </Button>
      </TooltipTrigger>
      <TooltipContent side="right">
        {label} ({MOD}B)
      </TooltipContent>
    </Tooltip>
  );
}

function Shell({ children }: { children: React.ReactNode }) {
  return (
    <Sidebar
      collapsible="icon"
      aria-label="Pages"
      // The page card draws the edge; the sidebar's own border doubled it.
      className="top-12 h-[calc(100svh-3rem)] group-data-[side=left]:border-r-0"
    >
      {children}
    </Sidebar>
  );
}

/** useNavShortcuts (mounted with the sidebar) wires Mod+1..6 to the sidebar pages, Mod+, to
 *  Settings, and Alt+Left (⌘[ on macOS) from a site back to all sites.
 *  Mod+B (collapse the sidebar) comes with the sidebar itself. */
function useNavShortcuts() {
  const { route, navigate } = useRoute();
  useEffect(() => {
    const items = [...NAV_GROUPS.flatMap((g) => g.items), SETTINGS_ITEM];
    const onKey = (e: KeyboardEvent) => {
      if (e.defaultPrevented || e.repeat) return;
      // A modal owns the keyboard: moving the page under it would strand it.
      if (document.querySelector('[role="dialog"][aria-modal="true"], [role="alertdialog"]')) return;
      const mod = IS_MAC ? e.metaKey : e.ctrlKey;
      const back = IS_MAC ? e.metaKey && e.key === "[" : e.altKey && e.key === "ArrowLeft";
      if (back && route.kind === "site") {
        e.preventDefault();
        navigate({ kind: "home", section: "sites" });
        return;
      }
      if (!mod || e.altKey || e.shiftKey) return;
      const hit = items.find((i) => i.key === e.key);
      if (!hit) return;
      e.preventDefault();
      navigate({ kind: "home", section: hit.section } satisfies Route);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [route, navigate]);
}
