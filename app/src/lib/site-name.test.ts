import { describe, expect, test } from "bun:test";
import { folderName, nameProblem, randomName, suggestName } from "./site-name";

describe("site names", () => {
  test("suggestName makes a folder name servable", () => {
    expect(suggestName("Laravel App_v2")).toBe("laravel-app-v2");
    expect(suggestName("--shop--")).toBe("shop");
    expect(suggestName("demo.test")).toBe("demo");
  });

  test("nameProblem rejects invalid and taken names", () => {
    expect(nameProblem("shop", [])).toBeNull();
    expect(nameProblem("Shop.test", [])).toBeNull();
    expect(nameProblem("", [])).not.toBeNull();
    expect(nameProblem("-shop", [])).not.toBeNull();
    expect(nameProblem("my_shop", [])).not.toBeNull();
    expect(nameProblem("shop", ["shop"])).toBe("shop.test already exists.");
  });

  test("folderName reads Windows and Unix paths", () => {
    expect(folderName("C:\\Code\\shop\\")).toBe("shop");
    expect(folderName("/home/me/code/blog")).toBe("blog");
  });
});

describe("randomName", () => {
  test("is a valid site name", () => {
    for (let i = 0; i < 50; i++) expect(nameProblem(randomName([]), [])).toBeNull();
  });

  test("never returns a taken name", () => {
    const always = () => 0; // always the first words
    const first = randomName([], always);
    const next = randomName([first], always);
    expect(next).not.toBe(first);
    expect(nameProblem(next, [first])).toBeNull();
  });
});
