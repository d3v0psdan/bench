import { describe, expect, test } from "bun:test";
import type { Site } from "./api";
import { groupSites } from "./site-groups";

const site = (name: string, kind: Site["kind"], extra: Partial<Site> = {}): Site => ({
  name,
  host: `${name}.test`,
  url: `https://${name}.test`,
  kind,
  ...extra,
});

const sites = [
  site("shop", "parked", { path: "/code/shop", parked_in: "/code", favorite: true }),
  site("blog", "parked", { path: "/code/blog", parked_in: "/code" }),
  site("old", "parked", { path: "/archive/old", parked_in: "/archive" }),
  site("api", "linked", { path: "/work/api" }),
  site("vite", "proxy", { proxy_to: "http://127.0.0.1:5173" }),
];

const shape = (groups: ReturnType<typeof groupSites>) => groups.map((g) => [g.key, g.sites.map((s) => s.name)]);

describe("groupSites", () => {
  test("favorites lead, then linked, proxies and one group per parked directory", () => {
    expect(shape(groupSites(sites, "", "all"))).toEqual([
      ["favorites", ["shop"]],
      ["linked", ["api"]],
      ["proxy", ["vite"]],
      ["parked:/archive", ["old"]],
      ["parked:/code", ["blog"]],
    ]);
  });

  test("the query matches host, folder and proxy target", () => {
    expect(shape(groupSites(sites, "ARCHIVE", "all"))).toEqual([["parked:/archive", ["old"]]]);
    expect(shape(groupSites(sites, "5173", "all"))).toEqual([["proxy", ["vite"]]]);
  });

  test("the kind filter keeps favorites of that kind only", () => {
    expect(shape(groupSites(sites, "", "linked"))).toEqual([["linked", ["api"]]]);
    expect(shape(groupSites(sites, "", "parked"))).toEqual([
      ["favorites", ["shop"]],
      ["parked:/archive", ["old"]],
      ["parked:/code", ["blog"]],
    ]);
  });
});
