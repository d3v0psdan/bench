import { describe, expect, test } from "bun:test";
import type { BinaryInfo, Site } from "@/lib/api";
import { sitePhp } from "@/lib/php";

const site = (over: Partial<Site> = {}): Site => ({ name: "shop", host: "shop.test", url: "https://shop.test", kind: "linked", ...over });
const php = (channel: string, installed: boolean): BinaryInfo => ({
  name: "php",
  channel,
  version: `${channel}.0`,
  installed,
  dir: "",
  sources: [],
});

describe("sitePhp", () => {
  test("follows the default when the site has no version", () => {
    expect(sitePhp(site(), "8.4", [php("8.4", true)])).toMatchObject({ channel: "8.4", installed: true });
  });

  test("says when the effective version isn't installed", () => {
    expect(sitePhp(site({ php: "8.3" }), "8.4", [php("8.3", false), php("8.4", true)])).toMatchObject({
      channel: "8.3",
      installed: false,
    });
  });

  test("is unknown while the catalog loads, never healthy", () => {
    expect(sitePhp(site(), "8.4", null).installed).toBeNull();
  });

  test("no version at all is not installed", () => {
    expect(sitePhp(site(), null, [])).toEqual({ channel: null, installed: false });
  });
});
