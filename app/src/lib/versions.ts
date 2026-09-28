/** sameRelease: two versions share major.minor.patch, so a "0.1.0-dev"
 *  daemon matches an "0.1.0" app. */
export function sameRelease(a: string, b: string): boolean {
  const core = (v: string) =>
    v
      .trim()
      .replace(/^v/, "")
      .split("-")[0]
      .split(".")
      .slice(0, 3)
      .map((n) => Number(n) || 0)
      .join(".");
  return core(a) === core(b);
}
