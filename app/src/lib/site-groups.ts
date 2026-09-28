import type { Site } from "@/lib/api";

export type SiteKindFilter = "all" | Site["kind"];

export interface SiteGroup {
  key: string;
  title: string;
  /** A parked group's directory, shown in mono under the title. */
  dir?: string;
  sites: Site[];
}

/** groupSites filters sites by a query (host or folder) and kind, then
 *  groups them: favorites first, then linked, proxies, and one group per
 *  parked directory. Each site appears once. */
export function groupSites(sites: Site[], query: string, kind: SiteKindFilter): SiteGroup[] {
  const q = query.trim().toLowerCase();
  const shown = sites.filter(
    (s) =>
      (kind === "all" || s.kind === kind) &&
      (q === "" || [s.host, s.path, s.proxy_to].some((v) => v?.toLowerCase().includes(q))),
  );

  const groups: SiteGroup[] = [];
  const add = (key: string, title: string, list: Site[], dir?: string) => {
    if (list.length > 0) groups.push({ key, title, dir, sites: list });
  };
  add(
    "favorites",
    "Favorites",
    shown.filter((s) => s.favorite),
  );
  const rest = shown.filter((s) => !s.favorite);
  add(
    "linked",
    "Linked",
    rest.filter((s) => s.kind === "linked"),
  );
  add(
    "proxy",
    "Proxies",
    rest.filter((s) => s.kind === "proxy"),
  );
  const dirs = [...new Set(rest.filter((s) => s.kind === "parked").map((s) => s.parked_in ?? ""))].sort();
  for (const dir of dirs) {
    add(
      `parked:${dir}`,
      "Parked",
      rest.filter((s) => s.kind === "parked" && (s.parked_in ?? "") === dir),
      dir || undefined,
    );
  }
  return groups;
}
