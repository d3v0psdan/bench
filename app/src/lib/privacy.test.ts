import { describe, expect, test } from "bun:test";
import { isSecretEnv, MASK, maskHost, maskPaths, maskText, privacyView } from "./privacy";

const ALL_ON = { on: true, names: true, folders: true, secrets: true, mail: true };

describe("maskHost", () => {
  test("keeps only the TLD, at one length for every name", () => {
    expect(maskHost("demo.test")).toBe(`${MASK}.test`);
    expect(maskHost("a-much-longer-name.test")).toBe(`${MASK}.test`);
  });
});

describe("maskText", () => {
  test("masks every site host in a sentence and leaves *.test alone", () => {
    expect(maskText("Deleting api.demo.test, then shop.test's .env")).toBe(`Deleting ${MASK}.test, then ${MASK}.test's .env`);
    expect(maskText("*.test DNS")).toBe("*.test DNS");
  });
});

describe("maskPaths", () => {
  test("masks paths that name the user and leaves other text alone", () => {
    expect(maskPaths("Couldn't read C:\\Users\\dan\\shop\\.env: denied")).toBe(`Couldn't read ${MASK}: denied`);
    expect(maskPaths("moved /home/dan/shop and ~/work")).toBe(`moved ${MASK} and ${MASK}`);
    expect(maskPaths("GET /api/sites failed at https://demo.test/x")).toBe("GET /api/sites failed at https://demo.test/x");
  });

  test("keeps going through spaces in folder names", () => {
    expect(maskPaths("open C:\\Users\\John Doe\\sites\\demo\\.env: access denied")).toBe(`open ${MASK}: access denied`);
    expect(maskPaths("open C:\\Users\\John Doe: access denied")).toBe(`open ${MASK}: access denied`);
    expect(maskPaths("C:\\Users\\dan\\shop was moved or deleted.")).toBe(`${MASK} was moved or deleted.`);
  });

  test("leaves /home and /Users inside URLs alone", () => {
    expect(maskPaths("https://x.test/home/about and http://127.0.0.1:8025/Users/x")).toBe(
      "https://x.test/home/about and http://127.0.0.1:8025/Users/x",
    );
    expect(privacyView(ALL_ON).text("https://x.test/home/about")).toBe(`https://${MASK}.test/home/about`);
  });
});

describe("isSecretEnv", () => {
  test("flags credential keys that hold a value", () => {
    expect(isSecretEnv("DB_PASSWORD", "hunter2")).toBe(true);
    expect(isSecretEnv("AWS_SECRET_ACCESS_KEY", "abc")).toBe(true);
    expect(isSecretEnv("DB_PASSWORD", "")).toBe(false);
    expect(isSecretEnv("MAIL_PASSWORD", "null")).toBe(false);
    expect(isSecretEnv("DB_HOST", "127.0.0.1")).toBe(false);
  });
});

describe("privacyView", () => {
  test("hides nothing while the master switch is off", () => {
    const p = privacyView({ ...ALL_ON, on: false });
    expect([p.names, p.folders, p.secrets, p.mail]).toEqual([false, false, false, false]);
    expect(p.host("demo.test")).toBe("demo.test");
    expect(p.path("C:\\Users\\dan\\demo")).toBe("C:\\Users\\dan\\demo");
  });

  test("hides only the options switched on", () => {
    const p = privacyView({ ...ALL_ON, folders: false });
    expect(p.host("demo.test")).toBe(`${MASK}.test`);
    expect(p.text("Opened demo.test from /home/dan/demo")).toBe(`Opened ${MASK}.test from /home/dan/demo`);
    expect(privacyView(ALL_ON).text("Opened demo.test from /home/dan/demo")).toBe(`Opened ${MASK}.test from ${MASK}`);
    expect(p.path("/home/dan/demo")).toBe("/home/dan/demo");
    expect(privacyView(ALL_ON).path("/home/dan/demo")).toBe(MASK);
  });
});
