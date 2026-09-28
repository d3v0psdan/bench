import { createContext, useContext } from "react";

/** Top-level sections, the tab row when no site is open. */
/** Activity is a section without a tab: the header button and the tray open it. */
export type Section = "overview" | "sites" | "services" | "php" | "mail" | "settings" | "activity";

/** A site's own tabs. Phase 3 adds dumps, logs and the dev runner here. */
export type SiteTab = "overview" | "settings";

/** Settings sub-pages, the left-hand nav on the Settings tab. */
export type SettingsPage = "general" | "privacy" | "https" | "diagnostics" | "about";


/** An action a page runs once on arrival, so the palette can start what
 *  the page's own buttons start. */
export type Intent = "new" | "link" | "park" | "proxy" | "new-service";

export type Route =
  | {
      kind: "home";
      section: Section;
      settings?: SettingsPage;
      /** The settings card to scroll to on arrival (a sub-link's section id). */
      settingsSection?: string;
      service?: string;
      serviceLogs?: boolean;
      intent?: Intent;
      /** Mail opens filtered to this inbox (an app's APP_NAME). */
      inbox?: string;
    }
  | { kind: "site"; name: string; tab: SiteTab };

export const HOME: Route = { kind: "home", section: "overview" };

export const RouteContext = createContext<{ route: Route; navigate: (route: Route) => void } | null>(null);

/** useRoute gives panes the current page and a way to move to another. */
export function useRoute() {
  const ctx = useContext(RouteContext);
  if (!ctx) throw new Error("useRoute outside RouteContext");
  return ctx;
}
